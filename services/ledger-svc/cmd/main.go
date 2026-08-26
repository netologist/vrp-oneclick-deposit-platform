package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	ledgerv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/ledger/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/config"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/db"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/grpcutil"
	"github.com/netologist/vrp-oneclick-deposit-platform/services/ledger-svc/internal"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(config.Get("LOG_LEVEL", "info")),
	})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	slog.Info("ledger-svc starting", "addr", addr)
	if err := srv.Serve(ctx); err != nil {
		slog.Error("server exited", "err", err)
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
