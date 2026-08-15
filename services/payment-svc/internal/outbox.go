package internal

import (
	"context"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"
)

// OutboxRelay polls the transactional outbox and publishes to Kafka.
type OutboxRelay struct {
	repo     *Repo
	writer   *kafka.Writer
	interval time.Duration
}

func NewOutboxRelay(repo *Repo, brokers []string) *OutboxRelay {
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireAll,
		Async:        false,
	}
	return &OutboxRelay{
		repo:     repo,
		writer:   w,
		interval: 200 * time.Millisecond,
	}
}

func (r *OutboxRelay) Close() error {
	return r.writer.Close()
}

// Run blocks until ctx is cancelled.
func (r *OutboxRelay) Run(ctx context.Context) {
	slog.Info("outbox relay started", "interval", r.interval.String())
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("outbox relay stopping")
			return
		case <-t.C:
			r.flush(ctx)
		}
	}
}

func (r *OutboxRelay) flush(ctx context.Context) {
	rows, err := r.repo.ListOutbox(ctx, 100)
	if err != nil {
		slog.Error("outbox list failed", "err", err)
		return
	}
	for _, row := range rows {
		msg := kafka.Message{
			Topic: row.Topic,
			Key:   []byte(row.Key),
			Value: row.Payload,
			Time:  row.CreatedAt,
		}
		if err := r.writer.WriteMessages(ctx, msg); err != nil {
			slog.Error("outbox publish failed", "id", row.ID, "topic", row.Topic, "err", err)
			// stop this batch; retry next tick (at-least-once)
			return
		}
		if err := r.repo.DeleteOutbox(ctx, row.ID); err != nil {
			slog.Error("outbox delete failed", "id", row.ID, "err", err)
			return
		}
		slog.Debug("outbox published", "id", row.ID, "topic", row.Topic, "key", row.Key)
	}
}
