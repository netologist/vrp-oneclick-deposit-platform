package internal

import (
	"context"
	"errors"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func (r *Repo) CreateMerchantAndAPIKey(ctx context.Context, m *Merchant, keyHash, keyPrefix string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "begin tx", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	err = tx.QueryRow(ctx, `
		INSERT INTO merchant (name, webhook_url, contact_email, kyb_status, status, hmac_secret)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, m.Name, m.WebhookURL, m.ContactEmail, m.KYBStatus, m.Status, m.HMACSecret).
		Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "insert merchant", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO api_key (merchant_id, key_hash, key_prefix, status)
		VALUES ($1, $2, $3, $4)
	`, m.ID, keyHash, keyPrefix, APIKeyStatusActive)
	if err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "insert api_key", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "commit tx", err)
	}
	return nil
}

func (r *Repo) GetByID(ctx context.Context, id string) (*Merchant, error) {
	m := &Merchant{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, webhook_url, COALESCE(contact_email, ''), kyb_status, status, hmac_secret, created_at, updated_at
		FROM merchant
		WHERE id = $1
	`, id).Scan(
		&m.ID, &m.Name, &m.WebhookURL, &m.ContactEmail, &m.KYBStatus, &m.Status, &m.HMACSecret, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domainerr.New(domainerr.CodeNotFound, "merchant not found")
		}
		return nil, domainerr.Wrap(domainerr.CodeInternal, "get merchant", err)
	}
	return m, nil
}

// GetByAPIKey looks up active keys by the first 8 chars of the plaintext key,
// then bcrypt-compares the full key and returns the joined merchant.
func (r *Repo) GetByAPIKey(ctx context.Context, apiKey string) (*Merchant, error) {
	if len(apiKey) < 8 {
		return nil, domainerr.New(domainerr.CodeNotFound, "merchant not found")
	}
	prefix := apiKey[:8]

	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.name, m.webhook_url, COALESCE(m.contact_email, ''), m.kyb_status, m.status,
		       m.hmac_secret, m.created_at, m.updated_at, ak.key_hash
		FROM api_key ak
		JOIN merchant m ON m.id = ak.merchant_id
		WHERE ak.key_prefix = $1 AND ak.status = $2
	`, prefix, APIKeyStatusActive)
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "query api_key by prefix", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			m       Merchant
			keyHash string
		)
		if err := rows.Scan(
			&m.ID, &m.Name, &m.WebhookURL, &m.ContactEmail, &m.KYBStatus, &m.Status,
			&m.HMACSecret, &m.CreatedAt, &m.UpdatedAt, &keyHash,
		); err != nil {
			return nil, domainerr.Wrap(domainerr.CodeInternal, "scan api_key row", err)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(keyHash), []byte(apiKey)); err == nil {
			return &m, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "iterate api_key rows", err)
	}
	return nil, domainerr.New(domainerr.CodeNotFound, "merchant not found")
}

func (r *Repo) Suspend(ctx context.Context, id string) (*Merchant, error) {
	m := &Merchant{}
	err := r.pool.QueryRow(ctx, `
		UPDATE merchant
		SET status = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, webhook_url, COALESCE(contact_email, ''), kyb_status, status, hmac_secret, created_at, updated_at
	`, id, StatusSuspended).Scan(
		&m.ID, &m.Name, &m.WebhookURL, &m.ContactEmail, &m.KYBStatus, &m.Status, &m.HMACSecret, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domainerr.New(domainerr.CodeNotFound, "merchant not found")
		}
		return nil, domainerr.Wrap(domainerr.CodeInternal, "suspend merchant", err)
	}
	return m, nil
}

func (r *Repo) GetWebhookConfig(ctx context.Context, merchantID string) (*WebhookConfig, error) {
	cfg := &WebhookConfig{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, webhook_url, hmac_secret
		FROM merchant
		WHERE id = $1
	`, merchantID).Scan(&cfg.MerchantID, &cfg.WebhookURL, &cfg.HMACSecret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domainerr.New(domainerr.CodeNotFound, "merchant not found")
		}
		return nil, domainerr.Wrap(domainerr.CodeInternal, "get webhook config", err)
	}
	return cfg, nil
}
