package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/redis/go-redis/v9"

	riskv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/risk/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/config"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/grpcutil"
	"github.com/netologist/vrp-oneclick-deposit-platform/services/risk-svc/internal"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(config.Get("LOG_LEVEL", "info")),
	})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := config.Get("RISK_GRPC_ADDR", ":50054")
	redisAddr := config.Get("REDIS_ADDR", "localhost:6379")

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("redis ping failed", "addr", redisAddr, "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

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
