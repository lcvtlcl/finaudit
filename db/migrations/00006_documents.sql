-- +goose Up
CREATE TABLE documents (
    id             BIGSERIAL PRIMARY KEY,
    user_id        TEXT NOT NULL,
    filename       TEXT NOT NULL,
    stored_path    TEXT NOT NULL,
    size_bytes     BIGINT NOT NULL DEFAULT 0,
    last_upload_id BIGINT REFERENCES uploads(id) ON DELETE SET NULL, -- результат последнего анализа
    uploaded_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_documents_user ON documents(user_id);

-- +goose Down
DROP TABLE documents;
