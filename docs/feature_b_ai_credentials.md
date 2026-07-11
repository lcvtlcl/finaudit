# Фича Б — персональные настройки ИИ (фундамент под мульти-провайдер)

Цель: ключи/модель/настройки хранятся **по юзеру в БД** (не глобально в рантайме).
Разблокирует мульти-провайдер (3.1/3.5/3.6), рабочую правку профиля (3.3), исчезновение демо-баннера после подключения (5).

## Схема БД (миграция 00004)

`ai_credentials` — подписанные ключи юзера под разных провайдеров:
- `id`, `user_id`→users, `provider` ('deepseek'|'openai'|…), `label` (подпись юзера),
  `api_key_enc BYTEA` (AES-256-GCM, `nonce||ciphertext`), `key_hint` (последние 4 символа для маскировки),
  `model`, `created_at`.

`user_settings` — состояние настроек юзера:
- `user_id` PK →users, `active_credential_id`→ai_credentials (какой ключ активен),
  `tokens_used`, `updated_at`.

## Шифрование

`internal/crypto` — AES-256-GCM. Ключ шифрования из env **`APP_ENC_KEY`** (base64, 32 байта).
Ключи провайдеров в открытом виде в БД не лежат. Если `APP_ENC_KEY` не задан — персональные
ключи отключены, работает глобальный fallback из `.env` (демо-режим).

## Реестр провайдеров (`internal/llm/providers.go`)

Провайдер → `{BaseURL, Models[], PricePer1MTokUSD, BalanceURL}`. Пока: deepseek (+ баланс), openai (заготовка).
Стоимость в $ = `tokens_used/1e6 * PricePer1MTokUSD`. Баланс — best-effort по BalanceURL, если провайдер отдаёт.

## API (заменяет глобальные /api/settings)

- `GET /api/providers` — провайдеры и их модели (для выпадашек).
- `GET /api/ai/credentials` — список ключей юзера (маскированные, без секрета).
- `POST /api/ai/credentials` — добавить `{provider,label,key,model}`.
- `DELETE /api/ai/credentials/{id}` — удалить.
- `POST /api/ai/credentials/{id}/activate` — сделать активным.
- `GET /api/settings` — теперь per-user: `tokens_used`, стоимость $, активный ключ/модель, баланс.
- `POST /api/profile` — правка `name`/`company` (кнопка «Изменить»).

## Конвейер /audit

При запросе залогиненного юзера берём его **активный** ключ (расшифровка), собираем per-request
llm-клиент (baseURL+key+model провайдера), после ответа пишем `AddTokens(userID, n)`.
Нет активного ключа → глобальный `.env` (демо).

## Контракт для дата-инженера

`internal/models.Transaction` **не трогаем**. Новые таблицы независимы. Store-методы — на `*postgres.Store`.
