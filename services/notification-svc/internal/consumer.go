package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

const (
	paymentEventsTopic = "payment.events"
	consumerGroup      = "notification-service"
	deliveredKeyTTL    = 24 * time.Hour
	deliveredKeyPrefix = "webhook:delivered:"
)

type Consumer struct {
	Reader    *kafka.Reader
	Redis     *redis.Client
	Deliverer *Deliverer
	Log       *slog.Logger
}

func NewReader(brokers []string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          paymentEventsTopic,
		GroupID:        consumerGroup,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		StartOffset:    kafka.FirstOffset,
	})
}

func NewDLQWriter(brokers []string) *kafka.Writer {
	return &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        dlqTopic,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		Async:        false,
	}
}

// Run consumes payment.events until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	c.Log.Info("notification consumer started",
		"topic", paymentEventsTopic,
		"group", consumerGroup,
	)
	for {
		msg, err := c.Reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("fetch message: %w", err)
		}
		if err := c.handle(ctx, msg); err != nil {
			c.Log.Error("handle payment event failed",
				"offset", msg.Offset,
				"partition", msg.Partition,
				"err", err,
			)
			// Do not commit on failure so the message can be retried after restart.
			continue
		}
		if err := c.Reader.CommitMessages(ctx, msg); err != nil {
			c.Log.Error("commit offset failed", "err", err)
			return fmt.Errorf("commit: %w", err)
		}
	}
}

func (c *Consumer) handle(ctx context.Context, msg kafka.Message) error {
	var evt PaymentEvent
	if err := json.Unmarshal(msg.Value, &evt); err != nil {
		// Poison message — log and skip so the consumer is not stuck.
		c.Log.Error("invalid payment event json; skipping",
			"offset", msg.Offset,
			"err", err,
			"raw", string(msg.Value),
		)
		return nil
	}

	paymentID := evt.PaymentKey()
	if paymentID == "" {
		c.Log.Error("payment event missing id; skipping", "offset", msg.Offset)
		return nil
	}

	// Only act on settled payment notifications (ignore other event types if present).
	if evt.EventType != "" && evt.EventType != "payment.settled" {
		c.Log.Info("ignoring non-settled event",
			"event_type", evt.EventType,
			"payment_id", paymentID,
		)
		return nil
	}

	delivered, err := c.alreadyDelivered(ctx, paymentID)
	if err != nil {
		return fmt.Errorf("check delivered: %w", err)
	}
	if delivered {
		c.Log.Info("webhook already delivered; skipping", "payment_id", paymentID)
		return nil
	}

	if err := c.Deliverer.Deliver(ctx, evt, msg.Value); err != nil {
		return err
	}

	if err := c.markDelivered(ctx, paymentID); err != nil {
		// Delivery succeeded; still surface error so offset is not committed and
		// redis mark can be retried. Deliver is idempotent via redis check + merchant 2xx.
		return fmt.Errorf("mark delivered: %w", err)
	}
	return nil
}

func deliveredKey(paymentID string) string {
	return deliveredKeyPrefix + paymentID
}

func (c *Consumer) alreadyDelivered(ctx context.Context, paymentID string) (bool, error) {
	n, err := c.Redis.Exists(ctx, deliveredKey(paymentID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (c *Consumer) markDelivered(ctx context.Context, paymentID string) error {
	return c.Redis.Set(ctx, deliveredKey(paymentID), "1", deliveredKeyTTL).Err()
}

func (c *Consumer) Close() error {
	return c.Reader.Close()
}
