package stubsrv

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
