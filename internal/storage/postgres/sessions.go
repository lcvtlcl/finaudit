package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreateSession сохраняет токен сессии с временем истечения.
// Персистентное хранилище: сессии переживают перезапуск/редеплой приложения.
func (s *Store) CreateSession(ctx context.Context, token string, userID int64, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)`,
		token, userID, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create session: %w", err)
	}
	return nil
}

// GetSession возвращает userID по токену, если сессия существует и не истекла.
// Истёкшая сессия удаляется и трактуется как отсутствующая.
func (s *Store) GetSession(ctx context.Context, token string) (int64, bool, error) {
	var userID int64
	var expiresAt time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, expires_at FROM sessions WHERE token = $1`, token,
	).Scan(&userID, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("postgres: get session: %w", err)
	}
	if time.Now().After(expiresAt) {
		_, _ = s.pool.Exec(ctx, `DELETE FROM sessions WHERE token = $1`, token)
		return 0, false, nil
	}
	return userID, true, nil
}

// TouchSession продлевает срок жизни сессии (скользящее окно) при активности пользователя.
func (s *Store) TouchSession(ctx context.Context, token string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET expires_at = $2 WHERE token = $1`, token, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: touch session: %w", err)
	}
	return nil
}

// DeleteSession удаляет сессию (logout).
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	if err != nil {
		return fmt.Errorf("postgres: delete session: %w", err)
	}
	return nil
}
