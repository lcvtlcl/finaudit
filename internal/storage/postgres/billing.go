package postgres

import (
	"context"
	"fmt"
	"time"
)

// Subscription — текущая подписка пользователя.
type Subscription struct {
	UserID    int64
	Plan      string // free | pro
	Status    string // active | inactive
	ExpiresAt *time.Time
}

// Active — подписка активна и не истекла.
func (s Subscription) Active() bool {
	if s.Status != "active" {
		return false
	}
	if s.ExpiresAt == nil {
		return false
	}
	return s.ExpiresAt.After(time.Now())
}

// CreatePayment сохраняет созданный платёж в статусе pending.
func (s *Store) CreatePayment(ctx context.Context, userID int64, paymentID, plan, amount, currency string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO payments (user_id, payment_id, plan, amount, currency, status)
		VALUES ($1, $2, $3, $4::numeric, $5, 'pending')
		ON CONFLICT (payment_id) DO NOTHING`,
		userID, paymentID, plan, amount, currency)
	if err != nil {
		return fmt.Errorf("postgres: создание платежа: %w", err)
	}
	return nil
}

// PaymentRow — платёж из БД (нужен, чтобы понять, кому и какой план активировать).
type PaymentRow struct {
	UserID int64
	Plan   string
	Status string
}

// GetPaymentByExternalID возвращает платёж по идентификатору ЮKassa.
func (s *Store) GetPaymentByExternalID(ctx context.Context, paymentID string) (PaymentRow, error) {
	var p PaymentRow
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, plan, status FROM payments WHERE payment_id = $1`, paymentID).
		Scan(&p.UserID, &p.Plan, &p.Status)
	if err != nil {
		return PaymentRow{}, fmt.Errorf("postgres: платёж %s не найден: %w", paymentID, err)
	}
	return p, nil
}

// MarkPaymentSucceeded помечает платёж оплаченным (идемпотентно).
func (s *Store) MarkPaymentSucceeded(ctx context.Context, paymentID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE payments SET status = 'succeeded', paid_at = now()
		WHERE payment_id = $1 AND status <> 'succeeded'`, paymentID)
	if err != nil {
		return fmt.Errorf("postgres: обновление платежа: %w", err)
	}
	return nil
}

// MarkPaymentCanceled помечает платёж отменённым.
func (s *Store) MarkPaymentCanceled(ctx context.Context, paymentID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE payments SET status = 'canceled'
		WHERE payment_id = $1 AND status = 'pending'`, paymentID)
	if err != nil {
		return fmt.Errorf("postgres: отмена платежа: %w", err)
	}
	return nil
}

// ActivateSubscription включает (или продлевает) подписку пользователя.
// Если подписка ещё активна — новый период добавляется к остатку, а не съедает его.
func (s *Store) ActivateSubscription(ctx context.Context, userID int64, plan string, periodDays int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO subscriptions (user_id, plan, status, expires_at, updated_at)
		VALUES ($1, $2, 'active', now() + ($3 || ' days')::interval, now())
		ON CONFLICT (user_id) DO UPDATE SET
			plan       = EXCLUDED.plan,
			status     = 'active',
			expires_at = GREATEST(COALESCE(subscriptions.expires_at, now()), now())
			             + ($3 || ' days')::interval,
			updated_at = now()`,
		userID, plan, periodDays)
	if err != nil {
		return fmt.Errorf("postgres: активация подписки: %w", err)
	}
	return nil
}

// GetSubscription возвращает подписку пользователя. Если записи нет — бесплатный план.
func (s *Store) GetSubscription(ctx context.Context, userID int64) (Subscription, error) {
	sub := Subscription{UserID: userID, Plan: "free", Status: "inactive"}
	err := s.pool.QueryRow(ctx, `
		SELECT plan, status, expires_at FROM subscriptions WHERE user_id = $1`, userID).
		Scan(&sub.Plan, &sub.Status, &sub.ExpiresAt)
	if err != nil {
		// записи нет — пользователь на бесплатном тарифе, это не ошибка
		return Subscription{UserID: userID, Plan: "free", Status: "inactive"}, nil
	}
	return sub, nil
}
