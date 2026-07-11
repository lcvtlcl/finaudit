package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Credential — ключ провайдера ИИ пользователя. Секрет (api_key_enc) наружу не отдаётся;
// для UI используется KeyMasked (последние 4 символа из key_hint).
type Credential struct {
	ID        int64  `json:"id"`
	Provider  string `json:"provider"`
	Label     string `json:"label"`
	Model     string `json:"model"`
	KeyMasked string `json:"keyMasked"`
	Active    bool   `json:"active"`
}

// UserSettings — состояние настроек пользователя.
type UserSettings struct {
	ActiveCredentialID *int64
	TokensUsed         int64
}

// ErrNoActiveCredential — у пользователя нет активного ключа (используется глобальный fallback).
var ErrNoActiveCredential = errors.New("postgres: нет активного ключа ИИ")

// EnsureUserSettings создаёт строку user_settings, если её ещё нет.
func (s *Store) EnsureUserSettings(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_settings (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("postgres: ensure user_settings: %w", err)
	}
	return nil
}

// CreateCredential сохраняет зашифрованный ключ провайдера и возвращает его ID.
func (s *Store) CreateCredential(ctx context.Context, userID int64, provider, label string, keyEnc []byte, keyHint, model string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO ai_credentials (user_id, provider, label, api_key_enc, key_hint, model)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		userID, provider, label, keyEnc, keyHint, model,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("postgres: create credential: %w", err)
	}
	return id, nil
}

// ListCredentials возвращает ключи пользователя (без секретов), помечая активный.
func (s *Store) ListCredentials(ctx context.Context, userID int64) ([]Credential, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.provider, c.label, c.model, c.key_hint,
		        (us.active_credential_id = c.id) AS active
		 FROM ai_credentials c
		 LEFT JOIN user_settings us ON us.user_id = c.user_id
		 WHERE c.user_id = $1
		 ORDER BY c.created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list credentials: %w", err)
	}
	defer rows.Close()

	var out []Credential
	for rows.Next() {
		var (
			c      Credential
			hint   string
			active *bool
		)
		if err := rows.Scan(&c.ID, &c.Provider, &c.Label, &c.Model, &hint, &active); err != nil {
			return nil, fmt.Errorf("postgres: scan credential: %w", err)
		}
		c.KeyMasked = "••••" + hint
		c.Active = active != nil && *active
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteCredential удаляет ключ пользователя (только свой).
func (s *Store) DeleteCredential(ctx context.Context, userID, id int64) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM ai_credentials WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("postgres: delete credential: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// SetActiveCredential делает ключ активным (проверяя принадлежность пользователю).
func (s *Store) SetActiveCredential(ctx context.Context, userID, credID int64) error {
	var owner int64
	if err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM ai_credentials WHERE id = $1`, credID).Scan(&owner); err != nil {
		return fmt.Errorf("postgres: credential lookup: %w", err)
	}
	if owner != userID {
		return pgx.ErrNoRows
	}
	if err := s.EnsureUserSettings(ctx, userID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE user_settings SET active_credential_id = $1, updated_at = now() WHERE user_id = $2`,
		credID, userID)
	if err != nil {
		return fmt.Errorf("postgres: set active credential: %w", err)
	}
	return nil
}

// ActiveCredentialSecret возвращает провайдера, модель и зашифрованный ключ активного ключа пользователя.
// ErrNoActiveCredential — если активный ключ не выбран.
func (s *Store) ActiveCredentialSecret(ctx context.Context, userID int64) (provider, model string, keyEnc []byte, err error) {
	row := s.pool.QueryRow(ctx,
		`SELECT c.provider, c.model, c.api_key_enc
		 FROM user_settings us
		 JOIN ai_credentials c ON c.id = us.active_credential_id
		 WHERE us.user_id = $1`,
		userID,
	)
	if err = row.Scan(&provider, &model, &keyEnc); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil, ErrNoActiveCredential
		}
		return "", "", nil, fmt.Errorf("postgres: active credential secret: %w", err)
	}
	return provider, model, keyEnc, nil
}

// AddTokens увеличивает счётчик израсходованных токенов пользователя.
func (s *Store) AddTokens(ctx context.Context, userID, n int64) error {
	if err := s.EnsureUserSettings(ctx, userID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE user_settings SET tokens_used = tokens_used + $1, updated_at = now() WHERE user_id = $2`,
		n, userID)
	if err != nil {
		return fmt.Errorf("postgres: add tokens: %w", err)
	}
	return nil
}

// GetUserSettings возвращает настройки пользователя (создавая пустые при отсутствии).
func (s *Store) GetUserSettings(ctx context.Context, userID int64) (UserSettings, error) {
	if err := s.EnsureUserSettings(ctx, userID); err != nil {
		return UserSettings{}, err
	}
	var us UserSettings
	err := s.pool.QueryRow(ctx,
		`SELECT active_credential_id, tokens_used FROM user_settings WHERE user_id = $1`, userID,
	).Scan(&us.ActiveCredentialID, &us.TokensUsed)
	if err != nil {
		return UserSettings{}, fmt.Errorf("postgres: get user settings: %w", err)
	}
	return us, nil
}

// UpdateProfile обновляет имя и компанию пользователя (кнопка «Изменить»).
func (s *Store) UpdateProfile(ctx context.Context, userID int64, name, company string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET name = $1, company = $2 WHERE id = $3`, name, company, userID)
	if err != nil {
		return fmt.Errorf("postgres: update profile: %w", err)
	}
	return nil
}
