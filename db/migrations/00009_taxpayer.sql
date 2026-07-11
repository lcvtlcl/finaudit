-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS legal_form TEXT NOT NULL DEFAULT 'ip';
ALTER TABLE users ADD COLUMN IF NOT EXISTS tax_regime TEXT NOT NULL DEFAULT 'usn';

-- +goose Down
ALTER TABLE users DROP COLUMN tax_regime;
ALTER TABLE users DROP COLUMN legal_form;
