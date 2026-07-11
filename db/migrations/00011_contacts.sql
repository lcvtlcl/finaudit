-- +goose Up
-- Обращения из формы обратной связи на сайте.
CREATE TABLE IF NOT EXISTS contact_requests (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    contact    TEXT NOT NULL,                 -- как с человеком связаться (email/телефон/телеграм)
    topic      TEXT NOT NULL,                 -- general | selfhosted | billing | privacy
    message    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_contact_requests_created ON contact_requests(created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS contact_requests;
