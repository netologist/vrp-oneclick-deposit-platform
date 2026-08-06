package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hozgan/vrp-demo/pkg/shared/domainerr"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	StatusInitiated       = "INITIATED"
	StatusConsentReserved = "CONSENT_RESERVED"
	StatusRiskPassed      = "RISK_PASSED"
	StatusAuthorising     = "AUTHORISING"
	StatusSettled         = "SETTLED"
	StatusFailed          = "FAILED"
	StatusManualReview    = "MANUAL_REVIEW"

	TopicPaymentEvents  = "payment.events"
	EventPaymentSettled = "payment.settled"
)

type Payment struct {
	ID             uuid.UUID
	IdempotencyKey string
	MerchantID     uuid.UUID
	ConsentID      uuid.UUID
	ConsumerID     string
	AmountPence    int64
	Currency       string
	Status         string
	BankPaymentRef string
	ReservationID  *uuid.UUID
	RiskScore      *int32
	RiskDecision   string
	FailureReason  string
	Description    string
	InitiatedAt    time.Time
	SettledAt      *time.Time
	UpdatedAt      time.Time
}

type OutboxRow struct {
	ID        uuid.UUID
	Topic     string
	Key       string
	Payload   []byte
	CreatedAt time.Time
}

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func (r *Repo) CreatePayment(ctx context.Context, p *Payment) error {
	const q = `
INSERT INTO payment (
  id, idempotency_key, merchant_id, consent_id, consumer_id,
  amount_pence, currency, status, description, initiated_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`

	now := time.Now().UTC()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.Status == "" {
		p.Status = StatusInitiated
	}
	p.InitiatedAt = now
	p.UpdatedAt = now

	_, err := r.pool.Exec(ctx, q,
		p.ID, p.IdempotencyKey, p.MerchantID, p.ConsentID, p.ConsumerID,
		p.AmountPence, p.Currency, p.Status, p.Description, p.InitiatedAt, p.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domainerr.New(domainerr.CodeDuplicateIdempotency, "idempotency key already used")
		}
		return domainerr.Wrap(domainerr.CodeInternal, "insert payment", err)
	}
	return r.insertEvent(ctx, nil, p.ID, "", p.Status, "payment created", nil)
}

func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (*Payment, error) {
	return r.scanOne(ctx, `
SELECT id, idempotency_key, merchant_id, consent_id, consumer_id,
       amount_pence, currency, status, COALESCE(bank_payment_ref,''),
       reservation_id, risk_score, COALESCE(risk_decision,''),
       COALESCE(failure_reason,''), COALESCE(description,''),
       initiated_at, settled_at, updated_at
FROM payment WHERE id = $1`, id)
}

func (r *Repo) GetByIDAndMerchant(ctx context.Context, id, merchantID uuid.UUID) (*Payment, error) {
	p, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.MerchantID != merchantID {
		return nil, domainerr.New(domainerr.CodeNotFound, "payment not found")
	}
	return p, nil
}

func (r *Repo) GetByIdempotencyKey(ctx context.Context, key string) (*Payment, error) {
	return r.scanOne(ctx, `
SELECT id, idempotency_key, merchant_id, consent_id, consumer_id,
       amount_pence, currency, status, COALESCE(bank_payment_ref,''),
       reservation_id, risk_score, COALESCE(risk_decision,''),
       COALESCE(failure_reason,''), COALESCE(description,''),
       initiated_at, settled_at, updated_at
FROM payment WHERE idempotency_key = $1`, key)
}

func (r *Repo) scanOne(ctx context.Context, q string, args ...any) (*Payment, error) {
	var p Payment
	err := r.pool.QueryRow(ctx, q, args...).Scan(
		&p.ID, &p.IdempotencyKey, &p.MerchantID, &p.ConsentID, &p.ConsumerID,
		&p.AmountPence, &p.Currency, &p.Status, &p.BankPaymentRef,
		&p.ReservationID, &p.RiskScore, &p.RiskDecision,
		&p.FailureReason, &p.Description,
		&p.InitiatedAt, &p.SettledAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.New(domainerr.CodeNotFound, "payment not found")
	}
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "get payment", err)
	}
	return &p, nil
}

