CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS account (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type      TEXT NOT NULL,
    owner_ref TEXT NOT NULL,
    currency  CHAR(3) NOT NULL DEFAULT 'GBP',
    UNIQUE (type, owner_ref, currency)
);

CREATE TABLE IF NOT EXISTS journal_entry (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id  TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL,
    reversed    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS journal_line (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    journal_entry_id UUID NOT NULL REFERENCES journal_entry(id),
    account_id       UUID NOT NULL REFERENCES account(id),
    direction        CHAR(2) NOT NULL CHECK (direction IN ('DR', 'CR')),
    amount_pence     BIGINT NOT NULL CHECK (amount_pence > 0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION check_journal_balance() RETURNS TRIGGER AS $$
DECLARE
    imbalance BIGINT;
    line_count INT;
BEGIN
    SELECT COUNT(*) INTO line_count
    FROM journal_line
    WHERE journal_entry_id = NEW.journal_entry_id;

    -- Balance check only when we have at least 2 lines (entry complete)
    IF line_count < 2 THEN
        RETURN NEW;
    END IF;

    SELECT COALESCE(SUM(CASE WHEN direction = 'DR' THEN amount_pence ELSE -amount_pence END), 0)
    INTO imbalance
    FROM journal_line
    WHERE journal_entry_id = NEW.journal_entry_id;

    IF imbalance != 0 THEN
        RAISE EXCEPTION 'Journal entry % is not balanced (imbalance=%)', NEW.journal_entry_id, imbalance;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_journal_balance ON journal_line;
CREATE CONSTRAINT TRIGGER trg_journal_balance
    AFTER INSERT ON journal_line
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    EXECUTE FUNCTION check_journal_balance();

CREATE INDEX IF NOT EXISTS idx_journal_line_entry ON journal_line(journal_entry_id);
CREATE INDEX IF NOT EXISTS idx_journal_line_account ON journal_line(account_id);
