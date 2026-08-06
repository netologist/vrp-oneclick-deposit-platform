CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS consent (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id       UUID NOT NULL,
    consumer_id       TEXT NOT NULL,
    bank_consent_ref  TEXT NOT NULL UNIQUE,
    status            TEXT NOT NULL DEFAULT 'ACTIVE',
    max_amount_pence  BIGINT NOT NULL,
    max_monthly_pence BIGINT NOT NULL,
    currency          CHAR(3) NOT NULL DEFAULT 'GBP',
    valid_from        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until       TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS consent_reservation (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consent_id   UUID NOT NULL REFERENCES consent(id),
    payment_id   UUID NOT NULL UNIQUE,
    amount_pence BIGINT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'HELD', -- HELD | CONFIRMED | RELEASED
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS consent_usage (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consent_id   UUID NOT NULL REFERENCES consent(id),
    payment_id   UUID NOT NULL UNIQUE,
    amount_pence BIGINT NOT NULL,
    settled_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_consent_consumer ON consent(consumer_id, merchant_id);
CREATE INDEX IF NOT EXISTS idx_consent_reservation_consent ON consent_reservation(consent_id) WHERE status = 'HELD';
CREATE INDEX IF NOT EXISTS idx_consent_usage_window ON consent_usage(consent_id, settled_at);
