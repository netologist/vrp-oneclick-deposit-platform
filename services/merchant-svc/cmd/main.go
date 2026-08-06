package main

import (
	"context"
	"log/slog"
	"os"

	merchantv1 "github.com/hozgan/vrp-demo/gen/merchant/v1"
	"github.com/hozgan/vrp-demo/pkg/shared/config"
	"github.com/hozgan/vrp-demo/pkg/shared/db"
	"github.com/hozgan/vrp-demo/pkg/shared/grpcutil"
	"github.com/hozgan/vrp-demo/services/merchant-svc/internal"
)

const serviceName = "merchant.v1.MerchantService"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx := context.Background()

	dbURL := config.Get("MERCHANT_DB_URL", "postgres://vrp:vrp@localhost:5432/merchant?sslmode=disable")
	addr := config.Get("MERCHANT_GRPC_ADDR", ":50051")

	pool, err := db.NewPool(ctx, dbURL)
	if err != nil {
		slog.Error("connect database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := internal.NewRepo(pool)
	svc := internal.NewService(repo)
	handler := internal.NewHandler(svc)

	srv := grpcutil.NewServer(addr)
	merchantv1.RegisterMerchantServiceServer(srv.GRPC, handler)
	srv.SetServing(serviceName, true)

	slog.Info("merchant service starting", "addr", addr)
	if err := srv.Serve(ctx); err != nil {
		slog.Error("grpc serve failed", "err", err)
		os.Exit(1)
	}
}
