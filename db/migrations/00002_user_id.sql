-- +goose Up
ALTER TABLE uploads ADD COLUMN user_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE uploads DROP COLUMN user_id;
