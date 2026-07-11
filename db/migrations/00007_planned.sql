-- +goose Up
CREATE TABLE planned_payments (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL,
    pay_date   DATE NOT NULL,
    amount     NUMERIC(18,2) NOT NULL,
    direction  TEXT NOT NULL DEFAULT 'out',
    purpose    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_planned_user ON planned_payments(user_id);

-- +goose Down
DROP TABLE planned_payments;
