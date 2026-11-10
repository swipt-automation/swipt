package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"

	"github.com/coder/websocket"
	"golang.org/x/net/http/httpguts"
)

// The gRPC-Web translation and the "grpc-websockets" subprotocol are wire
// compatible with the improbable-eng/grpc-web client transports: a WebSocket
// carries one stream, its first message holds the request metadata, each
// later binary message starts with 0 for body data or is the single byte 1
// for half-close, and the server answers with gRPC-Web frames.
const (
	contentTypeGRPC        = "application/grpc"
	contentTypeGRPCWeb     = "application/grpc-web"
	contentTypeGRPCWebText = "application/grpc-web-text"

	webSocketSubprotocol  = "grpc-websockets"
	webSocketReadLimit    = 1 << 22
	webSocketPingInterval = 30 * time.Second
)

func isWebSocketGRPCRequest(request *http.Request) bool {
	return httpguts.HeaderValuesContainsToken(request.Header.Values("Upgrade"), "websocket") &&
		httpguts.HeaderValuesContainsToken(request.Header.Values("Sec-Websocket-Protocol"), webSocketSubprotocol)
}

func (s *Server) serveWeb(writer http.ResponseWriter, request *http.Request) {
	isTextFormat := strings.HasPrefix(request.Header.Get("Content-Type"), contentTypeGRPCWebText)
	webContentType := contentTypeGRPCWeb
	grpcRequest := request.Clone(request.Context())
	if isTextFormat {
		webContentType = contentTypeGRPCWebText
		grpcRequest.Body = &bodyReadCloser{
			Reader: base64.NewDecoder(base64.StdEncoding, request.Body),
			Closer: request.Body,
		}
	}
	grpcRequest.ProtoMajor = 2
	grpcRequest.ProtoMinor = 0
	grpcRequest.Header.Set("Content-Type", strings.Replace(request.Header.Get("Content-Type"), webContentType, contentTypeGRPC, 1))
	grpcRequest.Header.Del("Content-Length")
	response := newWebResponseWriter(writer, isTextFormat)
	s.grpc.ServeHTTP(response, grpcRequest)
	response.finish()
}

type bodyReadCloser struct {
	io.Reader
	io.Closer
}

type webResponseWriter struct {
	writer       http.ResponseWriter
	rawWriter    http.ResponseWriter
	header       http.Header
	contentType  string
	wroteHeaders bool
	wroteBody    bool
}

func newWebResponseWriter(writer http.ResponseWriter, isTextFormat bool) *webResponseWriter {
	response := &webResponseWriter{
		writer:      writer,
		rawWriter:   writer,
		header:      make(http.Header),
		contentType: contentTypeGRPCWeb,
	}
	if isTextFormat {
		response.writer = newBase64ResponseWriter(writer)
		response.contentType = contentTypeGRPCWebText
	}
	return response
}

func (w *webResponseWriter) Header() http.Header {
	return w.header
}

func (w *webResponseWriter) Write(content []byte) (int, error) {
	if !w.wroteHeaders {
		w.prepareHeaders()
		w.wroteHeaders = true
	}
	w.wroteBody = true
	return w.writer.Write(content)
}

func (w *webResponseWriter) WriteHeader(statusCode int) {
	if !w.wroteHeaders {
		w.prepareHeaders()
		w.wroteHeaders = true
	}
	w.writer.WriteHeader(statusCode)
}

func (w *webResponseWriter) Flush() {
	if w.wroteHeaders || w.wroteBody {
		flushWriter(w.writer)
	}
}

func (w *webResponseWriter) prepareHeaders() {
	rawHeader := w.rawWriter.Header()
	for key, values := range w.header {
		canonicalKey := http.CanonicalHeaderKey(strings.TrimPrefix(key, http.TrailerPrefix))
		if canonicalKey == "Trailer" {
			continue
		}
		if canonicalKey == "Content-Type" {
			newValues := make([]string, 0, len(values))
			for _, value := range values {
				newValues = append(newValues, strings.Replace(value, contentTypeGRPC, w.contentType, 1))
			}
			values = newValues
		}
		rawHeader[canonicalKey] = values
	}
}

