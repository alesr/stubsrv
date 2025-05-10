package stubsrv

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

const defaultPort = "8008"

type stubConfig struct{ port string }

type Option func(*stubConfig)

func WithPort(port string) Option {
	return func(cfg *stubConfig) {
		cfg.port = port
	}
}

// Key: "METHOD /path"
type routes map[string]routeInfo

type routeInfo struct {
	handler     http.Handler
	middlewares []Middleware
}

type templateRoute struct {
	method   string
	segments []string
	queries  map[string]string
	info     routeInfo
}

type Stub struct {
	logger         *slog.Logger
	mu             sync.Mutex
	routers        routes
	templateRoutes []templateRoute
	baseURL        string
	port           string
	Server         *httptest.Server
	mux            *http.ServeMux
	closed         bool
}

func NewStub(logger *slog.Logger, opts ...Option) *Stub {
	s := Stub{
		logger:  logger.WithGroup("stubsrv"),
		routers: make(routes),
		port:    defaultPort,
	}

	var cfg stubConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	s.port = cfg.port

	s.mux = http.NewServeMux()

	// control-plane endpoint
	s.mux.HandleFunc("/_control/handlers", s.controlAddHandler)

	// readiness probe
	s.mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// dispatcher for user routes
	s.mux.HandleFunc("/", s.dispatch)

	return &s
}

func (s *Stub) AddHandler(method, path string, handlerFunc http.HandlerFunc, middlewares ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		panic("cannot add handlers on a closed stub server")
	}

	upperMethod := strings.ToUpper(method)

	if strings.Contains(path, ":") {
		tr := templateRoute{
			method:   upperMethod,
			segments: strings.Split(strings.Trim(path, "/"), "/"),
			queries:  nil,
			info: routeInfo{
				handler:     handlerFunc,
				middlewares: middlewares,
			},
		}
		s.templateRoutes = append(s.templateRoutes, tr)
		s.logger.Debug("Template handler added", slog.String("method_path", upperMethod+" "+path))
		return
	}

	key := upperMethod + " " + path
	s.routers[key] = routeInfo{
		handler:     handlerFunc,
		middlewares: middlewares,
	}
	s.logger.Debug("Handler added", slog.String("method_path", key))
}

func (s *Stub) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Server != nil {
		return errors.New("stub server is already started")
	}

	listenAddr := net.JoinHostPort("", s.port)
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
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

type DynamicHandlerSpec struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Status  int               `json:"status"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers"`
}

func (s *Stub) controlAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var spec DynamicHandlerSpec

	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if spec.Method == "" || spec.Path == "" {
		http.Error(w, "method and path are required", http.StatusBadRequest)
		return
	}
	if spec.Status == 0 {
		spec.Status = http.StatusOK
	}

	responseHandler := func(w http.ResponseWriter, r *http.Request) {
		for k, v := range spec.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(spec.Status)
		if spec.Body != "" {
			_, _ = w.Write([]byte(spec.Body))
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.Contains(spec.Path, ":") || len(spec.Query) > 0 {
		tr := templateRoute{
			method:   strings.ToUpper(spec.Method),
			segments: strings.Split(strings.Trim(spec.Path, "/"), "/"),
			queries:  spec.Query,
			info: routeInfo{
				handler: http.HandlerFunc(responseHandler),
			},
		}
		s.templateRoutes = append(s.templateRoutes, tr)
	} else {
		key := strings.ToUpper(spec.Method) + " " + spec.Path
		s.routers[key] = routeInfo{
			handler: http.HandlerFunc(responseHandler),
		}
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Stub) dispatch(w http.ResponseWriter, r *http.Request) {
	key := strings.ToUpper(r.Method) + " " + r.URL.Path

	s.mu.Lock()
	info, ok := s.routers[key]
	if ok {
		final := chainMiddleware(info.handler, info.middlewares...)
		s.mu.Unlock()
		final.ServeHTTP(w, r)
		return
	}

	for _, tr := range s.templateRoutes {
		if tr.method != r.Method {
			continue
		}
		if !pathMatch(tr.segments, r.URL.Path) {
			continue
		}
		if !queryMatch(tr.queries, r.URL.Query()) {
			continue
		}

		final := chainMiddleware(tr.info.handler, tr.info.middlewares...)
		s.mu.Unlock()
		final.ServeHTTP(w, r)
		return
	}

	var methodMismatch bool
	targetPath := " " + r.URL.Path

	for k := range s.routers {
		if strings.HasSuffix(k, targetPath) {
			methodMismatch = true
			break
		}
	}

	if !methodMismatch {
		for _, tr := range s.templateRoutes {
			if !pathMatch(tr.segments, r.URL.Path) {
				continue
			}
			if !queryMatch(tr.queries, r.URL.Query()) {
				continue
			}
			methodMismatch = true
			break
		}
	}
	s.mu.Unlock()

	if methodMismatch {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	http.NotFound(w, r)
}
