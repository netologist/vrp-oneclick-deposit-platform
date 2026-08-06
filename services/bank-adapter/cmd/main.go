package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	bankv1 "github.com/hozgan/vrp-demo/gen/bank/v1"
	"github.com/hozgan/vrp-demo/pkg/shared/config"
	"github.com/hozgan/vrp-demo/pkg/shared/grpcutil"
	svc "github.com/hozgan/vrp-demo/services/bank-adapter/internal"
	"github.com/hozgan/vrp-demo/services/bank-adapter/internal/mockbank"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	mockAddr := config.Get("MOCK_BANK_HTTP_ADDR", ":18080")
	baseURL := config.Get("BANK_HTTP_BASE_URL", "http://localhost:18080")
	grpcAddr := config.Get("BANK_GRPC_ADDR", ":50056")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mock := mockbank.New(mockAddr)
	if err := mock.Start(); err != nil {
		slog.Error("failed to start mock bank HTTP", "err", err)
		os.Exit(1)
	}

	adapter := svc.NewAdapter(baseURL)
	srv := grpcutil.NewServer(grpcAddr)
	bankv1.RegisterBankAdapterServer(srv.GRPC, adapter)
	srv.SetServing(bankv1.BankAdapter_ServiceDesc.ServiceName, true)

	slog.Info("bank-adapter starting",
		"grpc", grpcAddr,
		"mock_http", mock.Addr(),
		"bank_http_base_url", baseURL,
	)

	err := srv.Serve(ctx)

	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = mock.Shutdown(shCtx)

	if err != nil {
		slog.Error("gRPC server stopped", "err", err)
		os.Exit(1)
	}
}
