-- +goose Up
-- Загрузки выписок
CREATE TABLE uploads (
    id          BIGSERIAL PRIMARY KEY,
    filename    TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'received', -- received|parsed|analyzed|failed
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Нормализованные транзакции
CREATE TABLE transactions (
    id           BIGSERIAL PRIMARY KEY,
    upload_id    BIGINT      NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
    op_date      DATE        NOT NULL,
    amount       NUMERIC(18,2) NOT NULL,
    direction    TEXT        NOT NULL,            -- in|out
    counterparty TEXT,
    inn          TEXT,
    purpose      TEXT,                            -- сырое назначение платежа
    category     TEXT,                            -- null пока не категоризовано
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tx_upload ON transactions(upload_id);
CREATE INDEX idx_tx_date   ON transactions(op_date);

-- +goose Down
DROP TABLE transactions;
DROP TABLE uploads;
