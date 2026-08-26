package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/auth"
	"github.com/redis/go-redis/v9"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota + 1
	ctxMerchantID
)

func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

func MerchantIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxMerchantID).(string); ok {
		return v
	}
	return ""
}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestID, id)
}

func withMerchantID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxMerchantID, id)
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(withRequestID(r.Context(), id)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFrom(r.Context()),
			"merchant_id", MerchantIDFrom(r.Context()),
		)
	})
}

func AuthMiddleware(tokens *auth.TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" || !strings.HasPrefix(strings.ToLower(h), "bearer ") {
				writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid Authorization header")
				return
			}
			raw := strings.TrimSpace(h[len("Bearer "):])
			if raw == "" {
				writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "missing bearer token")
				return
			}
			claims, err := tokens.Parse(raw)
			if err != nil {
				writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
				return
			}
			if claims.MerchantID == "" {
				writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "token missing merchant_id")
				return
			}
			ctx := withMerchantID(r.Context(), claims.MerchantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// rateLimitLua is an atomic Lua script that increments the key and sets a 2-second TTL on first hit.
var rateLimitLua = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local current = redis.call('INCR', key)
if current == 1 then
    redis.call('EXPIRE', key, 2)
end
return current
`)

// RateLimitMiddleware enforces limitPerSec requests/second per merchant via an atomic Redis Lua script.
// If Redis is nil or unavailable, the request is permitted (fail-open for resilience).
func RateLimitMiddleware(rdb *redis.Client, limitPerSec int) func(http.Handler) http.Handler {
	if limitPerSec <= 0 {
		limitPerSec = 100
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rdb == nil {
				next.ServeHTTP(w, r)
				return
			}
			merchantID := MerchantIDFrom(r.Context())
			if merchantID == "" {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			window := time.Now().Unix()
			key := "ratelimit:" + merchantID + ":" + strconv.FormatInt(window, 10)

			n, err := rateLimitLua.Run(ctx, rdb, []string{key}, limitPerSec).Int64()
			if err != nil {
				slog.Warn("rate_limit_redis_error", "err", err, "merchant_id", merchantID)
				next.ServeHTTP(w, r)
				return
			}
			if n > int64(limitPerSec) {
				w.Header().Set("Retry-After", "1")
				writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