func (w *webResponseWriter) finish() {
	if w.wroteHeaders || w.wroteBody {
		w.writeTrailerFrame()
	} else {
		w.WriteHeader(http.StatusOK)
		flushWriter(w.writer)
	}
}

func (w *webResponseWriter) writeTrailerFrame() {
	flushedKeys := make(map[string]bool)
	for key := range w.rawWriter.Header() {
		flushedKeys[strings.ToLower(key)] = true
	}
	trailerHeader := make(http.Header)
	for key, values := range w.header {
		lowerKey := strings.ToLower(strings.TrimPrefix(key, http.TrailerPrefix))
		if lowerKey == "trailer" || flushedKeys[lowerKey] {
			continue
		}
		trailerHeader[lowerKey] = values
	}
	var trailerBuffer bytes.Buffer
	trailerHeader.Write(&trailerBuffer)
	w.writer.Write(webMetadataFrameHeader(trailerBuffer.Len()))
	w.writer.Write(trailerBuffer.Bytes())
	flushWriter(w.writer)
}

func webMetadataFrameHeader(payloadLength int) []byte {
	return binary.BigEndian.AppendUint32([]byte{1 << 7}, uint32(payloadLength))
}

func flushWriter(writer http.ResponseWriter) {
	flusher, isFlusher := writer.(http.Flusher)
	if isFlusher {
		flusher.Flush()
	}
}

type base64ResponseWriter struct {
	wrapped http.ResponseWriter
	encoder io.WriteCloser
}

func newBase64ResponseWriter(wrapped http.ResponseWriter) http.ResponseWriter {
	writer := &base64ResponseWriter{wrapped: wrapped}
	writer.encoder = base64.NewEncoder(base64.StdEncoding, wrapped)
	return writer
}

func (w *base64ResponseWriter) Header() http.Header {
	return w.wrapped.Header()
}

func (w *base64ResponseWriter) Write(content []byte) (int, error) {
	return w.encoder.Write(content)
}

func (w *base64ResponseWriter) WriteHeader(statusCode int) {
	w.wrapped.WriteHeader(statusCode)
}

func (w *base64ResponseWriter) Flush() {
	w.encoder.Close()
	w.encoder = base64.NewEncoder(base64.StdEncoding, w.wrapped)
	flushWriter(w.wrapped)
}

func (s *Server) serveWebSocket(writer http.ResponseWriter, request *http.Request) {
	conn, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		Subprotocols:       []string{webSocketSubprotocol},
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.options.Logger.Debug("accept a websocket: ", err)
		return
	}
	conn.SetReadLimit(webSocketReadLimit)
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	messageType, firstMessage, err := conn.Read(ctx)
	if err != nil || messageType != websocket.MessageBinary {
		conn.CloseNow()
		return
	}
	header, err := parseWebSocketHeader(firstMessage)
	if err != nil {
		s.options.Logger.Debug("parse websocket request metadata: ", err)
		conn.CloseNow()
		return
	}
	contentType := header.Get("Content-Type")
	if contentType == "" {
		header.Set("Content-Type", contentTypeGRPC)
	} else {
		header.Set("Content-Type", strings.Replace(contentType, contentTypeGRPCWeb, contentTypeGRPC, 1))
	}
	header.Del("Content-Length")
	response := newWebSocketResponseWriter(ctx, conn)
	grpcRequest := request.WithContext(ctx)
	grpcRequest.Method = http.MethodPost
	grpcRequest.ProtoMajor = 2
	grpcRequest.ProtoMinor = 0
	grpcRequest.Header = header
	grpcRequest.Body = &webSocketBodyReader{
		ctx:      ctx,
		cancel:   cancel,
		conn:     conn,
		response: response,
	}
	go keepWebSocketAlive(ctx, conn)
	s.grpc.ServeHTTP(response, grpcRequest)
	response.writeTrailerFrame()
	conn.Close(websocket.StatusNormalClosure, "")
}

func parseWebSocketHeader(content []byte) (http.Header, error) {
	reader := textproto.NewReader(bufio.NewReader(io.MultiReader(bytes.NewReader(content), strings.NewReader("\r\n"))))
	mimeHeader, err := reader.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	return http.Header(mimeHeader), nil
}

