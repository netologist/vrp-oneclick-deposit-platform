package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/redis/go-redis/v9"

	riskv1 "github.com/hozgan/vrp-demo/gen/risk/v1"
	"github.com/hozgan/vrp-demo/pkg/shared/config"
	"github.com/hozgan/vrp-demo/pkg/shared/grpcutil"
	"github.com/hozgan/vrp-demo/services/risk-svc/internal"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	addr := config.Get("RISK_GRPC_ADDR", ":50054")
	redisAddr := config.Get("REDIS_ADDR", "localhost:6379")

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("redis ping failed", "addr", redisAddr, "err", err)
		os.Exit(1)
	}

	engine := internal.NewEngine(rdb)
	handler := internal.NewHandler(engine, rdb)

	srv := grpcutil.NewServer(addr)
	riskv1.RegisterRiskServiceServer(srv.GRPC, handler)
	srv.SetServing(riskv1.RiskService_ServiceDesc.ServiceName, true)

	slog.Info("risk-svc listening", "addr", addr, "redis", redisAddr)
	if err := srv.Serve(ctx); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
