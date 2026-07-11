package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Document — сохранённый документ пользователя (можно анализировать позже, не загружая заново).
type Document struct {
	ID           int64     `json:"id"`
	Filename     string    `json:"filename"`
	SizeBytes    int64     `json:"sizeBytes"`
	UploadedAt   time.Time `json:"uploadedAt"`
	LastUploadID *int64    `json:"lastUploadId"` // результат последнего анализа (nil = не анализировался)
}

// CreateDocument сохраняет запись о загруженном документе (без анализа).
func (s *Store) CreateDocument(ctx context.Context, userID, filename, storedPath string, size int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO documents (user_id, filename, stored_path, size_bytes)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, filename, storedPath, size,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("postgres: create document: %w", err)
	}
	return id, nil
}

// ListDocuments возвращает документы пользователя.
func (s *Store) ListDocuments(ctx context.Context, userID string) ([]Document, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, filename, size_bytes, uploaded_at, last_upload_id
		 FROM documents WHERE user_id = $1 ORDER BY uploaded_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list documents: %w", err)
	}
	defer rows.Close()

	var out []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.Filename, &d.SizeBytes, &d.UploadedAt, &d.LastUploadID); err != nil {
			return nil, fmt.Errorf("postgres: scan document: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DocumentPath возвращает имя и путь к файлу документа (с проверкой владельца). ErrNoRows — нет/не владелец.
func (s *Store) DocumentPath(ctx context.Context, userID string, id int64) (filename, path string, err error) {
	var owner string
	err = s.pool.QueryRow(ctx,
		`SELECT user_id, filename, stored_path FROM documents WHERE id = $1`, id,
	).Scan(&owner, &filename, &path)
	if err != nil {
		return "", "", fmt.Errorf("postgres: document path: %w", err)
	}
	if owner != userID {
		return "", "", pgx.ErrNoRows
	}
	return filename, path, nil
}

// SetDocumentUpload привязывает к документу результат последнего анализа.
func (s *Store) SetDocumentUpload(ctx context.Context, id, uploadID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE documents SET last_upload_id = $1 WHERE id = $2`, uploadID, id)
	if err != nil {
		return fmt.Errorf("postgres: set document upload: %w", err)
	}
	return nil
}

// DeleteDocument удаляет документ пользователя, возвращает путь к файлу для удаления с диска.
func (s *Store) DeleteDocument(ctx context.Context, userID string, id int64) (string, error) {
	var owner, path string
	if err := s.pool.QueryRow(ctx,
		`SELECT user_id, stored_path FROM documents WHERE id = $1`, id,
	).Scan(&owner, &path); err != nil {
		return "", fmt.Errorf("postgres: find document: %w", err)
	}
	if owner != userID {
		return "", pgx.ErrNoRows
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM documents WHERE id = $1`, id); err != nil {
		return "", fmt.Errorf("postgres: delete document: %w", err)
	}
	return path, nil
}
