package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	consentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/consent/v1"
	merchantv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/merchant/v1"
	paymentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/payment/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/auth"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/config"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/grpcutil"
	"github.com/netologist/vrp-oneclick-deposit-platform/services/gateway/internal/httpapi"
	"github.com/redis/go-redis/v9"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	httpAddr := config.Get("GATEWAY_HTTP_ADDR", ":8080")
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		env := config.Get("ENVIRONMENT", "")
		appEnv := config.Get("APP_ENV", "")
		if env == "production" || env == "prod" || appEnv == "production" {
			slog.Error("JWT_SECRET is required in production environment")
			os.Exit(1)
		}
		slog.Warn("JWT_SECRET unset; using insecure default (development only)")
		jwtSecret = "super-secret-jwt-key"
	}
	jwtTTL := config.GetDuration("JWT_TTL", time.Hour)
	redisAddr := config.Get("REDIS_ADDR", "localhost:6379")
	rateLimit := config.GetInt("GATEWAY_RATE_LIMIT_RPS", 100)
	merchantAddr := config.Get("MERCHANT_SVC_ADDR", "localhost:50051")
	consentAddr := config.Get("CONSENT_SVC_ADDR", "localhost:50052")
	paymentAddr := config.Get("PAYMENT_SVC_ADDR", "localhost:50053")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	merchantConn, err := grpcutil.Dial(dialCtx, merchantAddr)
	if err != nil {
		slog.Error("dial_merchant_failed", "addr", merchantAddr, "err", err)
		os.Exit(1)
	}
	defer merchantConn.Close()

	consentConn, err := grpcutil.Dial(dialCtx, consentAddr)
	if err != nil {
		slog.Error("dial_consent_failed", "addr", consentAddr, "err", err)
		os.Exit(1)
	}
	defer consentConn.Close()

	paymentConn, err := grpcutil.Dial(dialCtx, paymentAddr)
	if err != nil {
		slog.Error("dial_payment_failed", "addr", paymentAddr, "err", err)
		os.Exit(1)
	}
	defer paymentConn.Close()

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Warn("redis_unavailable_rate_limit_disabled", "addr", redisAddr, "err", err)
		_ = rdb.Close()
		rdb = nil
	} else {
		defer rdb.Close()
	}

	tokens := auth.NewTokenService(jwtSecret, jwtTTL)
	handlers := &httpapi.Handlers{
		Tokens:   tokens,
		Merchant: merchantv1.NewMerchantServiceClient(merchantConn),
		Consent:  consentv1.NewConsentServiceClient(consentConn),
		Payment:  paymentv1.NewPaymentServiceClient(paymentConn),
	}

	router := httpapi.NewRouter(httpapi.RouterDeps{
		Handlers:     handlers,
		Tokens:       tokens,
		Redis:        rdb,
		RateLimitRPS: rateLimit,
	})

	srv := &http.Server{
		Addr:              httpAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("gateway_listening", "addr", httpAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		slog.Info("gateway_shutting_down")
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("gateway_server_error", "err", err)
			os.Exit(1)
		}
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("gateway_shutdown_error", "err", err)
		os.Exit(1)
	}
}
