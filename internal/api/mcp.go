package api

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"math"
	"net/http"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/automation/declarations"

	"github.com/nekohasekai/go-sdk/mcp"
	"golang.org/x/image/draw"
)

const (
	mcpSessionIdle = time.Hour
	taskAfter      = time.Second
	progressEvery  = 10 * time.Second
)

// Claude Code scales an image down to fit 2000x2000 pixels, unless the model
// sets larger limits, and to fit maxImageTokens tiles of 28x28 pixels, which is
// 4784 for the models that set it (Claude Code 2.1.322, Vw and Rle in
// module-117, D$n and Y in module-442, image_limits in module-061).
const (
	imageSideLimit = 2000
	imageTile      = 28
	imageTileLimit = 4784
)

type jsInput struct {
	Code      string  `json:"code" jsonschema:"JavaScript with top-level await"`
	TimeoutMs float64 `json:"timeoutMs,omitempty" jsonschema:"Time limit in milliseconds"`
}

func (s *Server) newMCP() (*mcp.Server, http.Handler) {
	server := mcp.NewServer(&mcp.Implementation{Name: "swipt", Title: "Swipt", Version: serverVersion()}, &mcp.ServerOptions{
		Tasks: &mcp.TaskOptions{CreateAfter: taskAfter},
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "js",
		Title:       "Control the device",
		Description: "Run JavaScript on the device; import declarations and import() load " + strings.Join(declarations.Modules, ", "),
		Meta:        mcp.Meta{"anthropic/alwaysLoad": true},
	}, s.js)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "js_api",
		Title:       "Device JavaScript API",
		Description: "TypeScript declarations of the js globals and modules",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		Meta:        mcp.Meta{"anthropic/alwaysLoad": true},
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: declarations.TypeScript}}}, nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "js_reset",
		Title:       "Reset the JavaScript session",
		Description: "Reset the js session",
	}, s.jsReset)
	return server, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		SessionTimeout: mcpSessionIdle,
	})
}

func (s *Server) serveMCP(w http.ResponseWriter, r *http.Request, credential *Credential) {
	if credential != nil {
		token, _, isBasic := r.BasicAuth()
		if !isBasic {
			token, _ = strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if !credential.authorized(token) {
			w.Header().Set("WWW-Authenticate", `Basic realm="swipt"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
	}
	// Quick Tunnels hold back the body of a text/event-stream response until it ends.
	if r.Method == http.MethodGet {
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	s.mcpHTTP.ServeHTTP(w, r)
}

func (s *Server) mcpSession(session *mcp.ServerSession) (*automation.REPL, error) {
	s.access.Lock()
	defer s.access.Unlock()
	if s.closed {
		return nil, errStopped
	}
	repl := s.sessions[session]
	if repl == nil {
		repl = s.options.Swiplets.NewREPL(s.options.Dialog)
		s.sessions[session] = repl
		go func() {
			_ = session.Wait()
			s.access.Lock()
			delete(s.sessions, session)
			s.access.Unlock()
			repl.Close()
		}()
	}
	return repl, nil
}

func (s *Server) js(ctx context.Context, req *mcp.CallToolRequest, input jsInput) (*mcp.CallToolResult, any, error) {
	if input.TimeoutMs < 0 {
		return nil, nil, E.New("timeoutMs must not be negative")
	}
	repl, err := s.mcpSession(req.Session)
	if err != nil {
		return nil, nil, err
	}
	timeout := time.Duration(input.TimeoutMs * float64(time.Millisecond))
	if token := req.Params.GetProgressToken(); token != nil {
		ticker := time.NewTicker(progressEvery)
		done := make(chan struct{})
		defer func() {
			close(done)
			ticker.Stop()
		}()
		go func() {
			for progress := 1; ; progress++ {
				select {
				case <-ticker.C:
				case <-done:
					return
				}
				err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token, Progress: float64(progress), Message: "running"})
				if err != nil {
					s.options.Logger.Debug(E.Cause(err, "notify progress"))
				}
			}
		}()
	}
	return mcp.StartTaskFor(ctx, req, func(ctx context.Context, _ *mcp.ServerTask) (*mcp.CallToolResult, any, error) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		defer context.AfterFunc(s.ctx, cancel)()
		result := &mcp.CallToolResult{}
		var text []string
		flush := func() {
			if len(text) > 0 {
				result.Content = append(result.Content, &mcp.TextContent{Text: strings.Join(text, "\n")})
				text = nil
			}
		}
		err := repl.Execute(ctx, automation.Script{Code: input.Code}, timeout, func(output automation.Output) {
			if output.Kind != automation.OutputImage {
				text = append(text, output.Text)
				return
			}
			flush()
			data, err := fitForModel(output.Image)
			if err != nil {
				text = append(text, err.Error())
				return
			}
			result.Content = append(result.Content, &mcp.ImageContent{Data: data, MIMEType: "image/png"})
		})
		if err != nil {
			failure, isFailure := automation.ScriptFailure(err)
			if !isFailure {
				return nil, nil, err
			}
			result.IsError = true
			text = append(text, failure.Describe())
		}
		flush()
		return result, nil, nil
	})
}

func (s *Server) jsReset(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	repl, err := s.mcpSession(req.Session)
	if err != nil {
		return nil, nil, err
	}
	if err = repl.Reset(ctx); err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "reset"}}}, nil, nil
}

func fitForModel(data []byte) ([]byte, error) {
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, E.Cause(err, "decode the image")
	}
	bounds := source.Bounds()
	scale := min(1, imageSideLimit/float64(max(bounds.Dx(), bounds.Dy())))
	size := func() (int, int) {
		return max(int(math.Round(float64(bounds.Dx())*scale)), 1), max(int(math.Round(float64(bounds.Dy())*scale)), 1)
	}
	for width, height := size(); (width+imageTile-1)/imageTile*((height+imageTile-1)/imageTile) > imageTileLimit; width, height = size() {
		scale *= 0.99
	}
	width, height := size()
	if width == bounds.Dx() && height == bounds.Dy() {
		return data, nil
	}
	scaled := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), source, bounds, draw.Src, nil)
	var buffer bytes.Buffer
	if err = png.Encode(&buffer, scaled); err != nil {
		return nil, E.Cause(err, "encode the image")
	}
	return buffer.Bytes(), nil
}
