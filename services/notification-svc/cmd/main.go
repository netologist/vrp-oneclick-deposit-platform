package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	merchantv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/merchant/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/config"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/grpcutil"
	"github.com/netologist/vrp-oneclick-deposit-platform/services/notification-svc/internal"
	"github.com/redis/go-redis/v9"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(config.Get("LOG_LEVEL", "info")),
	}))
	slog.SetDefault(log)

	brokers := splitCSV(config.Get("KAFKA_BROKERS", "localhost:19092"))
	redisAddr := config.Get("REDIS_ADDR", "localhost:6379")
	merchantAddr := config.Get("MERCHANT_SVC_ADDR", "localhost:50051")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Error("redis ping failed", "addr", redisAddr, "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := grpcutil.Dial(dialCtx, merchantAddr)
	if err != nil {
		log.Error("dial merchant service failed", "addr", merchantAddr, "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	merchants := merchantv1.NewMerchantServiceClient(conn)
	reader := internal.NewReader(brokers)
	dlq := internal.NewDLQWriter(brokers)
	defer func() {
		if err := dlq.Close(); err != nil {
			log.Warn("dlq writer close", "err", err)
		}
	}()

	deliverer := internal.NewDeliverer(merchants, dlq, log)
	consumer := &internal.Consumer{
		Reader:    reader,
		Redis:     rdb,
		Deliverer: deliverer,
		Log:       log,
	}
	defer func() {
		if err := consumer.Close(); err != nil {
			log.Warn("kafka reader close", "err", err)
		}
	}()

	log.Info("notification-svc starting",
		"kafka_brokers", brokers,
		"redis", redisAddr,
		"merchant_svc", merchantAddr,
	)

	if err := consumer.Run(ctx); err != nil {
		log.Error("consumer stopped with error", "err", err)
		os.Exit(1)
	}
	log.Info("notification-svc stopped")
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
	switch strings.ToLower(s) {
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
