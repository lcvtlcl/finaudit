# Процесс работы (WWPP)

Команда из 3 человек, ночь хакатона. Цель — не мешать друг другу и быстро мёржить.

## Ветки
- `main` — всегда рабочая, деплоится. Прямой пуш запрещён.
- Фича-ветки от `main`, по слою: `feat/ingest`, `feat/metrics`, `feat/llm`, `feat/deploy`, `fix/...`.

## Цикл
1. `git switch -c feat/<слой>`
2. Кодишь (в Kodik), коммитишь часто и мелко.
3. `git push -u origin feat/<слой>`
4. Открываешь Pull Request в `main`.
5. Быстрый ревью (можно через нашего ассистента — он читает папку), мёрж.

## Зоны (чтобы не было конфликтов)
- **Data/Backend** — `internal/ingest`, `internal/metrics`, `internal/storage`, `db/`
- **Fullstack** — `internal/api`, `internal/llm`, `web/`, промпты
- **DevOps** — `deploy/`, `Makefile`, CI, миграции

## Коммиты
Префиксы: `feat:`, `fix:`, `chore:`, `docs:`. Коротко и по делу.

## Священное правило
`.env` и любые ключи — НИКОГДА в git. Только `.env.example`.
