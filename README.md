# Central de Monitoramento de APIs & Webhooks

Status Page + Logger — verificação periódica de health-check em endpoints
cadastrados, logs de latência/status e **status page pública em tempo real**.

| Doc | Descrição |
|-----|-----------|
| [`docs/requirements.md`](docs/requirements.md) | Requisitos funcionais (RF-001..029) e não funcionais (RNF-001..020) |
| [`docs/architecture.md`](docs/architecture.md) | Stack (Go · React · PostgreSQL), worker pools, SSE, Docker/Nginx |
| [`docs/tasks.md`](docs/tasks.md) | Plano de execução em 4 fases (checkboxes + critérios) |

---

## 🧱 Stack

| Camada | Tecnologia |
|--------|------------|
| Backend | **Go ≥ 1.22** (Chi · pgx) — scheduler + worker pool com goroutines |
| Frontend | **React + TypeScript + Tailwind CSS + Recharts** *(Fase 4)* |
| Banco | **PostgreSQL ≥ 15** (checks particionada por mês + rollups por minuto) |
| Tempo real | **SSE** (Server-Sent Events) *(Fase 3)* |
| Deploy | **Docker Compose** + Dockerfile multi-stage (distroless) + **Nginx** |

---

## ✅ Estado atual (Fases 1, 2 e 3 concluídas)

- **Fase 1 — Setup**: módulo Go, config por env, `pgxpool` + `/healthz` `/readyz`,
  migração inicial com particionamento, Dockerfile multi-stage (**~14 MB**),
  `docker-compose.yml` + Nginx (TLS, redirect, SSE sem buffer) e Makefile.
- **Fase 2 — Core Engine**:
  - `scheduler` — tick global, jitter ±20% (anti thundering herd), guarda de
    sobreposição `in_flight` (RF-011) e **backpressure não-bloqueante**.
  - `worker` — pool de K goroutines com canal bufferizado, **panic recovery**,
    graceful drain e métricas atômicas (dropped/failed/in-flight).
  - `checker` — HTTP check com timeout (RF-009), validação de status/body
    (RF-010) e classificação `UP/DOWN/DEGRADED` por latência (RF-008) +
    **guarda anti-SSRF** por padrão (S-01).
  - `storage` — **MemStore** (dev/testes) e **PgStore** (produção), escrita em
    **lote** + upsert de rollups.
  - `domain` — state machine de confirmação N/M (RF-013): DOWN após 3 falhas,
    UP após 2 sucessos; incidentes com abertura/fechamento (RF-017).
  - `broker` — pub/sub in-memory alimentando o SSE (Fase 3).
- **Fase 3 — API REST + SSE + Alertas**:
  - `auth` — signup/login com **bcrypt + JWT** (expiração), middleware de rotas
    admin e **rate limit** em login/signup (RNF-017).
  - `api` — CRUD de endpoints/grupos (T3.2), logs/stats via **rollups**
    (P50/P95/uptime — T3.3), status público sem segredos (T3.4) e **SSE
    `/api/v1/events`** com snapshot + heartbeat (T3.5/T3.6).
  - `notifier` — alertas por **e-mail (SMTP)** e **webhook** (Slack/Discord),
    janela de supressão, retry com backoff e auditoria em `notifications`
    (T3.7).

**Cobertura de testes** (com `-race`): todos os pacotes verdes — api, auth,
broker, checker, domain, engine, notifier, scheduler, storage, worker.

---

## 🌐 API — referência rápida

| Rota | Auth | Descrição |
|------|------|-----------|
| `POST /api/v1/auth/signup` | — | cria conta (rate limit) |
| `POST /api/v1/auth/login` | — | e-mail+senha → JWT (rate limit) |
| `/api/v1/admin/endpoints` | **JWT** | CRUD + `POST /test` (conectividade) |
| `/api/v1/admin/groups` | **JWT** | CRUD de grupos |
| `/api/v1/admin/checks` | **JWT** | logs brutos paginados/filtráveis |
| `/api/v1/admin/stats/*` | **JWT** | séries (rollups) + resumo uptime |
| `/api/v1/admin/notifications` | **JWT** | auditoria de alertas |
| `GET /api/v1/status` | — | status atual (sem segredos) |
| `GET /api/v1/incidents` | — | timeline de incidentes |
| `GET /api/v1/events` | — | SSE: snapshot + eventos + heartbeat |

```bash
# Exemplo: criar conta e monitorar um endpoint
TOKEN=$(curl -sk https://localhost/api/v1/auth/login -d '{"email":"adm@ex.com","password":"senha-segura-123"}' | jq -r .token)
curl -sk -H "Authorization: Bearer $TOKEN" https://localhost/api/v1/admin/endpoints/ \
  -d '{"name":"api-gw","url":"https://example.com/health","method":"GET","interval_seconds":60}'
```

---

## 🚀 Como rodar

### Rápido (Docker — produção local)

```bash
cp .env.example .env          # ajuste JWT_SECRET
docker compose up --build -d  # postgres + backend + nginx
curl -k https://localhost/healthz   # {"status":"ok"}
```

> Certificados TLS: gere self-signed em `deploy/nginx/certs/` ou use acme.sh.

### Desenvolvimento (backend com store em memória, sem Docker)

```bash
make run                      # DB_DSN vazio → MemStore
# http://localhost:8080/healthz · /readyz
```

### Testes

```bash
make test        # go test ./...
make test-race   # + detector de corrida
make lint        # go vet
```

---

## 📁 Estrutura

```
project-3/
├── backend/
│   ├── cmd/
│   │   ├── monitor/          # entrypoint (graceful shutdown + health)
│   │   └── migrate/          # runner de migrations (up/down)
│   ├── internal/
│   │   ├── api/              # REST admin/público + SSE
│   │   ├── auth/             # JWT + bcrypt (T3.1)
│   │   ├── broker/           # pub/sub p/ SSE
│   │   ├── checker/          # execução HTTP + classificação + anti-SSRF
│   │   ├── config/           # env → config validada
│   │   ├── domain/           # entidades + state machine N/M
│   │   ├── engine/           # orquestrador scheduler+pool+persistência
│   │   ├── migrate/          # migrations embarcadas (go:embed)
│   │   ├── notifier/         # alertas SMTP + webhook (T3.7)
│   │   ├── scheduler/        # produção de jobs
│   │   ├── storage/          # Store (MemStore · PgStore)
│   │   └── worker/           # pool de goroutines
│   └── Dockerfile            # multi-stage → distroless nonroot
├── deploy/nginx/nginx.conf   # reverse proxy + SSE sem buffer
├── docker-compose.yml
├── frontend/                 # placeholder (Fase 4)
└── docs/                     # requirements · architecture · tasks
```

---

## 🛤️ Roadmap (docs/tasks.md)

| Fase | Status |
|------|--------|
| 1 — Setup & Boilerplate | ✅ Concluída |
| 2 — Core Engine (scheduler + worker pool) | ✅ Concluída |
| 3 — API REST + SSE + Alertas | ✅ Concluída |
| 4 — UI/UX Frontend (dashboard + status page) | ⏳ Próxima |