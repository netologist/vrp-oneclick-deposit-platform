package main

import (
	"context"
	"log/slog"
	"os"

	ledgerv1 "github.com/hozgan/vrp-demo/gen/ledger/v1"
	"github.com/hozgan/vrp-demo/pkg/shared/config"
	"github.com/hozgan/vrp-demo/pkg/shared/db"
	"github.com/hozgan/vrp-demo/pkg/shared/grpcutil"
	"github.com/hozgan/vrp-demo/services/ledger-svc/internal"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx := context.Background()
	dbURL := config.Get("LEDGER_DB_URL", "postgres://vrp:vrp@localhost:5432/ledger?sslmode=disable")
	addr := config.Get("LEDGER_GRPC_ADDR", ":50055")

	pool, err := db.NewPool(ctx, dbURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	store := internal.NewStore(pool)
	svc := internal.NewServer(store)

	srv := grpcutil.NewServer(addr)
	ledgerv1.RegisterLedgerServiceServer(srv.GRPC, svc)
	srv.SetServing(ledgerv1.LedgerService_ServiceDesc.ServiceName, true)

	if err := srv.Serve(ctx); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}
