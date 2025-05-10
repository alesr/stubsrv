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

		assert.Len(t, got.routers, 0)
	})
}

func TestStub_AddHandler(t *testing.T) {
	t.Parallel()

	t.Run("successfully adds handler with no middleware", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		handlerFn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

		stub.AddHandler("GET", "/test", handlerFn)

		assert.Len(t, stub.routers, 1) // only the newly added route
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

		assert.Len(t, stub.routers, 1)
		route, exists := stub.routers["POST /test-with-middleware"]
		assert.True(t, exists)

		assert.Len(t, route.middlewares, 1)
	})

	t.Run("method is converted to uppercase", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())

		handlerFn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

		stub.AddHandler("get", "/foo", handlerFn)

		assert.Len(t, stub.routers, 1)
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

		assert.Len(t, stub.routers, 3)
		_, exists1 := stub.routers["GET /first"]
		_, exists2 := stub.routers["POST /second"]
		_, exists3 := stub.routers["PUT /third"]
		assert.True(t, exists1)
		assert.True(t, exists2)
		assert.True(t, exists3)
	})

	t.Run("successfully adds handler after server has started", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger())
		require.NoError(t, stub.Start())
		defer stub.Close()

		handlerFn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})

		assert.NotPanics(t, func() {
			stub.AddHandler("GET", "/after-start", handlerFn)
		})

		resp, err := http.Get(stub.URL() + "/after-start")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusTeapot, resp.StatusCode)
	})
}

func TestStub_Start(t *testing.T) {
	t.Parallel()

	t.Run("starts server successfully with dynamic port", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger(), WithPort("0"))
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

		parsedURL, err := url.Parse(stub.baseURL)
		require.NoError(t, err)
		assert.NotEqual(t, "0", parsedURL.Port())
		assert.NotEqual(t, defaultPort, parsedURL.Port())

		resp, err := http.Get(stub.baseURL + "/test")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.True(t, handlerCalled)
	})

	t.Run("starts server with specific port", func(t *testing.T) {
		t.Parallel()

		stub := NewStub(noopLogger(), WithPort("1234"))

		stub.AddHandler(http.MethodGet, "/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		err := stub.Start()
		defer stub.Server.Close()

		assert.NoError(t, err)
		assert.NotNil(t, stub.Server)

		parsedURL, err := url.Parse(stub.baseURL)
		assert.NoError(t, err)
		assert.Equal(t, "1234", parsedURL.Port())

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

		firstStub := NewStub(noopLogger(), WithPort(testPort))
		err := firstStub.Start()
		require.NoError(t, err)
		defer firstStub.Server.Close()

		secondStub := NewStub(logger, WithPort(testPort))
		err = secondStub.Start() // same port
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

func TestStub_ControlAddHandler(t *testing.T) {
	t.Parallel()

	stub := NewStub(noopLogger())
	require.NoError(t, stub.Start())
	defer stub.Close()

	// prepare control-plane payload
	payload := `{
		"method": "GET",
		"path": "/dynamic",
		"status": 201,
		"body": "created",
		"headers": { "Content-Type": "text/plain" }
	}`

	resp, err := http.Post(stub.URL()+"/_control/handlers", "application/json", strings.NewReader(payload))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// verify the dynamic route now exists and responds as configured
	dynResp, err := http.Get(stub.URL() + "/dynamic")
	require.NoError(t, err)
	defer dynResp.Body.Close()
	bodyBytes, _ := io.ReadAll(dynResp.Body)

	assert.Equal(t, http.StatusCreated, dynResp.StatusCode)
	assert.Equal(t, "created", string(bodyBytes))
	assert.Equal(t, "text/plain", dynResp.Header.Get("Content-Type"))
}

func TestStub_Dispatch(t *testing.T) {
	t.Parallel()

	stub := NewStub(noopLogger())
	stub.AddHandler(http.MethodGet, "/foo", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// exact match
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	stub.dispatch(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// path exists but wrong method
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/foo", nil)
	stub.dispatch(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	// unknown path
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/unknown", nil)
	stub.dispatch(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStub_TemplateRouteMatching(t *testing.T) {
	t.Parallel()

	stub := NewStub(noopLogger())
	require.NoError(t, stub.Start())
	defer stub.Close()

	// Register template route (with query constraint) via control endpoint.
	payload := `{
		"method": "GET",
		"path": "/users/:id/orders/:orderId",
		"query": { "status": "shipped" },
		"status": 200,
		"body": "matched"
	}`
	respCtl, err := http.Post(stub.URL()+"/_control/handlers", "application/json", strings.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, respCtl.StatusCode)

	base := stub.URL() + "/users/42/orders/24"

	// 1) Correct method, path and query → 200
	okURL := base + "?status=shipped"
	resp, err := http.Get(okURL)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "matched", string(body))

	// 2) Wrong query value → 404
	badQueryURL := base + "?status=pending"
	resp2, err := http.Get(badQueryURL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp2.StatusCode)

	// 3) Correct path/query but wrong method → 405
	reqPost, _ := http.NewRequest(http.MethodPost, okURL, nil)
	resp3, err := http.DefaultClient.Do(reqPost)
	require.NoError(t, err)
	assert.Equal(t, http.StatusMethodNotAllowed, resp3.StatusCode)
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}
