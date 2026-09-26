# Makefile — Central de Monitoramento (T1.9)

.PHONY: help build test test-race lint run migrate-up migrate-down docker-up docker-down docker-logs frontend-build clean

help: ## Lista os alvos disponíveis
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## Compila o backend
	cd backend && go build ./... && go build -o bin/monitor ./cmd/monitor

test: ## Rodas os testes (memória, sem banco)
	cd backend && go test ./...

test-race: ## Testes com detector de corrida
	cd backend && go test -race ./...

lint: ## Lint (go vet + build)
	cd backend && go vet ./...

run: ## Sobe o monitor local com store em memória (dev)
	cd backend && DB_DSN= go run ./cmd/monitor

migrate-up: ## Aplica migrations no PostgreSQL (usa DB_DSN do .env)
	cd backend && go run ./cmd/migrate -dsn "$${DB_DSN}"

migrate-down: ## Destrói o schema (destrutivo!)
	cd backend && go run ./cmd/migrate -down -dsn "$${DB_DSN}"

docker-up: ## Sobe a stack (backend + postgres + nginx)
	docker compose up --build -d

docker-down: ## Derruba a stack
	docker compose down

docker-logs: ## Logs dos serviços
	docker compose logs -f --tail=100

frontend-build: ## Build do frontend (estáticos → frontend/dist)
	cd frontend && npm ci && npm run build

clean: ## Remove binários
	rm -rf backend/bin