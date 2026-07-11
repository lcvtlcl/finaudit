-- +goose Up
CREATE TABLE ai_credentials (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL,
    label       TEXT NOT NULL,
    api_key_enc BYTEA NOT NULL,
    key_hint    TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ai_cred_user ON ai_credentials(user_id);

CREATE TABLE user_settings (
    user_id              BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    active_credential_id BIGINT REFERENCES ai_credentials(id) ON DELETE SET NULL,
    tokens_used          BIGINT NOT NULL DEFAULT 0,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE user_settings;
DROP TABLE ai_credentials;
