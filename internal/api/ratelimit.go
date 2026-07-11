package api

import (
	"sync"
	"time"
)

// rateLimiter — простой лимитер «N запросов за окно» по ключу (обычно userID).
//
// Нужен на создании платежа: без него один пользователь может нагенерировать
// сотни платежей в ЮKassa (мусор в кабинете, лишняя нагрузка, потенциальный абьюз).
// Реализация в памяти и без внешних зависимостей: для одного инстанса этого достаточно.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		hits:   make(map[string][]time.Time),
		limit:  limit,
		window: window,
	}
}

// allow сообщает, можно ли пропустить запрос по данному ключу,
// и заодно фиксирует попытку.
func (rl *rateLimiter) allow(key string) bool {
	if rl == nil || rl.limit <= 0 {
		return true
	}

	now := time.Now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// выкидываем всё, что вышло за окно
	fresh := rl.hits[key][:0]
	for _, t := range rl.hits[key] {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}

	if len(fresh) >= rl.limit {
		rl.hits[key] = fresh
		return false
	}

	rl.hits[key] = append(fresh, now)

	// подчищаем карту, чтобы она не росла бесконечно от разовых ключей
	if len(rl.hits) > 10000 {
		for k, v := range rl.hits {
			if len(v) == 0 || v[len(v)-1].Before(cutoff) {
				delete(rl.hits, k)
			}
		}
	}

	return true
}
