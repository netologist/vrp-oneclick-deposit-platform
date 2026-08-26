CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS payment (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  TEXT NOT NULL UNIQUE,
    merchant_id      UUID NOT NULL,
    consent_id       UUID NOT NULL,
    consumer_id      TEXT NOT NULL DEFAULT '',
    amount_pence     BIGINT NOT NULL,
    currency         CHAR(3) NOT NULL DEFAULT 'GBP',
    status           TEXT NOT NULL DEFAULT 'INITIATED' CHECK (status IN ('INITIATED', 'CONSENT_RESERVED', 'RISK_PASSED', 'AUTHORISING', 'SETTLED', 'FAILED', 'MANUAL_REVIEW')),
    bank_payment_ref TEXT,
    reservation_id   UUID,
    risk_score       INT,
    risk_decision    TEXT,
    failure_reason   TEXT,
    description      TEXT,
    initiated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at       TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS payment_event (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id  UUID NOT NULL REFERENCES payment(id),
    from_status TEXT NOT NULL,
    to_status   TEXT NOT NULL,
    reason      TEXT,
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS outbox (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    topic      TEXT NOT NULL,
    key        TEXT NOT NULL,
    payload    JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payment_merchant ON payment(merchant_id);
CREATE INDEX IF NOT EXISTS idx_payment_event_payment ON payment_event(payment_id);
CREATE INDEX IF NOT EXISTS idx_outbox_created ON outbox(created_at);
