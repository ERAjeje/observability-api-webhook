# 📡 APIs & Webhooks Monitoring Center

> 🌐 Leia esta página em [Português](README.md).

A complete **endpoint monitoring** system: operators register the
**health-checks** of their APIs and webhooks, the system probes them on
configurable intervals, classifies each check (`UP`/`DOWN`/`DEGRADED`), opens
and closes **incidents** through a confirmation window, and delivers everything
on a **public real-time status page** — with logs, charts and alerts.

Two experiences in the same product:

- 🌍 **Public** — the status page (`/`) shows each service grouped, its current
  state, latency/uptime charts (24h/7d/30d) and the incident timeline, updated
  **in real time via SSE** (UP→DOWN within ~5 s without refreshing). No
  secrets: headers/tokens never leave the admin panel.
- 🔐 **Operator** — creates an account in seconds and manages endpoints and
  groups, inspects raw logs and statistics, configures alerts (SMTP e-mail or
  Slack/Discord webhook) and the status page branding — all through a secure
  admin panel with JWT, rate limiting and **per-account quotas**.

> **Status:** 🟢 Phases 1–5 complete · September/2026 · full Docker stack
> (~14 MB API · PostgreSQL · React SPA · nginx). Go backend with our own
> **worker pool** and **scheduler** (no third-party libs), statistics 100%
> served from **pre-computed rollups**, and quality checks **100% local**
> (`make verify`) — no remote service.

---

## What the project does

### For the operator — in a few steps

1. **Create the account** (signup/login with bcrypt + 24 h JWT and per-IP/e-mail
   rate limiting).
2. **Register an endpoint** — URL, method, auth headers, check interval
   (account minimum), timeout, latency threshold, expected status/body
   fragment. A **connectivity test** validates before saving and the
   **anti-SSRF guard** blocks internal targets by default.
3. **The scheduler fires checks** with jitter (anti *thundering herd*) and the
   **worker pool** runs them in parallel with non-blocking backpressure. The
   **N/M state machine** confirms: **DOWN only after 3 failures** and **UP
   after 2 successes** (RF-013) — opening/closing **incidents** with duration.
4. **Track everything**: public real-time status page (SSE), filterable/paged
   raw logs, 24h/7d/30d charts and **alerts** (SMTP or webhook) with a
   suppression window, retry with backoff and audit trail.

### What the status page shows (public)

- Each service group and its current state (`UP`/`DOWN`/`DEGRADED`);
- Latency (P50/P95) and uptime charts per window (24h, 7d, 30d) — served from
  **pre-computed rollups**, never from the checks table;
- **Incident** timeline (open, closed, duration);
- Operator-configurable visual identity (dynamic `branding`);
- **Real time**: UP→DOWN reflected within ~5 s with no refresh (SSE).

> Endpoint **auth headers** are **encrypted at rest** (AES-256-GCM,
> `HEADERS_ENC_KEY`) and **never** appear outside the authenticated admin
> panel — not in the HTML, not on public routes (RNF-018).

### Alerts the system sends

- Incident open/close, via **e-mail (SMTP)** and/or **webhook**
  (Slack/Discord), with **suppression** (300 s between alerts for the same
  endpoint), retry with backoff (up to 3 attempts) and audit in `notifications`.

---

## Stack used

| Layer | Technology | Why |
|---|---|---|
| Backend | **Go** (Chi + pgx) | goroutine concurrency in the worker pool, ~14 MB distroless binary, no heavy deps |
| Scheduler + Pool | **hand-rolled** | global tick with ±20% jitter, `in_flight` guard (RF-011) and non-blocking backpressure — no external lib |
| Database | **PostgreSQL 15** | `checks` **partitioned by month** + per-minute `rollups` = scalable retention and dashboards |
| Real time | **SSE** (`/api/v1/events`) | connection snapshot + heartbeat (15 s): UP→DOWN in ~5 s with a simple server |
| Frontend | **Vite + React + TS + Tailwind + Recharts** | light SPA with **code-splitting** (Recharts out of the public bundle — RNF-015) |
| Authentication | **bcrypt + JWT (24 h)** | costly password hashing; protected admin routes + rate limiting (RNF-017) |
| Alerts | **SMTP + webhook** (fail-closed) | suppression + retry/backoff + audit; dynamic saved settings (env fallback) |
| At-rest security | **AES-256-GCM** (`HEADERS_ENC_KEY`) | endpoint headers/tokens encrypted in PostgreSQL; fail-closed without the key (S-05) |
| Per-account quotas | **`internal/quota`** | max endpoints, min interval and monthly checks projection → **429** (S-08) |
| Observability | **`/metrics` (Prometheus text)** | own registry, no external lib (RF-012) |
| Deploy | **Docker Compose** + nginx | one command brings it up; multi-stage Dockerfile → distroless nonroot; TLS (acme.sh) |
| Verification | **`make verify`** (100% local) | vet + `-race` tests + frontend build/audit; **no GitHub Actions / remote CI** |

