-- +goose Up
CREATE TABLE batches (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_batches_user ON batches(user_id);

CREATE TABLE batch_files (
    id          BIGSERIAL PRIMARY KEY,
    batch_id    BIGINT NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    account_key TEXT NOT NULL DEFAULT '',
    upload_id   BIGINT REFERENCES uploads(id) ON DELETE SET NULL, -- свод группы, к которой отнесён файл
    tx_count    INT NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'ok', -- ok | failed
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_batch_files_batch ON batch_files(batch_id);

-- +goose Down
DROP TABLE batch_files;
DROP TABLE batches;