func (r *Repo) UpdateAfterConsent(ctx context.Context, p *Payment, reservationID uuid.UUID, consumerID string) error {
	from := p.Status
	now := time.Now().UTC()
	const q = `
UPDATE payment
SET status = $2, reservation_id = $3, consumer_id = $4, updated_at = $5
WHERE id = $1`
	if _, err := r.pool.Exec(ctx, q, p.ID, StatusConsentReserved, reservationID, consumerID, now); err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "update after consent", err)
	}
	if err := r.insertEvent(ctx, nil, p.ID, from, StatusConsentReserved, "consent reserved", map[string]any{
		"reservation_id": reservationID.String(),
		"consumer_id":    consumerID,
	}); err != nil {
		return err
	}
	p.Status = StatusConsentReserved
	p.ReservationID = &reservationID
	p.ConsumerID = consumerID
	p.UpdatedAt = now
	return nil
}

func (r *Repo) UpdateAfterRisk(ctx context.Context, p *Payment, score int32, decision string) error {
	from := p.Status
	now := time.Now().UTC()
	const q = `
UPDATE payment
SET status = $2, risk_score = $3, risk_decision = $4, updated_at = $5
WHERE id = $1`
	if _, err := r.pool.Exec(ctx, q, p.ID, StatusRiskPassed, score, decision, now); err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "update after risk", err)
	}
	if err := r.insertEvent(ctx, nil, p.ID, from, StatusRiskPassed, "risk scored", map[string]any{
		"score":    score,
		"decision": decision,
	}); err != nil {
		return err
	}
	p.Status = StatusRiskPassed
	p.RiskScore = &score
	p.RiskDecision = decision
	p.UpdatedAt = now
	return nil
}

func (r *Repo) UpdateAfterBank(ctx context.Context, p *Payment, bankRef string) error {
	from := p.Status
	now := time.Now().UTC()
	const q = `
UPDATE payment
SET status = $2, bank_payment_ref = $3, updated_at = $4
WHERE id = $1`
	if _, err := r.pool.Exec(ctx, q, p.ID, StatusAuthorising, bankRef, now); err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "update after bank", err)
	}
	if err := r.insertEvent(ctx, nil, p.ID, from, StatusAuthorising, "bank authorising", map[string]any{
		"bank_payment_ref": bankRef,
	}); err != nil {
		return err
	}
	p.Status = StatusAuthorising
	p.BankPaymentRef = bankRef
	p.UpdatedAt = now
	return nil
}

// SettleWithOutbox sets SETTLED and writes event + outbox in one transaction.
func (r *Repo) SettleWithOutbox(ctx context.Context, p *Payment) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "begin settle tx", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	from := p.Status
	now := time.Now().UTC()
	const uq = `
UPDATE payment
SET status = $2, settled_at = $3, updated_at = $3, failure_reason = NULL
WHERE id = $1`
	if _, err := tx.Exec(ctx, uq, p.ID, StatusSettled, now); err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "settle payment", err)
	}
	if err := r.insertEvent(ctx, tx, p.ID, from, StatusSettled, "payment settled", nil); err != nil {
		return err
	}

	payload, err := buildSettledPayload(p, now)
	if err != nil {
		return err
	}
	const oq = `INSERT INTO outbox (id, topic, key, payload, created_at) VALUES ($1,$2,$3,$4,$5)`
	if _, err := tx.Exec(ctx, oq, uuid.New(), TopicPaymentEvents, p.ID.String(), payload, now); err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "insert outbox", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "commit settle tx", err)
	}
	p.Status = StatusSettled
	p.SettledAt = &now
	p.UpdatedAt = now
	p.FailureReason = ""
	return nil
}

func (r *Repo) MarkFailed(ctx context.Context, p *Payment, reason string) error {
	from := p.Status
	now := time.Now().UTC()
	const q = `
UPDATE payment
SET status = $2, failure_reason = $3, updated_at = $4
WHERE id = $1`
	if _, err := r.pool.Exec(ctx, q, p.ID, StatusFailed, reason, now); err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "mark failed", err)
	}
	if err := r.insertEvent(ctx, nil, p.ID, from, StatusFailed, reason, nil); err != nil {
		return err
	}
	p.Status = StatusFailed
	p.FailureReason = reason
	p.UpdatedAt = now
	return nil
}

