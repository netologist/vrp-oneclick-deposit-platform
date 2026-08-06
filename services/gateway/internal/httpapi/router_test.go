package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hozgan/vrp-demo/pkg/shared/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_HealthRoutes(t *testing.T) {
	ts := auth.NewTokenService("test-secret", time.Hour)
	handlers := &Handlers{Tokens: ts}
	router := NewRouter(RouterDeps{
		Handlers: handlers,
		Tokens:   ts,
	})

	t.Run("healthz live", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/healthz/live", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"status":"ok"`)
	})

	t.Run("healthz ready", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/healthz/ready", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"status":"ready"`)
	})
}

func TestGateway_AuthMiddleware(t *testing.T) {
	ts := auth.NewTokenService("test-secret", time.Hour)
	handlers := &Handlers{Tokens: ts}
	router := NewRouter(RouterDeps{
		Handlers: handlers,
		Tokens:   ts,
	})

	t.Run("protected endpoint without token returns 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/merchants/123", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("protected endpoint with valid token proceeds", func(t *testing.T) {
		token, _, err := ts.Issue("merch-123")
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/v1/merchants/123", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		// Will reach GetMerchant handler; grpc client is nil so panics or returns 500
		assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestGateway_IssueToken_Validation(t *testing.T) {
	ts := auth.NewTokenService("test-secret", time.Hour)
	handlers := &Handlers{Tokens: ts}
	router := NewRouter(RouterDeps{
		Handlers: handlers,
		Tokens:   ts,
	})

	t.Run("empty json body returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/v1/auth/token", bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}
