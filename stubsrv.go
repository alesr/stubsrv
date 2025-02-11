package stubsrv

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"

	"net/http"
	"net/http/httptest"
)

type Middleware func(http.Handler) http.Handler

// Key: "METHOD /path"
type routes map[string]routeInfo

type routeInfo struct {
	handler     http.Handler
	middlewares []Middleware
}

type Stub struct {
	logger  *slog.Logger
	mu      sync.Mutex
	routers routes
	baseURL string
	Server  *httptest.Server
	mux     *http.ServeMux
	closed  bool
}

func NewStub(logger *slog.Logger) *Stub {
	s := Stub{
		logger:  logger.WithGroup("stubsrv"),
		routers: make(routes),
	}

	s.AddHandler(http.MethodGet, "/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return &s
}

func (s *Stub) AddHandler(method, path string, handlerFunc http.HandlerFunc, middlewares ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Server != nil {
		panic("Cannot add handlers after the server has started.")
	}

	key := strings.ToUpper(method) + " " + path
	s.routers[key] = routeInfo{
		handler:     handlerFunc,
		middlewares: middlewares,
	}
	s.logger.Debug("Handler added", slog.String("method_path", key))
}

type startConfig struct {
	port string
}

type StartOption func(*startConfig)

func WithPort(port string) StartOption {
	return func(cfg *startConfig) {
		cfg.port = port
	}
}

func (s *Stub) Start(opts ...StartOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.mux != nil || s.Server != nil {
		return errors.New("stub server is already started")
	}

	var cfg startConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	s.mux = http.NewServeMux()

	byPath := map[string]routes{}
	for key, info := range s.routers {
		parts := strings.SplitN(key, " ", 2)
		method, path := parts[0], parts[1]

		if _, ok := byPath[path]; !ok {
			byPath[path] = make(routes)
		}
		byPath[path][method] = info
	}

	for path, methodMap := range byPath {
		localPath := path
		localMethodMap := methodMap

		s.mux.HandleFunc(localPath, func(w http.ResponseWriter, r *http.Request) {
			info, ok := localMethodMap[r.Method]
			if !ok {
				http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
				return
			}
			finalHandler := chainMiddleware(info.handler, info.middlewares...)
			finalHandler.ServeHTTP(w, r)
		})
	}

	listenAddr := net.JoinHostPort("", cfg.port)
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		// reset the mux to ensure a clean state
		s.mux = nil
		return fmt.Errorf("could not listen on %s: %w", listenAddr, err)
	}

	s.Server = &httptest.Server{
		Listener: ln,
		Config:   &http.Server{Handler: s.mux},
	}

	s.Server.Start()

	s.baseURL = s.Server.URL
	return nil
}

func (s *Stub) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Server != nil && !s.closed {
		s.Server.Close()
		s.closed = true
	}
}

func (s *Stub) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Server == nil || s.closed {
		return ""
	}
	return s.baseURL
}

func chainMiddleware(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
