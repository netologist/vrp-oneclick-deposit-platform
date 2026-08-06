package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/auth"
	"github.com/stretchr/testify/assert"
)

func TestGateway_SwaggerRoutes(t *testing.T) {
	ts := auth.NewTokenService("test-secret", time.Hour)
	handlers := &Handlers{Tokens: ts}
	router := NewRouter(RouterDeps{
		Handlers: handlers,
		Tokens:   ts,
	})

	t.Run("openapi spec yaml", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/docs/openapi.yaml", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "openapi: \"3.0.3\"")
	})

	t.Run("swagger ui html", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/docs", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "SwaggerUIBundle")
	})
}
