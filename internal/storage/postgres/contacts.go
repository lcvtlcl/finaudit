package postgres

import (
	"context"
	"fmt"
)

// CreateContactRequest сохраняет обращение из формы обратной связи.
func (s *Store) CreateContactRequest(ctx context.Context, name, contact, topic, message string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO contact_requests (name, contact, topic, message)
		VALUES ($1, $2, $3, $4)`,
		name, contact, topic, message)
	if err != nil {
		return fmt.Errorf("postgres: сохранение обращения: %w", err)
	}
	return nil
}
