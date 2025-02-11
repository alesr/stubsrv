package stubsrv

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExampleStub(t *testing.T) {
	t.Parallel()

	stub := NewStub(noopLogger())

	// add GET route
	stub.AddHandler(http.MethodGet, "/foo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Foo"))
	})

	// add POST route
	stub.AddHandler(http.MethodPost, "/echo", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_, _ = w.Write(bodyBytes)
	})

	stub.Start()
	defer stub.Close()

	// call endpoints

	respGet, err := http.Get(stub.URL() + "/foo")
	require.NoError(t, err)

	defer respGet.Body.Close()
	bodyGet, _ := io.ReadAll(respGet.Body)
	fmt.Println(string(bodyGet))

	respPost, err := http.Post(stub.URL()+"/echo", "text/plain", strings.NewReader("Bar"))
	require.NoError(t, err)

	defer respPost.Body.Close()
	bodyPost, _ := io.ReadAll(respPost.Body)
	fmt.Println(string(bodyPost))

	// Output:
	// Foo
	// Bar
}

func TestStub_New(t *testing.T) {
	t.Parallel()

	t.Run("fields are correctly initialized", func(t *testing.T) {
		t.Parallel()

		got := NewStub(noopLogger())

		assert.NotNil(t, got.logger)
		assert.Nil(t, got.Server)
		assert.False(t, got.closed)

		assert.Len(t, got.routers, 1)
		route, exists := got.routers["GET /readyz"]
		assert.True(t, exists)
		assert.NotNil(t, route.handler)
		assert.Empty(t, route.middlewares)
	})
}

func TestStub_AddHandler(t *testing.T) {
	t.Parallel()

	t.Run("successfully adds handler with no middleware", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		handlerFn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

		stub.AddHandler("GET", "/test", handlerFn)

		assert.Len(t, stub.routers, 2) // we already have a built-in readyz endpoint
		route, exists := stub.routers["GET /test"]
		assert.True(t, exists)
		assert.NotNil(t, route.handler)
		assert.Empty(t, route.middlewares)
	})

	t.Run("successfully adds handler with middleware", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		handlerFn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

		middleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		}

		stub.AddHandler("POST", "/test-with-middleware", handlerFn, middleware)

		assert.Len(t, stub.routers, 2)
		route, exists := stub.routers["POST /test-with-middleware"]
		assert.True(t, exists)

		assert.Len(t, route.middlewares, 1)
	})

	t.Run("method is converted to uppercase", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		handlerFn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

		stub.AddHandler("get", "/foo", handlerFn)

		assert.Len(t, stub.routers, 2)
		_, exists := stub.routers["GET /foo"]
		assert.True(t, exists)
	})

	t.Run("multiple handlers can be added", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		handlerFn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

		stub.AddHandler("GET", "/first", handlerFn)
		stub.AddHandler("POST", "/second", handlerFn)
		stub.AddHandler("put", "/third", handlerFn)

		assert.Len(t, stub.routers, 4)
		_, exists1 := stub.routers["GET /first"]
		_, exists2 := stub.routers["POST /second"]
		_, exists3 := stub.routers["PUT /third"]
		assert.True(t, exists1)
		assert.True(t, exists2)
		assert.True(t, exists3)
	})

	t.Run("panics when adding handler after server has started", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		handlerFn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

		stub.Server = httptest.NewServer(http.NewServeMux())
		defer stub.Server.Close()

		assert.Panics(t, func() {
			stub.AddHandler("GET", "/after-start", handlerFn)
		})
	})
}

func TestStub_Start(t *testing.T) {
	t.Parallel()

	t.Run("starts server successfully with dynamic port", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())
		var handlerCalled bool

		stub.AddHandler(http.MethodGet, "/test", func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		})

		err := stub.Start()
		defer stub.Server.Close()

		assert.NoError(t, err)
		assert.NotNil(t, stub.Server)
		assert.NotNil(t, stub.mux)
		assert.NotEmpty(t, stub.baseURL)

		resp, err := http.Get(stub.baseURL + "/test")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.True(t, handlerCalled)
	})

	t.Run("starts server with specific port", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		stub.AddHandler(http.MethodGet, "/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		err := stub.Start(WithPort("8008"))
		defer stub.Server.Close()

		assert.NoError(t, err)
		assert.NotNil(t, stub.Server)

		parsedURL, err := url.Parse(stub.baseURL)
		assert.NoError(t, err)
		assert.Equal(t, "8008", parsedURL.Port())

		resp, err := http.Get(stub.baseURL + "/test")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

	})

	t.Run("server handles method not allowed", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		// GET endpoint
		stub.AddHandler(http.MethodGet, "/method-test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		err := stub.Start()
		defer stub.Server.Close()
		assert.NoError(t, err)

		// POST request to trigger method not allowed
		req, _ := http.NewRequest(http.MethodPost, stub.baseURL+"/method-test", nil)
		client := &http.Client{}
		resp, err := client.Do(req)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	})

	t.Run("fails to start on occupied port", func(t *testing.T) {
		t.Parallel()

		const testPort = "12345"
		logger := noopLogger()

		firstStub := NewStub(noopLogger())
		err := firstStub.Start(WithPort(testPort))
		require.NoError(t, err)
		defer firstStub.Server.Close()

		secondStub := NewStub(logger)
		err = secondStub.Start(WithPort(testPort)) // same port
		require.Error(t, err)
		assert.Contains(t, err.Error(), "could not listen")

		if secondStub.Server != nil {
			secondStub.Server.Close()
		}
	})

	t.Run("returns error when attempting to start twice", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())
		err := stub.Start()
		defer stub.Server.Close()
		assert.NoError(t, err)

		err = stub.Start()
		assert.Error(t, err)
	})

	t.Run("middleware chain is correctly applied", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		var middlewareCalled bool
		middleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				middlewareCalled = true
				next.ServeHTTP(w, r)
			})
		}

		stub.AddHandler(http.MethodGet, "/middleware-test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}, middleware)

		err := stub.Start()
		defer stub.Server.Close()
		assert.NoError(t, err)

		resp, err := http.Get(stub.baseURL + "/middleware-test")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.True(t, middlewareCalled)
	})
}

