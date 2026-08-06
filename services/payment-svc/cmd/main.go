package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	bankv1 "github.com/hozgan/vrp-demo/gen/bank/v1"
	consentv1 "github.com/hozgan/vrp-demo/gen/consent/v1"
	ledgerv1 "github.com/hozgan/vrp-demo/gen/ledger/v1"
	paymentv1 "github.com/hozgan/vrp-demo/gen/payment/v1"
	riskv1 "github.com/hozgan/vrp-demo/gen/risk/v1"
	"github.com/hozgan/vrp-demo/pkg/shared/config"
	"github.com/hozgan/vrp-demo/pkg/shared/db"
	"github.com/hozgan/vrp-demo/pkg/shared/grpcutil"
	"github.com/hozgan/vrp-demo/pkg/shared/idempotency"
	"github.com/hozgan/vrp-demo/services/payment-svc/internal"
	"github.com/redis/go-redis/v9"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(config.Get("LOG_LEVEL", "info")),
	})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := config.Get("PAYMENT_GRPC_ADDR", config.Get("PAYMENT_SVC_ADDR", ":50053"))
	if !strings.HasPrefix(addr, ":") {
		if host, port, ok := strings.Cut(addr, ":"); ok && (host == "" || host == "localhost" || host == "127.0.0.1") {
			addr = ":" + port
		} else if !strings.Contains(addr, ":") {
			addr = ":" + addr
		}
	}

	dbURL := config.Get("PAYMENT_DB_URL", "postgres://vrp:vrp@localhost:5432/payment?sslmode=disable")
	pool, err := db.NewPool(ctx, dbURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: config.Get("REDIS_ADDR", "localhost:6379"),
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("redis connect failed", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	consentAddr := config.Get("CONSENT_SVC_ADDR", "localhost:50052")
	riskAddr := config.Get("RISK_SVC_ADDR", "localhost:50054")
	ledgerAddr := config.Get("LEDGER_SVC_ADDR", "localhost:50055")
	bankAddr := config.Get("BANK_ADAPTER_ADDR", config.Get("BANK_SVC_ADDR", "localhost:50056"))

	consentConn, err := grpcutil.Dial(ctx, consentAddr)
	if err != nil {
		slog.Error("dial consent failed", "addr", consentAddr, "err", err)
		os.Exit(1)
	}
	defer consentConn.Close()

	riskConn, err := grpcutil.Dial(ctx, riskAddr)
	if err != nil {
		slog.Error("dial risk failed", "addr", riskAddr, "err", err)
		os.Exit(1)
	}
	defer riskConn.Close()

	ledgerConn, err := grpcutil.Dial(ctx, ledgerAddr)
	if err != nil {
		slog.Error("dial ledger failed", "addr", ledgerAddr, "err", err)
		os.Exit(1)
	}
	defer ledgerConn.Close()

	bankConn, err := grpcutil.Dial(ctx, bankAddr)
	if err != nil {
		slog.Error("dial bank failed", "addr", bankAddr, "err", err)
		os.Exit(1)
	}
	defer bankConn.Close()

	repo := internal.NewRepo(pool)
	orch := internal.NewOrchestrator(
		repo,
		idempotency.NewStore(rdb),
		consentv1.NewConsentServiceClient(consentConn),
		riskv1.NewRiskServiceClient(riskConn),
		bankv1.NewBankAdapterClient(bankConn),
		ledgerv1.NewLedgerServiceClient(ledgerConn),
	)
	handler := internal.NewHandler(orch, repo)

	brokers := splitCSV(config.Get("KAFKA_BROKERS", "localhost:19092"))
	relay := internal.NewOutboxRelay(repo, brokers)
	defer relay.Close()
	go relay.Run(ctx)

	srv := grpcutil.NewServer(addr)
	paymentv1.RegisterPaymentServiceServer(srv.GRPC, handler)
	srv.SetServing("", true)
	srv.SetServing(paymentv1.PaymentService_ServiceDesc.ServiceName, true)

	slog.Info("payment-svc starting",
		"addr", addr,
		"consent", consentAddr,
		"risk", riskAddr,
		"ledger", ledgerAddr,
		"bank", bankAddr,
		"kafka", brokers,
	)
	if err := srv.Serve(ctx); err != nil {
		slog.Error("grpc serve failed", "err", err)
		os.Exit(1)
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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
