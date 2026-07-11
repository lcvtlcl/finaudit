.PHONY: run build tidy up down migrate sqlc lint

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

tidy:
	go mod tidy

up:
	docker compose -f deploy/docker-compose.yml up -d --build

down:
	docker compose -f deploy/docker-compose.yml down

# goose: миграции Postgres
migrate:
	goose -dir db/migrations postgres "$$POSTGRES_DSN" up

# sqlc: генерация типобезопасного кода из db/queries
sqlc:
	sqlc generate

lint:
	go vet ./...