func TestStub_Close(t *testing.T) {
	t.Parallel()

	t.Run("closes the server and sets closed flag", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		err := stub.Start()
		require.NoError(t, err)
		require.NotNil(t, stub.Server)
		require.False(t, stub.closed)

		stub.Close()

		assert.True(t, stub.closed)
	})

	t.Run("does nothing when server is not started", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		assert.NotPanics(t, func() {
			stub.Close()
		})
	})

	t.Run("safe to call multiple times", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		err := stub.Start()
		require.NoError(t, err)

		stub.Close()
		assert.True(t, stub.closed)

		assert.NotPanics(t, func() {
			stub.Close()
			stub.Close()
		})
		assert.True(t, stub.closed)
	})

	t.Run("server becomes unreachable after close", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		stub.AddHandler(http.MethodGet, "/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		err := stub.Start()
		require.NoError(t, err)

		resp, err := http.Get(stub.baseURL + "/test")
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		stub.Close()

		_, err = http.Get(stub.baseURL + "/test")
		assert.Error(t, err)
	})
}

func TestStub_URL(t *testing.T) {
	t.Parallel()

	t.Run("returns empty string when server is not started", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		url := stub.URL()
		assert.Equal(t, "", url)
	})

	t.Run("returns server URL when server is running", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		err := stub.Start()
		require.NoError(t, err)
		defer stub.Close()

		url := stub.URL()
		assert.NotEmpty(t, url)
		assert.Equal(t, stub.baseURL, url)

		resp, err := http.Get(url + "/readyz")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("returns empty string after server is closed", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		err := stub.Start()
		require.NoError(t, err)

		urlBeforeClose := stub.URL()
		assert.NotEmpty(t, urlBeforeClose)

		stub.Close()

		urlAfterClose := stub.URL()
		assert.Equal(t, "", urlAfterClose)
	})
}

func TestChainMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("no middleware", func(t *testing.T) {
		t.Parallel()

		var handlerCalled bool
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
		})

		chained := chainMiddleware(handler)

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		chained.ServeHTTP(w, r)

		assert.True(t, handlerCalled)
	})

	t.Run("single middleware", func(t *testing.T) {
		t.Parallel()

		var handlerCalled, middlewareCalled bool
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
		})

		middleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				middlewareCalled = true
				next.ServeHTTP(w, r)
			})
		}

		chained := chainMiddleware(handler, middleware)

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		chained.ServeHTTP(w, r)

		assert.True(t, handlerCalled)
		assert.True(t, middlewareCalled)
	})

	t.Run("multiple middleware in correct order", func(t *testing.T) {
		t.Parallel()

		var execOrder []string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			execOrder = append(execOrder, "handler")
		})

		middleware1 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				execOrder = append(execOrder, "middleware1-before")
				next.ServeHTTP(w, r)
				execOrder = append(execOrder, "middleware1-after")
			})
		}

		middleware2 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				execOrder = append(execOrder, "middleware2-before")
				next.ServeHTTP(w, r)
				execOrder = append(execOrder, "middleware2-after")
			})
		}

		chained := chainMiddleware(handler, middleware1, middleware2)

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		chained.ServeHTTP(w, r)

		expected := []string{
			"middleware1-before",
			"middleware2-before",
			"handler",
			"middleware2-after",
			"middleware1-after",
		}
		assert.Equal(t, expected, execOrder)
	})

	t.Run("middleware can modify request and response", func(t *testing.T) {
		t.Parallel()

		var handlerReceivedHeader string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerReceivedHeader = r.Header.Get("X-Test")
			w.Header().Set("X-Response", "original")
			w.WriteHeader(http.StatusOK)
		})

		modifyingMiddleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set("X-Test", "modified")
				next.ServeHTTP(w, r)
				w.Header().Set("X-Response", "modified")
			})
		}

		chained := chainMiddleware(handler, modifyingMiddleware)

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		chained.ServeHTTP(w, r)

		assert.Equal(t, "modified", handlerReceivedHeader)
		assert.Equal(t, "modified", w.Header().Get("X-Response"))
	})

	t.Run("middleware can short-circuit handler execution", func(t *testing.T) {
		t.Parallel()

		var handlerCalled bool
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
		})

		shortCircuitingMiddleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				// not calling next.ServeHTTP intentionally
			})
		}

		chained := chainMiddleware(handler, shortCircuitingMiddleware)

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		chained.ServeHTTP(w, r)

		assert.False(t, handlerCalled)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}
