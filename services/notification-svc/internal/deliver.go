package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	merchantv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/merchant/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/webhook"
	"github.com/segmentio/kafka-go"
)

const (
	maxAttempts    = 3
	httpTimeout    = 5 * time.Second
	dlqTopic       = "webhook.dlq"
	contentTypeJSON = "application/json"
)

var retryBackoffs = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
}

// PaymentEvent is the Kafka payload published by the payment outbox.
// Payment fields are flat alongside event_type (payment.settled).
type PaymentEvent struct {
	EventType      string `json:"event_type"`
	ID             string `json:"id"`
	PaymentID      string `json:"payment_id"`
	MerchantID     string `json:"merchant_id"`
	ConsentID      string `json:"consent_id"`
	ConsumerID     string `json:"consumer_id"`
	AmountPence    int64  `json:"amount_pence"`
	Currency       string `json:"currency"`
	Status         string `json:"status"`
	BankPaymentRef string `json:"bank_payment_ref,omitempty"`
	Description    string `json:"description,omitempty"`
	RiskScore      int32  `json:"risk_score,omitempty"`
	RiskDecision   string `json:"risk_decision,omitempty"`
	InitiatedAt    string `json:"initiated_at,omitempty"`
	SettledAt      string `json:"settled_at,omitempty"`
}

func (e PaymentEvent) PaymentKey() string {
	if e.ID != "" {
		return e.ID
	}
	return e.PaymentID
}

// WebhookBody is POSTed to the merchant webhook URL.
type WebhookBody struct {
	EventType   string         `json:"event_type"`
	Payment     map[string]any `json:"payment"`
	DeliveredAt string         `json:"delivered_at"`
}

// DLQPayload is published to webhook.dlq after exhausted retries.
type DLQPayload struct {
	MerchantID string          `json:"merchant_id"`
	PaymentID  string          `json:"payment_id"`
	WebhookURL string          `json:"webhook_url"`
	Attempts   int             `json:"attempts"`
	LastError  string          `json:"last_error"`
	Payload    json.RawMessage `json:"payload"`
	FailedAt   string          `json:"failed_at"`
}

type Deliverer struct {
	Merchants merchantv1.MerchantServiceClient
	HTTP      *http.Client
	DLQ       *kafka.Writer
	Log       *slog.Logger
}

func NewDeliverer(merchants merchantv1.MerchantServiceClient, dlq *kafka.Writer, log *slog.Logger) *Deliverer {
	return &Deliverer{
		Merchants: merchants,
		HTTP:      &http.Client{Timeout: httpTimeout},
		DLQ:       dlq,
		Log:       log,
	}
}

// Deliver fetches merchant webhook config, POSTs a signed payload with retries,
// and publishes to webhook.dlq when all attempts fail.
// Returns nil when delivery succeeds or is intentionally skipped (no URL).
func (d *Deliverer) Deliver(ctx context.Context, evt PaymentEvent, raw []byte) error {
	paymentID := evt.PaymentKey()
	if paymentID == "" {
		return fmt.Errorf("payment event missing id")
	}
	if evt.MerchantID == "" {
		return fmt.Errorf("payment event missing merchant_id")
	}

	cfg, err := d.Merchants.GetWebhookConfig(ctx, &merchantv1.GetWebhookConfigRequest{
		MerchantId: evt.MerchantID,
	})
	if err != nil {
		return fmt.Errorf("get webhook config: %w", err)
	}
	if cfg.GetWebhookUrl() == "" {
		d.Log.Warn("merchant has no webhook url; skipping",
			"merchant_id", evt.MerchantID,
			"payment_id", paymentID,
		)
		return nil
	}

	body, err := buildWebhookBody(evt)
	if err != nil {
		return fmt.Errorf("build webhook body: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = d.postWebhook(ctx, cfg.GetWebhookUrl(), cfg.GetHmacSecret(), body)
		if lastErr == nil {
			d.Log.Info("webhook delivered",
				"payment_id", paymentID,
				"merchant_id", evt.MerchantID,
				"attempt", attempt,
			)
			return nil
		}
		d.Log.Warn("webhook delivery failed",
			"payment_id", paymentID,
			"merchant_id", evt.MerchantID,
			"attempt", attempt,
			"err", lastErr,
		)
		if attempt < maxAttempts {
			delay := retryBackoffs[attempt-1]
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	if err := d.publishDLQ(ctx, evt, cfg.GetWebhookUrl(), body, maxAttempts, lastErr); err != nil {
		return fmt.Errorf("deliver failed and dlq publish failed: %w (last deliver err: %v)", err, lastErr)
	}
	d.Log.Error("webhook moved to dlq",
		"payment_id", paymentID,
		"merchant_id", evt.MerchantID,
		"attempts", maxAttempts,
		"err", lastErr,
	)
	return nil
}

func buildWebhookBody(evt PaymentEvent) ([]byte, error) {
	paymentID := evt.PaymentKey()
	payment := map[string]any{
		"id":               paymentID,
		"merchant_id":      evt.MerchantID,
		"consent_id":       evt.ConsentID,
		"consumer_id":      evt.ConsumerID,
		"amount_pence":     evt.AmountPence,
		"currency":         evt.Currency,
		"status":           evt.Status,
		"bank_payment_ref": evt.BankPaymentRef,
		"description":      evt.Description,
		"risk_score":       evt.RiskScore,
		"risk_decision":    evt.RiskDecision,
		"initiated_at":     evt.InitiatedAt,
		"settled_at":       evt.SettledAt,
	}
	eventType := evt.EventType
	if eventType == "" {
		eventType = "payment.settled"
	}
	wb := WebhookBody{
		EventType:   eventType,
		Payment:     payment,
		DeliveredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	return json.Marshal(wb)
}

func (d *Deliverer) postWebhook(ctx context.Context, url, secret string, body []byte) error {
	ts := time.Now().UTC()
	sig := webhook.Sign(secret, ts, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", contentTypeJSON)
	req.Header.Set("X-PC-Signature", sig)
	req.Header.Set("X-PC-Timestamp", webhook.TimestampHeader(ts))

	resp, err := d.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}

func (d *Deliverer) publishDLQ(ctx context.Context, evt PaymentEvent, webhookURL string, body []byte, attempts int, lastErr error) error {
	if d.DLQ == nil {
		return fmt.Errorf("dlq writer not configured")
	}
	errMsg := ""
	if lastErr != nil {
		errMsg = lastErr.Error()
	}
	payload := DLQPayload{
		MerchantID: evt.MerchantID,
		PaymentID:  evt.PaymentKey(),
		WebhookURL: webhookURL,
		Attempts:   attempts,
		LastError:  errMsg,
		Payload:    json.RawMessage(body),
		FailedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal dlq: %w", err)
	}
	msg := kafka.Message{
		Key:   []byte(evt.MerchantID),
		Value: b,
		Time:  time.Now().UTC(),
	}
	if err := d.DLQ.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("write dlq: %w", err)
	}
	return nil
}