func (r *Repo) MarkManualReview(ctx context.Context, p *Payment, reason string) error {
	from := p.Status
	now := time.Now().UTC()
	const q = `
UPDATE payment
SET status = $2, failure_reason = $3, updated_at = $4
WHERE id = $1`
	if _, err := r.pool.Exec(ctx, q, p.ID, StatusManualReview, reason, now); err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "mark manual review", err)
	}
	if err := r.insertEvent(ctx, nil, p.ID, from, StatusManualReview, reason, nil); err != nil {
		return err
	}
	p.Status = StatusManualReview
	p.FailureReason = reason
	p.UpdatedAt = now
	return nil
}

// ResetForRetry prepares a FAILED payment for another saga attempt.
func (r *Repo) ResetForRetry(ctx context.Context, p *Payment) error {
	if p.Status != StatusFailed {
		return domainerr.New(domainerr.CodeConflict, "only FAILED payments can be retried")
	}
	from := p.Status
	now := time.Now().UTC()
	const q = `
UPDATE payment
SET status = $2,
    failure_reason = NULL,
    bank_payment_ref = NULL,
    reservation_id = NULL,
    risk_score = NULL,
    risk_decision = NULL,
    settled_at = NULL,
    updated_at = $3
WHERE id = $1 AND status = $4`
	ct, err := r.pool.Exec(ctx, q, p.ID, StatusInitiated, now, StatusFailed)
	if err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "reset for retry", err)
	}
	if ct.RowsAffected() == 0 {
		return domainerr.New(domainerr.CodeConflict, "payment is not in FAILED state")
	}
	if err := r.insertEvent(ctx, nil, p.ID, from, StatusInitiated, "retry initiated", nil); err != nil {
		return err
	}
	p.Status = StatusInitiated
	p.FailureReason = ""
	p.BankPaymentRef = ""
	p.ReservationID = nil
	p.RiskScore = nil
	p.RiskDecision = ""
	p.SettledAt = nil
	p.UpdatedAt = now
	return nil
}

func (r *Repo) ListOutbox(ctx context.Context, limit int) ([]OutboxRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, topic, key, payload, created_at
FROM outbox
ORDER BY created_at ASC
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list outbox: %w", err)
	}
	defer rows.Close()

	var out []OutboxRow
	for rows.Next() {
		var row OutboxRow
		if err := rows.Scan(&row.ID, &row.Topic, &row.Key, &row.Payload, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan outbox: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repo) DeleteOutbox(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM outbox WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete outbox: %w", err)
	}
	return nil
}

func (r *Repo) insertEvent(ctx context.Context, tx pgx.Tx, paymentID uuid.UUID, from, to, reason string, meta map[string]any) error {
	metaJSON := []byte(`{}`)
	if meta != nil {
		b, err := json.Marshal(meta)
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "marshal event meta", err)
		}
		metaJSON = b
	}
	const q = `
INSERT INTO payment_event (id, payment_id, from_status, to_status, reason, metadata, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)`
	args := []any{uuid.New(), paymentID, from, to, reason, metaJSON, time.Now().UTC()}
	var err error
	if tx != nil {
		_, err = tx.Exec(ctx, q, args...)
	} else {
		_, err = r.pool.Exec(ctx, q, args...)
	}
	if err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "insert payment event", err)
	}
	return nil
}

func buildSettledPayload(p *Payment, settledAt time.Time) ([]byte, error) {
	var riskScore any
	if p.RiskScore != nil {
		riskScore = *p.RiskScore
	}
	body := map[string]any{
		"event_type":       EventPaymentSettled,
		"payment_id":       p.ID.String(),
		"idempotency_key":  p.IdempotencyKey,
		"merchant_id":      p.MerchantID.String(),
		"consent_id":       p.ConsentID.String(),
		"consumer_id":      p.ConsumerID,
		"amount_pence":     p.AmountPence,
		"currency":         p.Currency,
		"status":           StatusSettled,
		"bank_payment_ref": p.BankPaymentRef,
		"risk_score":       riskScore,
		"risk_decision":    p.RiskDecision,
		"description":      p.Description,
		"initiated_at":     p.InitiatedAt.UTC().Format(time.RFC3339Nano),
		"settled_at":       settledAt.UTC().Format(time.RFC3339Nano),
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "marshal settled payload", err)
	}
	return b, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return strings.Contains(err.Error(), "duplicate key")
}
