package internal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	StatusPending  = "PENDING"
	StatusActive   = "ACTIVE"
	StatusRevoked  = "REVOKED"
	StatusExpired  = "EXPIRED"
	ResHeld        = "HELD"
	ResConfirmed   = "CONFIRMED"
	ResReleased    = "RELEASED"
	defaultCurrency = "GBP"
)

type ConsentRow struct {
	ID              string
	MerchantID      string
	ConsumerID      string
	BankConsentRef  string
	Status          string
	MaxAmountPence  int64
	MaxMonthlyPence int64
	Currency        string
	ValidFrom       time.Time
	ValidUntil      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ReservationRow struct {
	ID          string
	ConsentID   string
	PaymentID   string
	AmountPence int64
	Status      string
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func (r *Repo) Pool() *pgxpool.Pool { return r.pool }

func (r *Repo) InsertConsent(ctx context.Context, c *ConsentRow) error {
	return r.pool.QueryRow(ctx, `
		INSERT INTO consent (
			merchant_id, consumer_id, bank_consent_ref, status,
			max_amount_pence, max_monthly_pence, currency, valid_until
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, valid_from, created_at, updated_at
	`,
		c.MerchantID, c.ConsumerID, c.BankConsentRef, c.Status,
		c.MaxAmountPence, c.MaxMonthlyPence, c.Currency, c.ValidUntil,
	).Scan(&c.ID, &c.ValidFrom, &c.CreatedAt, &c.UpdatedAt)
}

func (r *Repo) GetConsent(ctx context.Context, id string) (*ConsentRow, error) {
	return scanConsent(r.pool.QueryRow(ctx, `
		SELECT id, merchant_id, consumer_id, bank_consent_ref, status,
		       max_amount_pence, max_monthly_pence, currency,
		       valid_from, valid_until, created_at, updated_at
		FROM consent WHERE id = $1
	`, id))
}

func (r *Repo) RevokeConsent(ctx context.Context, id, merchantID string) (*ConsentRow, error) {
	q := `
		UPDATE consent
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status = $3
	`
	args := []any{StatusRevoked, id, StatusActive}
	if merchantID != "" {
		q += ` AND merchant_id = $4`
		args = append(args, merchantID)
	}
	q += `
		RETURNING id, merchant_id, consumer_id, bank_consent_ref, status,
		          max_amount_pence, max_monthly_pence, currency,
		          valid_from, valid_until, created_at, updated_at
	`
	return scanConsent(r.pool.QueryRow(ctx, q, args...))
}

func (r *Repo) ListConsents(ctx context.Context, consumerID, merchantID, status string, limit, offset int) ([]ConsentRow, int, error) {
	where := "WHERE 1=1"
	args := make([]any, 0, 6)
	n := 1
	if consumerID != "" {
		where += fmt.Sprintf(" AND consumer_id = $%d", n)
		args = append(args, consumerID)
		n++
	}
	if merchantID != "" {
		where += fmt.Sprintf(" AND merchant_id = $%d", n)
		args = append(args, merchantID)
		n++
	}
	if status != "" {
		where += fmt.Sprintf(" AND status = $%d", n)
		args = append(args, status)
		n++
	}

	listQ := fmt.Sprintf(`
		SELECT id, merchant_id, consumer_id, bank_consent_ref, status,
		       max_amount_pence, max_monthly_pence, currency,
		       valid_from, valid_until, created_at, updated_at,
		       COUNT(*) OVER() AS total_count
		FROM consent %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, n, n+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list consents: %w", err)
	}
	defer rows.Close()

	var total int
	out := make([]ConsentRow, 0, limit)
	for rows.Next() {
		var c ConsentRow
		if err := rows.Scan(
			&c.ID, &c.MerchantID, &c.ConsumerID, &c.BankConsentRef, &c.Status,
			&c.MaxAmountPence, &c.MaxMonthlyPence, &c.Currency,
			&c.ValidFrom, &c.ValidUntil, &c.CreatedAt, &c.UpdatedAt,
			&total,
		); err != nil {
			return nil, 0, fmt.Errorf("scan consent: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *Repo) GetReservationByPaymentID(ctx context.Context, tx pgx.Tx, paymentID string) (*ReservationRow, error) {
	return scanReservation(tx.QueryRow(ctx, `
		SELECT id, consent_id, payment_id, amount_pence, status, expires_at, created_at
		FROM consent_reservation WHERE payment_id = $1
	`, paymentID))
}

func (r *Repo) LockConsent(ctx context.Context, tx pgx.Tx, id string) (*ConsentRow, error) {
	return scanConsent(tx.QueryRow(ctx, `
		SELECT id, merchant_id, consumer_id, bank_consent_ref, status,
		       max_amount_pence, max_monthly_pence, currency,
		       valid_from, valid_until, created_at, updated_at
		FROM consent WHERE id = $1
		FOR UPDATE
	`, id))
}

// RollingUsage sums settled usage (30d) and non-expired HELD reservations.
func (r *Repo) RollingUsage(ctx context.Context, q pgx.Tx, consentID string) (used, held int64, err error) {
	err = q.QueryRow(ctx, `
		SELECT
			COALESCE((
				SELECT SUM(amount_pence) FROM consent_usage
				WHERE consent_id = $1 AND settled_at > NOW() - INTERVAL '30 days'
			), 0),
			COALESCE((
				SELECT SUM(amount_pence) FROM consent_reservation
				WHERE consent_id = $1 AND status = $2 AND expires_at > NOW()
			), 0)
	`, consentID, ResHeld).Scan(&used, &held)
	if err != nil {
		return 0, 0, fmt.Errorf("rolling usage: %w", err)
	}
	return used, held, nil
}

func (r *Repo) RollingUsagePool(ctx context.Context, consentID string) (used, held int64, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT
			COALESCE((
				SELECT SUM(amount_pence) FROM consent_usage
				WHERE consent_id = $1 AND settled_at > NOW() - INTERVAL '30 days'
			), 0),
			COALESCE((
				SELECT SUM(amount_pence) FROM consent_reservation
				WHERE consent_id = $1 AND status = $2 AND expires_at > NOW()
			), 0)
	`, consentID, ResHeld).Scan(&used, &held)
	if err != nil {
		return 0, 0, fmt.Errorf("rolling usage: %w", err)
	}
	return used, held, nil
}

func (r *Repo) TxCountThisMonth(ctx context.Context, consentID string) (int32, error) {
	var n int32
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM consent_usage
		WHERE consent_id = $1 AND settled_at > NOW() - INTERVAL '30 days'
	`, consentID).Scan(&n)
	return n, err
}

func (r *Repo) InsertReservation(ctx context.Context, tx pgx.Tx, consentID, paymentID string, amount int64) (*ReservationRow, error) {
	row := &ReservationRow{}
	err := tx.QueryRow(ctx, `
		INSERT INTO consent_reservation (consent_id, payment_id, amount_pence, status, expires_at)
		VALUES ($1, $2, $3, $4, NOW() + INTERVAL '10 minutes')
		RETURNING id, consent_id, payment_id, amount_pence, status, expires_at, created_at
	`, consentID, paymentID, amount, ResHeld).Scan(
		&row.ID, &row.ConsentID, &row.PaymentID, &row.AmountPence,
		&row.Status, &row.ExpiresAt, &row.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert reservation: %w", err)
	}
	return row, nil
}

func (r *Repo) GetReservation(ctx context.Context, tx pgx.Tx, id string) (*ReservationRow, error) {
	return scanReservation(tx.QueryRow(ctx, `
		SELECT id, consent_id, payment_id, amount_pence, status, expires_at, created_at
		FROM consent_reservation WHERE id = $1
		FOR UPDATE
	`, id))
}

func (r *Repo) ConfirmReservation(ctx context.Context, tx pgx.Tx, id, paymentID string) (*ReservationRow, error) {
	if paymentID != "" {
		return scanReservation(tx.QueryRow(ctx, `
			UPDATE consent_reservation
			SET status = $1
			WHERE id = $2 AND status = $3 AND payment_id = $4
			RETURNING id, consent_id, payment_id, amount_pence, status, expires_at, created_at
		`, ResConfirmed, id, ResHeld, paymentID))
	}
	return scanReservation(tx.QueryRow(ctx, `
		UPDATE consent_reservation
		SET status = $1
		WHERE id = $2 AND status = $3
		RETURNING id, consent_id, payment_id, amount_pence, status, expires_at, created_at
	`, ResConfirmed, id, ResHeld))
}

func (r *Repo) InsertUsage(ctx context.Context, tx pgx.Tx, consentID, paymentID string, amount int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO consent_usage (consent_id, payment_id, amount_pence)
		VALUES ($1, $2, $3)
		ON CONFLICT (payment_id) DO NOTHING
	`, consentID, paymentID, amount)
	return err
}

func (r *Repo) ReleaseReservation(ctx context.Context, tx pgx.Tx, id string) (*ReservationRow, error) {
	return scanReservation(tx.QueryRow(ctx, `
		UPDATE consent_reservation
		SET status = $1
		WHERE id = $2 AND status = $3
		RETURNING id, consent_id, payment_id, amount_pence, status, expires_at, created_at
	`, ResReleased, id, ResHeld))
}

type scannable interface {
	Scan(dest ...any) error
}

func scanConsent(row scannable) (*ConsentRow, error) {
	var c ConsentRow
	err := row.Scan(
		&c.ID, &c.MerchantID, &c.ConsumerID, &c.BankConsentRef, &c.Status,
		&c.MaxAmountPence, &c.MaxMonthlyPence, &c.Currency,
		&c.ValidFrom, &c.ValidUntil, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func scanReservation(row scannable) (*ReservationRow, error) {
	var r ReservationRow
	err := row.Scan(
		&r.ID, &r.ConsentID, &r.PaymentID, &r.AmountPence,
		&r.Status, &r.ExpiresAt, &r.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
