package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/auth"
	"github.com/netologist/vrp-oneclick-deposit-platform/services/gateway/internal/httpapi"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestIDMiddleware(t *testing.T) {
	t.Parallel()

	handler := httpapi.RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := httpapi.RequestIDFrom(r.Context())
		assert.NotEmpty(t, reqID)
		w.Header().Set("X-Echo-Request-Id", reqID)
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("generates new request ID when missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotEmpty(t, rec.Header().Get("X-Request-Id"))
		assert.Equal(t, rec.Header().Get("X-Request-Id"), rec.Header().Get("X-Echo-Request-Id"))
	})

	t.Run("preserves existing X-Request-Id header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Request-Id", "existing-custom-id")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "existing-custom-id", rec.Header().Get("X-Request-Id"))
		assert.Equal(t, "existing-custom-id", rec.Header().Get("X-Echo-Request-Id"))
	})
}

func TestAuthMiddleware(t *testing.T) {
	t.Parallel()

	tokens := auth.NewTokenService("test-secret-key", time.Hour)
	validToken, _, err := tokens.Issue("merchant-123")
	require.NoError(t, err)

	protectedHandler := httpapi.AuthMiddleware(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		merchantID := httpapi.MerchantIDFrom(r.Context())
		assert.Equal(t, "merchant-123", merchantID)
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name         string
		authHeader   string
		expectedCode int
	}{
		{
			name:         "valid bearer token",
			authHeader:   "Bearer " + validToken,
			expectedCode: http.StatusOK,
		},
		{
			name:         "missing authorization header",
			authHeader:   "",
			expectedCode: http.StatusUnauthorized,
		},
		{
			name:         "non-bearer scheme",
			authHeader:   "Basic dXNlcjpwYXNz",
			expectedCode: http.StatusUnauthorized,
		},
		{
			name:         "empty bearer token",
			authHeader:   "Bearer ",
			expectedCode: http.StatusUnauthorized,
		},
		{
			name:         "invalid jwt token",
			authHeader:   "Bearer invalid.jwt.token",
			expectedCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()

			protectedHandler.ServeHTTP(rec, req)
			assert.Equal(t, tt.expectedCode, rec.Code)
		})
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	tokens := auth.NewTokenService("test-secret", time.Hour)
	validToken, _, err := tokens.Issue("merchant-rate-test")
	require.NoError(t, err)

	buildChain := func(client *redis.Client, rps int) http.Handler {
		return httpapi.AuthMiddleware(tokens)(
			httpapi.RateLimitMiddleware(client, rps)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			),
		)
	}

	t.Run("allows requests under the rate limit", func(t *testing.T) {
		chain := buildChain(rdb, 5)

		for i := range 5 {
			req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
			req.Header.Set("Authorization", "Bearer "+validToken)
			rec := httptest.NewRecorder()

			chain.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code, "request %d should be allowed", i+1)
		}
	})

	t.Run("rejects request when limit is exceeded with 429", func(t *testing.T) {
		merchantToken, _, err := tokens.Issue("merchant-burst-test")
		require.NoError(t, err)

		chain := buildChain(rdb, 3)

		// First 3 pass
		for range 3 {
			req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
			req.Header.Set("Authorization", "Bearer "+merchantToken)
			rec := httptest.NewRecorder()
			chain.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
		}

		// 4th request exceeds limit -> 429
		req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
		req.Header.Set("Authorization", "Bearer "+merchantToken)
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		assert.Equal(t, "1", rec.Header().Get("Retry-After"))
	})

	t.Run("nil redis passes through without error", func(t *testing.T) {
		chain := buildChain(nil, 5)

		req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		rec := httptest.NewRecorder()

		chain.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("unauthenticated request without merchantID passes through rate limiter", func(t *testing.T) {
		limiter := httpapi.RateLimitMiddleware(rdb, 5)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/public", nil)
		rec := httptest.NewRecorder()

		limiter.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}
