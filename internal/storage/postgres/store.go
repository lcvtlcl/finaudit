package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wwpp/finaudit/internal/models"
)

// Upload — локальный тип пакета, отражает строку таблицы uploads.
type Upload struct {
	ID        int64
	Filename  string
	UserID    string
	Status    string
	CreatedAt time.Time
}

// User — локальный тип пакета, отражает строку таблицы users.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	Name         string
	Company      string
	LegalForm    string // ip | ooo | self_employed
	TaxRegime    string // usn | npd | osno
	CreatedAt    time.Time
}

var ErrEmailTaken = errors.New("email занят")

// Store инкапсулирует пул соединений с PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore создаёт пул соединений и проверяет доступность базы.
func NewStore(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close освобождает все соединения пула.
func (s *Store) Close() {
	s.pool.Close()
}

// CreateUpload вставляет новую запись загрузки и возвращает её ID.
func (s *Store) CreateUpload(ctx context.Context, userID, filename string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO uploads (user_id, filename, status) VALUES ($1, $2, 'received') RETURNING id`,
		userID, filename,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("postgres: create upload: %w", err)
	}
	return id, nil
}

// UploadSummary — загрузка со сводкой результата аудита.
type UploadSummary struct {
	ID           int64      `json:"id"`
	Filename     string     `json:"filename"`
	CreatedAt    time.Time  `json:"createdAt"`
	PeriodFrom   *time.Time `json:"periodFrom"`
	PeriodTo     *time.Time `json:"periodTo"`
	TotalIncome  *float64   `json:"totalIncome"`
	TotalExpense *float64   `json:"totalExpense"`
	NetCashFlow  *float64   `json:"netCashFlow"`
	HasCashGap   *bool      `json:"hasCashGap"`
}

func (s *Store) ListUploadsWithSummary(ctx context.Context, userID string) ([]UploadSummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.filename, u.created_at,
		        ar.period_from, ar.period_to,
		        ar.total_income, ar.total_expense, ar.net_cash_flow, ar.has_cash_gap
		 FROM uploads u
		 LEFT JOIN audit_results ar ON ar.upload_id = u.id
		 WHERE u.user_id = $1
		 ORDER BY u.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list uploads with summary: %w", err)
	}
	defer rows.Close()

	var result []UploadSummary
	for rows.Next() {
		var (
			u            UploadSummary
			periodFrom   *time.Time
			periodTo     *time.Time
			totalIncome  *float64
			totalExpense *float64
			netCashFlow  *float64
			hasCashGap   *bool
		)
		if err := rows.Scan(
			&u.ID,
			&u.Filename,
			&u.CreatedAt,
			&periodFrom,
			&periodTo,
			&totalIncome,
			&totalExpense,
			&netCashFlow,
			&hasCashGap,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan upload summary: %w", err)
		}
		u.PeriodFrom = periodFrom
		u.PeriodTo = periodTo
		u.TotalIncome = totalIncome
		u.TotalExpense = totalExpense
		u.NetCashFlow = netCashFlow
		u.HasCashGap = hasCashGap
		result = append(result, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: rows error: %w", err)
	}
	return result, nil
}

func (s *Store) GetAuditResult(ctx context.Context, uploadID int64) (models.AuditResult, error) {
	var payload []byte
	err := s.pool.QueryRow(ctx,
		`SELECT result FROM audit_results WHERE upload_id = $1`,
		uploadID,
	).Scan(&payload)
	if err != nil {
		return models.AuditResult{}, fmt.Errorf("postgres: get audit result: %w", err)
	}
	var result models.AuditResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return models.AuditResult{}, fmt.Errorf("postgres: unmarshal audit result: %w", err)
	}
	return result, nil
}

// UpdateTransactionFields правит поля транзакции (категория/направление/назначение/контрагент)
// в рамках конкретной загрузки. ErrNoRows — не найдено (или чужая загрузка).
func (s *Store) UpdateTransactionFields(ctx context.Context, uploadID, txID int64, category, direction, purpose, counterparty string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE transactions SET category = $1, direction = $2, purpose = $3, counterparty = $4
		 WHERE id = $5 AND upload_id = $6`,
		category, direction, purpose, counterparty, txID, uploadID)
	if err != nil {
		return fmt.Errorf("postgres: update transaction: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// DeleteUpload удаляет загрузку и связанные данные (только свою). ErrNoRows — не найдено/не владелец.
func (s *Store) DeleteUpload(ctx context.Context, userID string, uploadID int64) error {
	var owner string
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM uploads WHERE id = $1`, uploadID).Scan(&owner); err != nil {
		return fmt.Errorf("postgres: find upload: %w", err)
	}
	if owner != userID {
		return pgx.ErrNoRows
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM transactions WHERE upload_id = $1`, uploadID); err != nil {
		return fmt.Errorf("postgres: delete transactions: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM audit_results WHERE upload_id = $1`, uploadID); err != nil {
		return fmt.Errorf("postgres: delete audit result: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM uploads WHERE id = $1`, uploadID); err != nil {
		return fmt.Errorf("postgres: delete upload: %w", err)
	}
	return nil
}

// BulkInsertTransactions массово вставляет транзакции через COPY FROM.
func (s *Store) BulkInsertTransactions(ctx context.Context, uploadID int64, txs []models.Transaction) error {
	if len(txs) == 0 {
		return nil
	}

	rows := make([][]any, len(txs))
	for i, tx := range txs {
		rows[i] = []any{
			uploadID,
			tx.Date,
			tx.Amount,
			string(tx.Direction),
			tx.Counterparty,
			tx.INN,
			tx.Purpose,
			tx.Category,
		}
	}

	cols := []string{"upload_id", "op_date", "amount", "direction", "counterparty", "inn", "purpose", "category"}

	_, err := s.pool.CopyFrom(
		ctx,
		pgx.Identifier{"transactions"},
		cols,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("postgres: bulk insert transactions: %w", err)
	}
	return nil
}

// GetTransactionsByUpload возвращает все транзакции для указанной загрузки.
func (s *Store) GetTransactionsByUpload(ctx context.Context, uploadID int64) ([]models.Transaction, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, upload_id, op_date, amount, direction, counterparty, inn, purpose, category
		 FROM transactions WHERE upload_id = $1 ORDER BY op_date`,
		uploadID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: query transactions: %w", err)
	}
	defer rows.Close()

	var result []models.Transaction
	for rows.Next() {
		var (
			t         models.Transaction
			direction string
		)
		if err := rows.Scan(
			&t.ID,
			&t.UploadID,
			&t.Date,
			&t.Amount,
			&direction,
			&t.Counterparty,
			&t.INN,
			&t.Purpose,
			&t.Category,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan transaction: %w", err)
		}
		t.Direction = models.Direction(direction)
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: rows error: %w", err)
	}
	return result, nil
}

// SaveAuditResult сохраняет результат аудита в виде JSON.
func (s *Store) SaveAuditResult(ctx context.Context, uploadID int64, result models.AuditResult) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("postgres: marshal audit result: %w", err)
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO audit_results
		   (upload_id, period_from, period_to, total_income, total_expense, net_cash_flow, has_cash_gap, result)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (upload_id) DO UPDATE SET
		   period_from   = EXCLUDED.period_from,
		   period_to     = EXCLUDED.period_to,
		   total_income  = EXCLUDED.total_income,
		   total_expense = EXCLUDED.total_expense,
		   net_cash_flow = EXCLUDED.net_cash_flow,
		   has_cash_gap  = EXCLUDED.has_cash_gap,
		   result        = EXCLUDED.result`,
		uploadID,
		result.Period.From, result.Period.To,
		result.TotalIncome, result.TotalExpense, result.NetCashFlow,
		result.CashGap != nil, payload,
	)
	if err != nil {
		return fmt.Errorf("postgres: save audit result: %w", err)
	}
	return nil
}

func (s *Store) ListUploadsByUser(ctx context.Context, userID string) ([]Upload, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, filename, user_id, status, created_at
		 FROM uploads WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: query uploads: %w", err)
	}
	defer rows.Close()

	var result []Upload
	for rows.Next() {
		var u Upload
		if err := rows.Scan(&u.ID, &u.Filename, &u.UserID, &u.Status, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan upload: %w", err)
		}
		result = append(result, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: rows error: %w", err)
	}
	return result, nil
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash, name, company, legalForm, taxRegime string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, company, legal_form, tax_regime)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, email, password_hash, name, company, legal_form, tax_regime, created_at`,
		email, passwordHash, name, company, legalForm, taxRegime,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Company, &u.LegalForm, &u.TaxRegime, &u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrEmailTaken
		}
		return User{}, fmt.Errorf("postgres: create user: %w", err)
	}
	return u, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, name, company, legal_form, tax_regime, created_at
		 FROM users
		 WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Company, &u.LegalForm, &u.TaxRegime, &u.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("postgres: get user by email: %w", err)
	}
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, name, company, legal_form, tax_regime, created_at
		 FROM users
		 WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Company, &u.LegalForm, &u.TaxRegime, &u.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("postgres: get user by id: %w", err)
	}
	return u, nil
}

// UpdateTaxProfile обновляет юрформу и налоговый режим пользователя.
func (s *Store) UpdateTaxProfile(ctx context.Context, userID int64, legalForm, taxRegime string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET legal_form = $2, tax_regime = $3 WHERE id = $1`,
		userID, legalForm, taxRegime,
	)
	if err != nil {
		return fmt.Errorf("postgres: update tax profile: %w", err)
	}
	return nil
}