func keepWebSocketAlive(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(webSocketPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := conn.Ping(ctx); err != nil {
				return
			}
		}
	}
}

type webSocketResponseWriter struct {
	ctx           context.Context
	conn          *websocket.Conn
	header        http.Header
	flushedHeader http.Header
	wroteHeaders  bool
	wroteTrailers bool
}

func newWebSocketResponseWriter(ctx context.Context, conn *websocket.Conn) *webSocketResponseWriter {
	return &webSocketResponseWriter{
		ctx:           ctx,
		conn:          conn,
		header:        make(http.Header),
		flushedHeader: make(http.Header),
	}
}

func (w *webSocketResponseWriter) Header() http.Header {
	return w.header
}

func (w *webSocketResponseWriter) Write(content []byte) (int, error) {
	if !w.wroteHeaders {
		w.WriteHeader(http.StatusOK)
	}
	if err := w.conn.Write(w.ctx, websocket.MessageBinary, content); err != nil {
		return 0, err
	}
	return len(content), nil
}

func (w *webSocketResponseWriter) WriteHeader(int) {
	if w.wroteHeaders {
		return
	}
	w.wroteHeaders = true
	headerFrame := make(http.Header)
	for key, values := range w.header {
		canonicalKey := http.CanonicalHeaderKey(key)
		if canonicalKey == "Trailer" {
			continue
		}
		w.flushedHeader[canonicalKey] = values
		headerFrame[canonicalKey] = values
	}
	w.writeHeaderFrame(headerFrame)
}

func (w *webSocketResponseWriter) Flush() {
}

func (w *webSocketResponseWriter) writeHeaderFrame(header http.Header) {
	var headerBuffer bytes.Buffer
	header.Write(&headerBuffer)
	frame := make([]byte, 0, 5+headerBuffer.Len())
	frame = append(frame, webMetadataFrameHeader(headerBuffer.Len())...)
	frame = append(frame, headerBuffer.Bytes()...)
	_ = w.conn.Write(w.ctx, websocket.MessageBinary, frame)
}

func (w *webSocketResponseWriter) writeTrailerFrame() {
	if w.wroteTrailers {
		return
	}
	w.wroteTrailers = true
	trailerHeader := make(http.Header)
	for key, values := range w.header {
		lowerKey := strings.ToLower(strings.TrimPrefix(key, http.TrailerPrefix))
		if lowerKey == "trailer" {
			continue
		}
		if _, flushed := w.flushedHeader[http.CanonicalHeaderKey(lowerKey)]; flushed {
			continue
		}
		trailerHeader[lowerKey] = values
	}
	w.writeHeaderFrame(trailerHeader)
}

type webSocketBodyReader struct {
	ctx       context.Context
	cancel    context.CancelFunc
	conn      *websocket.Conn
	response  *webSocketResponseWriter
	remaining []byte
}

func (r *webSocketBodyReader) Read(buffer []byte) (int, error) {
	if len(r.remaining) > 0 {
		n := copy(buffer, r.remaining)
		r.remaining = r.remaining[n:]
		return n, nil
	}
	for {
		messageType, payload, err := r.conn.Read(r.ctx)
		if err != nil {
			r.cancel()
			return 0, err
		}
		if messageType != websocket.MessageBinary {
			return 0, E.New("unexpected non-binary websocket message")
		}
		if len(payload) == 0 {
			continue
		}
		if payload[0] == 1 {
			go r.waitForClose()
			return 0, io.EOF
		}
		content := payload[1:]
		if len(content) == 0 {
			continue
		}
		n := copy(buffer, content)
		r.remaining = content[n:]
		return n, nil
	}
}

func (r *webSocketBodyReader) waitForClose() {
	for {
		if _, _, err := r.conn.Read(r.ctx); err != nil {
			r.cancel()
			return
		}
	}
}

// grpc-go's handler transport closes the body after it writes the status, so
// the trailer frame has to go out here, before the connection closes.
func (r *webSocketBodyReader) Close() error {
	r.response.writeTrailerFrame()
	return r.conn.Close(websocket.StatusNormalClosure, "")
}
