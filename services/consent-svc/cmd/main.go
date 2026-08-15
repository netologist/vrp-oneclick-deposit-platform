package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	consentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/consent/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/config"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/db"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/grpcutil"
	"github.com/netologist/vrp-oneclick-deposit-platform/services/consent-svc/internal"
	"github.com/redis/go-redis/v9"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(config.Get("LOG_LEVEL", "info")),
	})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dbURL := config.Get("CONSENT_DB_URL", "postgres://vrp:vrp@localhost:5432/consent?sslmode=disable")
	addr := config.Get("CONSENT_GRPC_ADDR", ":50052")
	redisAddr := config.Get("REDIS_ADDR", "localhost:6379")

	pool, err := db.NewPool(ctx, dbURL)
	if err != nil {
		slog.Error("connect database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	var rdb *redis.Client
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := client.Ping(ctx).Err(); err != nil {
		slog.Warn("redis unavailable; continuing without cache", "addr", redisAddr, "err", err)
		_ = client.Close()
	} else {
		rdb = client
		defer func() { _ = rdb.Close() }()
		slog.Info("redis cache enabled", "addr", redisAddr)
	}

	repo := internal.NewRepo(pool)
	svc := internal.NewService(repo, rdb)
	handler := internal.NewHandler(svc)

	srv := grpcutil.NewServer(addr)
	consentv1.RegisterConsentServiceServer(srv.GRPC, handler)
	srv.SetServing(consentv1.ConsentService_ServiceDesc.ServiceName, true)

	slog.Info("consent-svc starting", "addr", addr)
	if err := srv.Serve(ctx); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
