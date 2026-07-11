-- sqlc queries for personal cabinet.

-- name: CreateUser :one
INSERT INTO users (email, password_hash, name, company)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: CreateAuditResult :one
INSERT INTO audit_results (
  upload_id,
  period_from,
  period_to,
  total_income,
  total_expense,
  net_cash_flow,
  has_cash_gap,
  result
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetAuditResult :one
SELECT * FROM audit_results WHERE upload_id = $1;

-- name: ListUploadsByUser :many
SELECT
  u.id,
  u.filename,
  u.status,
  u.created_at,
  u.user_id,
  ar.period_from,
  ar.period_to,
  ar.total_income,
  ar.total_expense,
  ar.net_cash_flow,
  ar.has_cash_gap,
  ar.created_at AS audit_created_at
FROM uploads u
LEFT JOIN audit_results ar ON ar.upload_id = u.id
WHERE u.user_id = $1
ORDER BY u.created_at DESC;