### Engineering decisions worth mentioning

- **Statistics only from pre-computed rollups (RNF-014).** No dashboard ever
  scans the checks table (partitioned by month): the engine aggregates per
  minute (`count`, `ok_count`, latency sum, P50/P95) and chart routes read only
  those series. Constant query cost at any volume.
- **N/M confirmation state machine (RF-013).** Transitions are not reactive to
  a single failure: 3 consecutive failures for `DOWN`, 2 successes for `UP`,
  configurable window — incidents only open with consensus, avoiding false
  alarms.
- **Backpressure that never loses work.** The pool queue is buffered and
  `Submit` is non-blocking; when it overflows, the scheduler *reprocesses* on
  the next ticks. Validated by a load test that **caught a real scheduler
  double-run** (stale snapshot) — the fix revalidates each job's due time
  before enqueueing.
- **SSE with snapshot + heartbeat.** On connect, the client receives the full
  current state; afterwards, incremental events. Reconnection is trivial (new
  snapshot) and a 15 s heartbeat keeps the connection alive behind proxies
  (nginx without buffering in the path).
- **Secrets encrypted at rest (S-05).** Auth headers go to PostgreSQL as an
  AES-256-GCM `$seal_v1` envelope; decrypted only in the engine (to check) and
  in the admin panel. Without the key, the system proceeds **without headers**
  — the secret never leaks.
- **Per-account quotas (S-08).** An operator cannot self-raid the platform with
  1,000 endpoints checking every second: there is an endpoint cap, a minimum
  interval and a monthly check projection, with a clear `429` on create/update.
- **Local quality gate.** Validation is `make verify` (vet + `-race` tests +
  frontend build/audit), `make load-test` (memory) and `make load-test-pg`
  (real PostgreSQL), plus Playwright E2E — and a local simulation of the VPS
  deploy (`SKIP_TLS=1`).
- **SSRF guard by default (S-01).** Loopback/RFC1918/cloud-metadata targets are
  blocked on registration, unless `ALLOW_PRIVATE_TARGETS=true` (lab).

---

## Monorepo structure

```
project-3/
├── backend/
│   ├── cmd/
│   │   ├── monitor/              # HTTP entrypoint (graceful shutdown + health)
│   │   └── migrate/              # migrations runner (up/down)
│   ├── internal/
│   │   ├── api/                  # REST admin/public handlers + SSE + /metrics
│   │   ├── auth/                 # bcrypt + JWT + rate limiting (RNF-017)
│   │   ├── broker/               # in-memory pub/sub feeding SSE
│   │   ├── checker/              # HTTP execution + timeout + anti-SSRF
│   │   ├── config/               # env → validated config
│   │   ├── domain/               # entities + N/M state machine (RF-013)
│   │   ├── engine/               # orchestrator: scheduler + pool + persistence
│   │   ├── metrics/              # Prometheus text registry (RF-012)
│   │   ├── migrate/              # embedded migrations (go:embed)
│   │   ├── notifier/             # SMTP + webhook alerts, suppression/retry
│   │   ├── quota/                # per-account quotas → 429 (S-08)
│   │   ├── scheduler/            # job production (tick + jitter + guard)
│   │   ├── seal/                 # AES-256-GCM header encryption (S-05)
│   │   ├── settings/             # dynamic config (branding/alerts)
│   │   ├── storage/              # Store: MemStore · PgStore (batch + rollups)
│   │   └── worker/               # goroutine pool (backpressure)
│   ├── Dockerfile                # multi-stage → distroless nonroot (~14 MB)
├── frontend/                     # Vite + React + TS + Tailwind + Recharts SPA
│   └── tests/e2e/                # Playwright — 4 scenarios against the Docker stack
├── deploy/
│   ├── nginx/nginx.conf          # TLS reverse proxy + unbuffered SSE + /metrics
│   └── provision.sh              # VPS bootstrap (Docker + acme.sh TLS)
├── docs/                         # requirements · architecture · tasks · security-review
├── .env.example                  # configuration template (no credentials)
├── docker-compose.yml            # postgres + backend + nginx
└── Makefile                      # orchestrator (build, test, verify, load-test*…)
```

