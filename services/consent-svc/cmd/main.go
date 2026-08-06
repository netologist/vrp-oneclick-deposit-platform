package main

import (
	"context"
	"log/slog"
	"os"

	consentv1 "github.com/hozgan/vrp-demo/gen/consent/v1"
	"github.com/hozgan/vrp-demo/pkg/shared/config"
	"github.com/hozgan/vrp-demo/pkg/shared/db"
	"github.com/hozgan/vrp-demo/pkg/shared/grpcutil"
	"github.com/hozgan/vrp-demo/services/consent-svc/internal"
	"github.com/redis/go-redis/v9"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx := context.Background()
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
