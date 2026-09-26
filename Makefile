# Makefile — Central de Monitoramento (T1.9)

.PHONY: help build test test-race lint run migrate-up migrate-down docker-up docker-down docker-logs frontend-build load-test load-test-pg verify clean

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

load-test: ## Sobrecarga: N endpoints em burst sem double-run/dedlock (RNF-002/011)
	cd backend && go test -tags load ./internal/engine -run Load -v -count=1

load-test-pg: ## Sobrecarga contra o PostgreSQL REAL (container efêmero) — valida store pgx sob carga
	@set -e; \
	docker rm -f monitor-load-pg >/dev/null 2>&1 || true; \
	docker run -d --rm --name monitor-load-pg \
	  -e POSTGRES_PASSWORD=loadpg -e POSTGRES_USER=monitor -e POSTGRES_DB=monitor \
	  -p 127.0.0.1:55432:5432 postgres:15-alpine >/dev/null; \
	trap 'docker rm -f monitor-load-pg >/dev/null 2>&1 || true' EXIT; \
	printf "== aguardando postgres ==\n"; \
	for i in $$(seq 1 40); do \
	  docker exec monitor-load-pg pg_isready -U monitor -d monitor >/dev/null 2>&1 && break; \
	  sleep 1; \
	done; \
	cd backend && LOAD_TEST_PG_DSN='postgres://monitor:loadpg@127.0.0.1:55432/monitor?sslmode=disable' \
	  go test -tags load ./internal/engine -run Load -v -count=1

verify: ## Verificação local completa (vet + testes -race + build/audit frontend) — sem nenhum serviço remoto
	@set -e; \
	printf "== go vet ==\n"; (cd backend && go vet ./...); \
	printf "== go test -race ==\n"; (cd backend && go test -race ./...); \
	printf "== frontend build ==\n"; (cd frontend && npm ci && npm run build); \
	printf "== npm audit (high) ==\n"; (cd frontend && npm audit --audit-level=high)

clean: ## Remove binários
	rm -rf backend/bin