### REST API (`/api/v1`)

| Group | Main routes |
|---|---|
| Public | `GET /status` · `GET /incidents` · `GET /config` · `GET /status/{id}/stats/summary` · `GET /status/{id}/stats/series` · `GET /events` (SSE) |
| Panel (`/admin`, JWT) | CRUD `/endpoints` (+ `POST /test`) · CRUD `/groups` · `GET /checks` · `GET /stats/*` · `GET|PUT /settings` · `GET /notifications` |
| Auth | `POST /signup` · `POST /login` |
| Probes | `GET /healthz` · `GET /readyz` · `GET /metrics` (Prometheus) |

```bash
# Example: create an account and monitor an endpoint
TOKEN=$(curl -sk https://localhost/api/v1/auth/login \
  -d '{"email":"adm@ex.com","password":"segura-123"}' | jq -r .token)
curl -sk -H "Authorization: Bearer $TOKEN" \
  https://localhost/api/v1/admin/endpoints/ \
  -d '{"name":"api-gw","url":"https://example.com/health","method":"GET","interval_seconds":60}'
```

Contract details, data model and decisions: [docs/architecture.md](docs/architecture.md).

---

## Running in development

### Prerequisites

- **Docker with Compose v2** (recommended — no Go, Node or PostgreSQL needed);
- Git (to clone). For native backend, **Go ≥ 1.22**; for the frontend,
  **Node ≥ 20** / npm.

### Step by step (recommended)

```bash
# 1) clone and enter the project
git clone <repo-url> && cd project-3

# 2) generate .env (secrets are never versioned)
cp .env.example .env        # adjust JWT_SECRET (or let provision.sh do it)

# 3) build the SPA and bring everything up: postgres + backend + nginx (80/443)
make frontend-build
make docker-up              # or: docker compose up --build -d
```

Wait for the backend to become `healthy` (10–40 s on the first distroless image
build) and check the probes:

```bash
curl -k https://localhost/healthz       # → ok
curl -k https://localhost/readyz        # → {"status":"ok"}
```

> **Migrations run automatically** (`AUTO_MIGRATE`) on the API entrypoint.
> Local TLS uses a self-signed cert (generate in `deploy/nginx/certs/` or use
> `provision.sh`).

### What is accessible

| What | URL | Notes |
|---|---|---|
| 🌍 Public status page | `https://localhost/` | real-time state per group + charts |
| 🔐 Admin panel | `https://localhost/admin/login` | sign up / log in |
| 📊 Metrics | `https://localhost/metrics` | Prometheus format (protect by firewall — S-11) |
| 🗄️ PostgreSQL | internal to Docker | not exposed on the host; use `docker compose exec postgres psql` |

### Demo account

The project **does not seed demo data** — creating the account and endpoints is
part of the walkthrough below. The E2E suite uses `e2e@monitor.test` /
`e2e-senha-segura-123` and provisions everything by itself.

### Execution alternatives

| Option | Command | When to use |
|---|---|---|
| **Full Docker stack** | `make docker-up` | ✨ recommended to evaluate everything |
| **Native backend (MemStore)** | `make run` | backend work in `:8080` without a DB |
| **Load (memory)** | `make load-test` | validates 800-endpoint burst exactly-once |
| **Load (real PostgreSQL)** | `make load-test-pg` | same against an ephemeral PostgreSQL container |
| **Full local verification** | `make verify` | vet + `-race` tests + frontend build/audit |

### Configuration (`.env`)

No passwords in the repository; `provision.sh` generates random `DB_PASSWORD`,
`JWT_SECRET` and `HEADERS_ENC_KEY`. Key variables to test business rules:

| Variable | Default | Effect |
|---|---|---|
| `JWT_SECRET` | — | token signing (required in production) |
| `DB_PASSWORD` | `monitor` | PostgreSQL password |
| `HEADERS_ENC_KEY` | empty (plaintext in dev) | encrypts headers at rest (S-05) — 64 hex |
| `MAX_ENDPOINTS_PER_ACCOUNT` | `50` | endpoint quota per account (S-08) |
| `MIN_CHECK_INTERVAL_SECONDS` | `10` | minimum interval per account (S-08) |
| `CHECKS_PER_MONTH_QUOTA` | `0` (derived) | projected monthly check cap (S-08) |
| `ALLOW_PRIVATE_TARGETS` | `false` | allows internal targets on registration (SSRF guard) |
| `FAIL_THRESHOLD` / `SUCCESS_THRESHOLD` | `3` / `2` | confirmation window (RF-013) |
| `SMTP_*` · `NOTIFY_WEBHOOK_URL` · `NOTIFY_ENABLED` | off | alert channels (T3.7) |

