package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/log"
	"github.com/swipt-automation/swipt/internal/proto"

	"github.com/nekohasekai/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	MCPPath = "/mcp"

	chunkSize         = 256 << 10
	keepaliveInterval = 30 * time.Second
	headerTimeout     = 10 * time.Second
)

var (
	errNoToken         = status.Error(codes.Unauthenticated, "the API requires a token")
	errUnauthenticated = status.Error(codes.Unauthenticated, "the token is wrong")
	errTokenReset      = status.Error(codes.Unauthenticated, "the token was reset")
	errStopped         = status.Error(codes.Unavailable, "the API stopped")
)

type credentialKey struct{}

type Credential struct {
	server *Server
	token  string
}

type request struct {
	cancel     context.CancelCauseFunc
	credential *Credential
}

type Options struct {
	Device     *device.Connection
	Swiplets   *automation.Host
	Dialog     automation.Dialog
	Logs       *log.Factory
	Logger     logger.ContextLogger
	AppVersion string
	AppBuild   string
	Shutdown   func()
}

type Server struct {
	options Options
	ctx     context.Context
	cancel  context.CancelFunc
	grpc    *grpc.Server
	servers []*http.Server
	mcp     *mcp.Server
	mcpHTTP http.Handler

	access   sync.Mutex
	session  *automation.REPL
	sessions map[*mcp.ServerSession]*automation.REPL
	requests map[uint64]request
	sequence uint64
	draining chan struct{}
	closed   bool
}

func New(options Options) *Server {
	if options.Logger == nil {
		options.Logger = logger.NOP()
	}
	s := &Server{
		options:  options,
		sessions: make(map[*mcp.ServerSession]*automation.REPL),
		requests: make(map[uint64]request),
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.grpc = grpc.NewServer(
		grpc.UnaryInterceptor(func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			ctx, release, err := s.authorize(ctx)
			if err != nil {
				return nil, err
			}
			defer release()
			return handler(ctx, request)
		}),
		grpc.StreamInterceptor(func(server any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			ctx, release, err := s.authorize(stream.Context())
			if err != nil {
				return err
			}
			defer release()
			return handler(server, &authorizedStream{ServerStream: stream, ctx: ctx})
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: keepaliveInterval}),
	)
	proto.RegisterSystemServiceServer(s.grpc, &systemService{Server: s})
	proto.RegisterLogServiceServer(s.grpc, &logService{Server: s})
	proto.RegisterSwipletServiceServer(s.grpc, &swipletService{Server: s})
	proto.RegisterREPLServiceServer(s.grpc, &replService{Server: s})
	s.mcp, s.mcpHTTP = s.newMCP()
	return s
}

func (s *Server) handler(prefix string, credential *Credential) http.Handler {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == MCPPath {
			s.serveMCP(w, r, credential)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), credentialKey{}, credential))
		contentType := r.Header.Get("Content-Type")
		switch {
		case isWebSocketGRPCRequest(r):
			s.serveWebSocket(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(contentType, contentTypeGRPCWeb):
			s.serveWeb(w, r)
		case r.ProtoMajor == 2 && strings.HasPrefix(contentType, contentTypeGRPC):
			s.grpc.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return handler
	}
	return http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "":
			r.URL.Path = "/"
		case r.URL.Path[0] != '/':
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	}))
}

func (s *Server) Serve(listener net.Listener, prefix string, credential *Credential) error {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		Handler:           http.NewCrossOriginProtection().Handler(s.handler(prefix, credential)),
		Protocols:         &protocols,
		ReadHeaderTimeout: headerTimeout,
	}
	s.access.Lock()
	if s.closed || s.draining != nil {
		s.access.Unlock()
		return listener.Close()
	}
	s.servers = append(s.servers, server)
	s.access.Unlock()
	tracked := &trackedListener{Listener: listener, conns: make(map[*trackedConn]struct{})}
	err := server.Serve(tracked)
	s.access.Lock()
	draining := s.draining
	s.access.Unlock()
	if draining != nil {
		<-draining
	}
	tracked.closeConns()
	s.access.Lock()
	s.servers = slices.DeleteFunc(s.servers, func(it *http.Server) bool { return it == server })
	s.access.Unlock()
	if errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) NewCredential(token string) *Credential {
	return &Credential{server: s, token: token}
}

func (c *Credential) SetToken(token string) {
	s := c.server
	s.access.Lock()
	if token == c.token {
		s.access.Unlock()
		return
	}
	c.token = token
	ended := s.takeSessions()
	var cancels []context.CancelCauseFunc
	for id, request := range s.requests {
		if request.credential == c {
			cancels = append(cancels, request.cancel)
			delete(s.requests, id)
		}
	}
	s.access.Unlock()
	for _, cancel := range cancels {
		cancel(errTokenReset)
	}
	ended()
}

func (c *Credential) authorized(token string) bool {
	c.server.access.Lock()
	current := c.token
	c.server.access.Unlock()
	return current != "" && subtle.ConstantTimeCompare([]byte(token), []byte(current)) == 1
}

func (s *Server) authorize(ctx context.Context) (context.Context, func(), error) {
	credential, found := ctx.Value(credentialKey{}).(*Credential)
	if !found {
		return nil, nil, errNoToken
	}
	if credential != nil {
		md, _ := metadata.FromIncomingContext(ctx)
		values := md.Get("authorization")
		if len(values) == 0 {
			return nil, nil, errNoToken
		}
		token, isBearer := strings.CutPrefix(values[0], "Bearer ")
		if !isBearer || !credential.authorized(token) {
			return nil, nil, errUnauthenticated
		}
	}
	ctx, cancel := context.WithCancelCause(ctx)
	s.access.Lock()
	if s.closed {
		s.access.Unlock()
		cancel(nil)
		return nil, nil, errStopped
	}
	s.sequence++
	id := s.sequence
	s.requests[id] = request{cancel: cancel, credential: credential}
	s.access.Unlock()
	return ctx, func() {
		s.access.Lock()
		delete(s.requests, id)
		s.access.Unlock()
		cancel(nil)
	}, nil
}

type authorizedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authorizedStream) Context() context.Context {
	return s.ctx
}

func (s *Server) cliSession() (*automation.REPL, error) {
	s.access.Lock()
	defer s.access.Unlock()
	if s.closed {
		return nil, errStopped
	}
	if s.session == nil {
		s.session = s.options.Swiplets.NewREPL(s.options.Dialog)
	}
	return s.session, nil
}

func (s *Server) takeSessions() func() {
	cli := s.session
	s.session = nil
	return func() {
		if cli != nil {
			cli.Close()
		}
		for session := range s.mcp.Sessions() {
			_ = session.Close()
		}
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.access.Lock()
	if s.closed || s.draining != nil {
		s.access.Unlock()
		return s.Close()
	}
	s.draining = make(chan struct{})
	servers := slices.Clone(s.servers)
	s.access.Unlock()
	var err error
	for _, server := range servers {
		err = E.Errors(err, server.Shutdown(ctx))
	}
	close(s.draining)
	return E.Errors(err, s.Close())
}

func (s *Server) Close() error {
	s.access.Lock()
	if s.closed {
		s.access.Unlock()
		return nil
	}
	s.closed = true
	ended := s.takeSessions()
	servers := slices.Clone(s.servers)
	s.access.Unlock()
	var err error
	for _, server := range servers {
		err = E.Errors(err, server.Close())
	}
	s.grpc.Stop()
	s.cancel()
	ended()
	return err
}

func serverVersion() string {
	info, loaded := debug.ReadBuildInfo()
	if !loaded || info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}
