-- +goose Up
-- Платежи и подписки (приём оплаты через ЮKassa).
-- Карточные данные у нас не хранятся и не проходят через сервис: их принимает платёжный провайдер.
CREATE TABLE IF NOT EXISTS payments (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    payment_id   TEXT NOT NULL UNIQUE,             -- идентификатор платежа в ЮKassa
    plan         TEXT NOT NULL,                    -- тарифный план (pro)
    amount       NUMERIC(12,2) NOT NULL,
    currency     TEXT NOT NULL DEFAULT 'RUB',
    status       TEXT NOT NULL,                    -- pending | succeeded | canceled
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_payments_user ON payments(user_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);

CREATE TABLE IF NOT EXISTS subscriptions (
    user_id     BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    plan        TEXT NOT NULL DEFAULT 'free',      -- free | pro
    status      TEXT NOT NULL DEFAULT 'inactive',  -- active | inactive
    expires_at  TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS payments;
