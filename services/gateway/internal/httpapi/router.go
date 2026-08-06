package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/hozgan/vrp-demo/pkg/shared/auth"
	"github.com/redis/go-redis/v9"
)

type RouterDeps struct {
	Handlers     *Handlers
	Tokens       *auth.TokenService
	Redis        *redis.Client
	RateLimitRPS int
}

func NewRouter(d RouterDeps) http.Handler {
	r := chi.NewRouter()

	r.Use(RequestIDMiddleware)
	r.Use(LoggerMiddleware)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RealIP)
	r.Use(chimw.Timeout(30 * time.Second))

	h := d.Handlers

	r.Get("/healthz/live", h.Live)
	r.Get("/healthz/ready", h.Ready)

	r.Route("/v1", func(r chi.Router) {
		// Public routes
		r.Post("/auth/token", h.IssueToken)
		r.Post("/merchants", h.RegisterMerchant)

		// Authenticated routes
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(d.Tokens))
			r.Use(RateLimitMiddleware(d.Redis, d.RateLimitRPS))

			r.Get("/merchants/{id}", h.GetMerchant)

			r.Post("/consents", h.CreateConsent)
			r.Get("/consents", h.ListConsents)
			r.Get("/consents/{id}", h.GetConsent)
			r.Delete("/consents/{id}", h.RevokeConsent)

			r.Post("/payments", h.InitiatePayment)
			r.Get("/payments/{id}", h.GetPayment)
			r.Post("/payments/{id}/retry", h.RetryPayment)
		})
	})

	return r
}
