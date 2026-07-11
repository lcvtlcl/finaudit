-- +goose Up
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    name          TEXT,
    company       TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE transactions ADD COLUMN activity TEXT;

CREATE TABLE audit_results (
    upload_id      BIGINT PRIMARY KEY REFERENCES uploads(id) ON DELETE CASCADE,
    period_from    DATE,
    period_to      DATE,
    total_income   NUMERIC(18,2),
    total_expense  NUMERIC(18,2),
    net_cash_flow  NUMERIC(18,2),
    has_cash_gap   BOOLEAN NOT NULL DEFAULT false,
    result         JSONB NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE audit_results;
ALTER TABLE transactions DROP COLUMN activity;
DROP TABLE users;
