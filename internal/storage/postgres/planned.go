package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/wwpp/finaudit/internal/models"
)

// CreatePlanned сохраняет запланированный платёж пользователя.
func (s *Store) CreatePlanned(ctx context.Context, userID string, p models.PlannedPayment) (int64, error) {
	dir := string(p.Direction)
	if dir != string(models.In) && dir != string(models.Out) {
		dir = string(models.Out)
	}
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO planned_payments (user_id, pay_date, amount, direction, purpose)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		userID, p.Date, p.Amount, dir, p.Purpose,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("postgres: create planned: %w", err)
	}
	return id, nil
}

// ListPlanned возвращает планируемые платежи пользователя по дате.
func (s *Store) ListPlanned(ctx context.Context, userID string) ([]models.PlannedPayment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, pay_date, amount, direction, purpose FROM planned_payments
		 WHERE user_id = $1 ORDER BY pay_date`, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list planned: %w", err)
	}
	defer rows.Close()

	var out []models.PlannedPayment
	for rows.Next() {
		var (
			p   models.PlannedPayment
			dir string
		)
		if err := rows.Scan(&p.ID, &p.Date, &p.Amount, &dir, &p.Purpose); err != nil {
			return nil, fmt.Errorf("postgres: scan planned: %w", err)
		}
		p.Direction = models.Direction(dir)
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePlanned удаляет запланированный платёж пользователя. ErrNoRows — не найдено/не владелец.
func (s *Store) DeletePlanned(ctx context.Context, userID string, id int64) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM planned_payments WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("postgres: delete planned: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