Full commented template: [.env.example](.env.example).

---

## Using the product (demo walkthrough)

> **10 seconds:** open `https://localhost/` and see the public status page — and
> at `https://localhost/admin/login` create your account and register the first
> endpoint.

**Full walkthrough for a live presentation:**

1. **Create an account** at `https://localhost/admin/login` (signup with
   e-mail + password). The panel opens with the endpoint list.
2. **Register 2 endpoints** (e.g., `https://api.github.com/zen` and
   `https://example.com/health`) with 1–5 min intervals. Use the
   **connectivity test** and watch the panel list the state, next checks, and
   charts start to form.
3. **Real time (the differentiator)** — in the lab with
   `ALLOW_PRIVATE_TARGETS=true`, register a local target and **take it down**:
   within ~5 s the public status page (in another tab, via SSE) shows `DOWN`, an
   **incident** opens in the timeline, and it closes when you bring the target
   back (state machine: 3 failures / 2 successes).
4. **Alerts** — in *Settings* configure a webhook (Slack/Discord) or SMTP and
   watch the events logged in the `notifications` audit. Tip: point an endpoint
   at your `https://discord.com/api/webhooks/...` to see the alert arriving.
5. **Quotas (S-08)** — try registering endpoint #`MAX+1` or one with
   `interval_seconds` below the account minimum: the API answers **429** with a
   friendly message that surfaces right in the panel form.
6. **Metrics** — `https://localhost/metrics` exposes the pool, queue, checks per
   result, P50/P95 latency and SSE events (Prometheus format).
7. **Load** — `make load-test` and `make load-test-pg` prove the pipeline under
   burst (800 endpoints, exactly-once, 0 drops).

---

## Quality and tests

- **Backend**: `make verify` → `go vet` + `go test -race ./...`. All packages
  green: `api, auth, broker, checker, domain, engine, metrics, notifier, quota,
  scheduler, seal, storage, worker`.
- **Load**: `make load-test` (memory) and `make load-test-pg` (real
  PostgreSQL) — **800 endpoints in burst**: 0 drops, 0 double-runs, peak
  in-flight ≤ pool; backpressure without deadlock. The `load` test caught a
  real scheduler double-run (fix documented in code).
- **E2E**: Playwright — 4 scenarios against the Docker stack (UC-01/UC-05:
  incident + status page via SSE, status page/branding, admin login + CRUD,
  alert settings). `cd frontend && npx playwright test`.
- **Security**: sweeps documented in
  [docs/security-review.md](docs/security-review.md) (S-01..S-24) with
  govulncheck + gosec in local verification — no remote CI.
- **Deploy**: local `make verify` + `SKIP_TLS=1` smoke of
  [deploy/provision.sh](deploy/provision.sh) validating `.env` → build → stack →
  readyz in a sandbox.

---

## Documentation

| Document | Contents |
|---|---|
| [docs/requirements.md](docs/requirements.md) | Functional (RF-001..029) and non-functional (RNF-001..020) requirements, use cases |
| [docs/architecture.md](docs/architecture.md) | Architecture, data model (partitions, rollups), SSE, ADRs |
| [docs/tasks.md](docs/tasks.md) | Execution plan in 5 phases (checkboxes + acceptance criteria) |
| [docs/security-review.md](docs/security-review.md) | Security sweeps (S-01..S-24) and finding status |
| [deploy/README.md](deploy/README.md) | VPS deploy: provisioning, acme.sh TLS, operations, firewall |
| [project.md](project.md) | Project map and current state |

---

## Git workflow

`develop` is the integration track (Conventional Commits); the remote is
**optional and used only as a backup** — deliberately **no GitHub Actions / CI**
(all validation is local via `make verify`). New features land as small commits
with clear messages (`feat:`/`fix:`/`docs:`).

## Releases

| Tag | Contents | Date |
|---|---|---|
| `v1.0.0` (planned) | Phases 1–5 — full engine, API + SSE + alerts, status page + admin frontend, S-05/S-08 hardening | Sep/2026 |

> Status page identity is **operator-configurable** (title and branding via
> `PUT /api/v1/admin/settings`) — RF-023; the SPA uses a default light theme
> with an easy palette to tweak in `frontend/`.