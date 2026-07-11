-- sqlc queries. Запусти `make sqlc` для генерации Go-кода.

-- name: CreateUpload :one
INSERT INTO uploads (filename, status)
VALUES ($1, 'received')
RETURNING *;

-- name: SetUploadStatus :exec
UPDATE uploads SET status = $2 WHERE id = $1;

-- name: InsertTransaction :one
INSERT INTO transactions
  (upload_id, op_date, amount, direction, counterparty, inn, purpose, category, activity)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: ListTransactionsByUpload :many
SELECT * FROM transactions WHERE upload_id = $1 ORDER BY op_date;

-- name: SetTransactionCategory :exec
UPDATE transactions SET category = $2 WHERE id = $1;

-- name: ListUncategorized :many
SELECT * FROM transactions WHERE upload_id = $1 AND category IS NULL;
