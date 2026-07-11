package postgres

import (
	"context"
	"fmt"
	"time"
)

// BatchSummary — пакет со счётчиком файлов (для списка «Пакеты»).
type BatchSummary struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	FileCount int       `json:"fileCount"`
}

// BatchFile — файл пакета со статусом распознавания/сведения.
type BatchFile struct {
	ID         int64  `json:"id"`
	Filename   string `json:"filename"`
	AccountKey string `json:"accountKey"`
	UploadID   *int64 `json:"uploadId"` // свод группы (nil = не сведён: ошибка)
	TxCount    int    `json:"txCount"`
	Status     string `json:"status"`
	Note       string `json:"note"`
}

// CreateBatch создаёт пакет и возвращает его ID.
func (s *Store) CreateBatch(ctx context.Context, userID int64, name string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO batches (user_id, name) VALUES ($1, $2) RETURNING id`,
		userID, name,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("postgres: create batch: %w", err)
	}
	return id, nil
}

// AddBatchFile добавляет запись о файле пакета.
func (s *Store) AddBatchFile(ctx context.Context, batchID int64, filename, accountKey string, uploadID *int64, txCount int, status, note string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO batch_files (batch_id, filename, account_key, upload_id, tx_count, status, note)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		batchID, filename, accountKey, uploadID, txCount, status, note,
	)
	if err != nil {
		return fmt.Errorf("postgres: add batch file: %w", err)
	}
	return nil
}

// ListBatches возвращает пакеты пользователя со счётчиком файлов.
func (s *Store) ListBatches(ctx context.Context, userID int64) ([]BatchSummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT b.id, b.name, b.created_at, COUNT(bf.id)
		 FROM batches b
		 LEFT JOIN batch_files bf ON bf.batch_id = b.id
		 WHERE b.user_id = $1
		 GROUP BY b.id
		 ORDER BY b.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list batches: %w", err)
	}
	defer rows.Close()

	var out []BatchSummary
	for rows.Next() {
		var b BatchSummary
		if err := rows.Scan(&b.ID, &b.Name, &b.CreatedAt, &b.FileCount); err != nil {
			return nil, fmt.Errorf("postgres: scan batch: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BatchOwner возвращает user_id владельца пакета (для проверки доступа).
func (s *Store) BatchOwner(ctx context.Context, batchID int64) (int64, error) {
	var owner int64
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM batches WHERE id = $1`, batchID).Scan(&owner)
	if err != nil {
		return 0, fmt.Errorf("postgres: batch owner: %w", err)
	}
	return owner, nil
}

// GetBatchFiles возвращает файлы пакета.
func (s *Store) GetBatchFiles(ctx context.Context, batchID int64) ([]BatchFile, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, filename, account_key, upload_id, tx_count, status, note
		 FROM batch_files WHERE batch_id = $1 ORDER BY id`,
		batchID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: get batch files: %w", err)
	}
	defer rows.Close()

	var out []BatchFile
	for rows.Next() {
		var f BatchFile
		if err := rows.Scan(&f.ID, &f.Filename, &f.AccountKey, &f.UploadID, &f.TxCount, &f.Status, &f.Note); err != nil {
			return nil, fmt.Errorf("postgres: scan batch file: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
