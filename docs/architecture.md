# Arquitetura — Central de Monitoramento de APIs & Webhooks (Status Page + Logger)

| Campo        | Valor                                              |
|--------------|----------------------------------------------------|
| **Produto**  | Central de Monitoramento de APIs & Webhooks        |
| **Base**     | `docs/requirements.md` v1.0                        |
| **Versão**   | 1.0 (draft)                                        |
| **Data**     | Setembro/2026                                      |
| **Status**   | Proposto — aguardando validação técnica            |

---

## 1. Visão Arquitetural

### 1.1 Princípios

1. **O estado do monitoramento é a fonte da verdade** — classificação `UP/DOWN/DEGRADED`,
   janelas de incidentes e agregações são calculadas e persistidas **no backend**; a UI apenas
   apresenta o estado (RF-008, RF-013, RF-017).
2. **Checagem é um job idempotente e tolerante a falhas** — cada execução é autônoma,
   re-agendável e nunca bloqueia o processo de outras checagens (RF-011, RF-012, RNF-002).
3. **Confirmação por janela contra falso positivo** — transições de estado exigem `N` falhas
   ou `M` sucessos consecutivos; uma falha isolada não derruba o status (RF-013, UC-02).
4. **Tempo real por push, com snapshot de reconexão** — SSE entrega eventos; na reconexão o
   cliente recebe o estado atual completo, sem divergência (RF-021, RNF-009, RNF-010).
5. **Gráficos sempre de dados agregados** — a UI nunca lê logs brutos; consulta rollups
   pré-calculados para renderização eficiente (RF-015, RF-020, RNF-014).
6. **Área pública separada da área administrativa** — status page sem autenticação, rotas de
   gestão protegidas e com rate-limit; segredos nunca vazam para o público (RF-006, RNF-017, RNF-018).
7. **Deploy simples e portátil** — Docker Compose local = mesmo orquestrador do VPS, com Nginx
   como único ponto de entrada (RNF-003).

### 1.2 Visão de Componentes

```
┌───────────────────────────────────────────────────────────────────────────┐
│                         FRONTEND — React + TypeScript                      │
│                                                                            │
│  Página de status (pública)        Painel administrativo (protegido)       │
│  · Tailwind CSS · Recharts/Chart.js · customização da marca (RF-023)       │
│  · SSE client (EventSource)        · CRUD de endpoints + alertas           │
│  · gráficos: uptime %, latência P50/P95/P99, timeline de incidentes        │
│                                                                            │
│  Bibliotecas: react · react-router · @tanstack/react-query · Recharts      │
└───────────────────────┬──────────────────────────────┬─────────────────────┘
                        │ GET /api/v1/status (público) │ GET/POST /api/v1/admin/* (auth)
                        │ GET /api/v1/events (SSE)     │
                        ▼                              ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                API + WORKER — Go (net/http + chi router)                   │
│                                                                            │
│  ┌─────────────────────────┐        ┌─────────────────────────────────┐    │
│  │  HTTP API (admin)       │        │  Scheduler (agendador)          │    │
│  │  · auth (JWT + bcrypt)  │        │  · tick global (ex.: 10s)       │    │
│  │  · CRUD endpoints/grupos│        │  · varre endpoints atrasados    │    │
│  │  · consulta logs/rollups│        │  · enfileira jobs → buffer ch   │    │
│  └───────────┬─────────────┘        └───────────────┬─────────────────┘    │
│              │                                     │ jobs                  │
│              ▼                                     ▼                        │
│  ┌─────────────────────────┐        ┌─────────────────────────────────┐    │
│  │  Event Broker (SSE hub) │        │  Worker Pool (K goroutines)    │    │
│  │  · pub/sub in-memory    │◀──ev──│  · HTTP check c/ timeout       │    │
│  │  · snapshot no connect  │        │  · state machine UP/DOWN/DEG   │    │
│  │  · heartbeat 15s        │        │  · grava check + rollups       │    │
│  └───────────┬─────────────┘        └───────────────┬─────────────────┘    │
│              │ eventos (fan-out)                    │ inciden/alertas      │
│              ▼                                      ▼                      │
│  SSE /api/v1/events  ───────────────▶  Notifier (e-mail SMTP · webhook)    │
└──────────────────────────────┬─────────────────────────────────────────────┘
                               │ pgx pool (bulk insert / upsert transacional)
                               ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                  POSTGRESQL  (PostgreSQL ≥ 15 + pgx)                      │
│                                                                            │
│  users · endpoints · check_groups · checks (particionado por mês) ·        │
│  check_rollups_minute · incidents · notifications (auditoria)              │
│  ▸ índices: (endpoint_id, ts DESC) · (endpoint_id, status_class, ts)       │
│  ▸ partial index p/ endpoints ativos · constraints de campos              │
└───────────────────────────────────────────────────────────────────────────┘

Infra (local e VPS): Docker Compose
  backend (Go) · frontend (build → estático servido pelo Nginx) ·
  postgres · nginx (reverse proxy + TLS)   —  env/prod separados, volumes p/ dados
```

### 1.3 Fluxo de Dados — Checagem de Health (ponta a ponta)

```
Scheduler (tick 10s)
   │  1. SELECT endpoints WHERE ativo AND next_check_at <= now
   │     · distribui com jitter p/ evitar "thundering herd" (RF-011)
   ▼
Jobs → channel bufferizado
   ▼
Worker Pool (K goroutines)
   │  a. consome job; GET/HEAD com timeout (RF-009) + validação (RF-010)
   │  b. mede latência; classifica UP/DOWN/DEGRADED (RF-008, RF-004)
   │  c. aplica state machine (janela N/M falhas/sucessos) (RF-013)
   │  d. grava check + upsert rollup (RF-014, RF-015)
   │  e. transição de estado?
   │       ├─ abre/fecha incidente (RF-017)
   │       ├─ notifier → alerta (RF-025..RF-028), com supressão (RF-027)
   │       └─ evento → broker → SSE p/ status page (RF-021)
   ▼
PostgreSQL → disco | SSE → clientes conectados | e-mail/webhook → alertas
```

### 1.4 Limites e invariantes chave

| Invariante                                          | Garantia                                             |
|-----------------------------------------------------|------------------------------------------------------|
| Um endpoint tem **1 execução em voo**               | guarda `in_flight` por endpoint + `next_check_at` só avança após conclusão |
| `DOWN` exige **N falhas** / `UP` exige **M sucessos** | state machine com contadores consecutivos (RF-013)   |
| Incidente aberto tem **horário de início exato**    | primeira falha confirmada da janela (RF-017)          |
| Checagem atrasada **nunca stacka**                  | job com deadline/timeout; resultado sempre registrado |
| Alerta só em **transição de estado**                | notifier ignora repetições (RF-027, RNF-007)          |
| Ação de endpoint X nunca atinge dado de outro Y     | queries escopadas; status page expõe só o agregado    |

---

## 2. Stack Tecnológica

| Camada      | Tecnologia                                       | Versão mín. | Justificativa (traceabilidade)                    |
|-------------|--------------------------------------------------|-------------|---------------------------------------------------|
| Backend     | **Go** (net/http) + router **Chi**               | Go ≥ 1.22   | Goroutines/pools p/ checagem em massa (RF-011, RNF-002); binário estático p/ Docker |
| Frontend    | **React + TypeScript** + **Tailwind CSS** + **Recharts** | React ≥ 18 | RF-019..024 — SPA leve, gráficos de métricas (RNF-014) |
| Banco       | **PostgreSQL** + **pgx** (pool)                  | ≥ 15        | RF-014..018 — rollups, particionamento por tempo, índices |
| Tempo real  | **Server-Sent Events (SSE)**                     | —           | RF-021, RNF-009..011 — unidirecional, 1 conexão HTTP |
| Alertas     | SMTP (e-mail) + Webhook (Slack/Discord)          | —           | RF-025..029, RNF-005..008 — assíncrono/auditável   |
| Deploy      | **Docker Compose** + **Nginx** reverse proxy     | —           | RNF-003 — mesmo orquestrador em dev e prod         |

**Decisões de stack (com justificativa):**

| Decisão | Opção escolhida | Por que (alternativas descartadas) |
|---------|-----------------|-------------------------------------|
| Backend | **Go** | Concorrência nativa (goroutines/canais) é o encaixe perfeito para checagens em massa; binário estático simplifica Docker multi-stage; Node.js exigiria worker threads event-loop com mais cerimônia para o mesmo resultado. |
| Banco | **PostgreSQL** | Rollups + particionamento + índices resolvem retenção/consulta; TimescapeDB/Redis adicionam infra sem ganho no MVP (Redis fica como evolução p/ broker multi-instância, ver §4.6). |
| Gráficos | **Recharts** | Declarativo e integrado ao React; Chart.js é alternativa viável — decisão final deixa-se para benchmark de renderização com rollups (RNF-014). |
| Tempo real | **SSE** | Ver §4 — decisão detalhada na comparação SSE vs WebSockets. |

---

## 3. Arquitetura do Sistema — Checagens em Massa com Goroutines e Worker Pools

### 3.1 Modelo de concorrência

O backend roda **um único processo** contendo API HTTP e o runtime de checagens. O
gerenciamento dos jobs é feito em **três estágios com canais**, inspirado no padrão
*distributor → job queue → worker pool*:

```
                              ┌──────────────────────────────┐
        endpoints ativos ────▶│  SCHEDULER (goroutine ticker)│
              (next_check_at) │  tick a cada 10s             │
                              └──────────────┬───────────────┘
                                             │ job{endpoint_id, url, ...}
                                             ▼
                              ┌──────────────────────────────┐
                              │  JOB QUEUE (chan Job)        │
                              │  buffer cap. (ex.: 1.000)    │
                              └──────────────┬───────────────┘
                                             ▼
   ┌───────────────────────────────────────────────────────────────────┐
   │  WORKER POOL — K goroutines (ex.: 10..50)                         │
   │  · cada worker: for job := range jobs → executa check             │
   │  · graceful drain: fecha channel + WaitGroup                       │
   │  · backpressure: buffer cheio ⇒ scheduler prioriza p/ próximo tick │
   └───────────────────────────────────────────────────────────────────┘
```

**Regras operacionais do pool (mapeamento p/ requisitos):**

| Regra | Implementação | Requisito |
|-------|---------------|-----------|
| Sem sobreposição por endpoint | mapa `in_flight[endpoint_id]` (mutex) consultado pelo scheduler; `next_check_at` avança só após conclusão do job | RF-011 |
| Timeout por endpoint | `http.Client{Timeout: timeout_ms}` + `context.WithTimeout` por job | RF-009 |
| Checks em massa sem "thundering herd" | jitter aleatório (±20% do intervalo) + redutor de prioridade por endpoint muito atrasado | RF-007, RNF-002 |
| Crash-tolerant | ao subir, o processo recalcula `next_check_at` para endpoints ativos (idempotente) | RNF-002 |
| Ban/DB lento não para o agendador | fila de gravação com *bulk insert* em lote (batch flush a cada 1s/100 registros) | RF-012, RNF-004 |
| Troca de configuração sem restart | endpoints e intervalos lidos do banco a cada tick (cache com TTL curto invalida no CRUD) | RF-001..005 |

### 3.2 Tamanho do pool e backpressure

- **`WORKER_POOL_SIZE`** configurável (env). Heurística de partida: métrica de
  `intervalo mínimo × volume de endpoints`, ex.: 100 endpoints com checks a cada 1 min
  ⇒ pico de ~1,7 jobs/s ⇒ pool de **10–20 workers** folgados.
- **Buffer do canal** com limite (ex.: `POOL_QUEUE_SIZE=1000`). Se o buffer enche
  (pico sustentado, ex.: 1.000 endpoints em 1 min), o scheduler **não bloqueia** —
  registra o déficit e reprocessa no próximo tick, com métrica `queue_starved` para
  observabilidade (o requisito é nunca stackar checagem, não estourar memória).
- Cada worker é **uma goroutine** que consome `for job := range jobs`. O pool é
  criado com `sync.WaitGroup` para **graceful shutdown**: `signal.SIGTERM` →
  pára scheduler → fecha channel → aguarda jobs em voo (com deadline) → drena.

### 3.3 State machine de status (por endpoint, em memória → persistência)

```
        (primeira checagem)
   UNKNOWN ── sucesso ──▶ UP ── latência > limite ──▶ DEGRADED
       ▲                    │  ▲                        │
       │                    │  │                        │
       │        sucessos(M) │  │ falhas(N)              │
       │                    ▼  │   (janela consecutiva) │
       │                   DOWN◀────────────────────────┘
       └───────────── registro de novo incidente ───────┘

  · contadores e estado mantidos por endpoint (estrutura em memória, mutex)
  · transição confirmada ⇒ persistência do novo estado + abertura/fechamento
    de incidente + evento SSE + notificação (transições são os únicos gatilhos)
```

A state machine vive em **memória** (por performance) e é **materializada no banco**
a cada transição — os `checks` crus guardam o histórico completo para auditoria e
pós-mortem. Isso satisfaz RF-013/017 sem consulta pesada a cada tick.

### 3.4 Adequação ao requisito "checagens em massa"

- **Custo por checagem**: 1 goroutine curta (~latência da chamada), 1 conexão HTTP
  reutilizável, 1 linha no batch de gravação → o modelo escala para **centenas de
  endpoints com intervalos de 1 min** numa única instância da VPS.
- **Crescimento futuro**: pool por *shard* de endpoints (ex.: `endpoint_id % N`),
  múltiplas réplicas com **lock distribuído em Postgres** (`pg_advisory_lock`) para
  divisão de trabalho entre processos — documentado, mas fora do MVP.

---

## 4. Comunicação em Tempo Real — SSE

### 4.1 Comparação SSE vs WebSockets

| Critério                 | SSE                                         | WebSockets                              |
|--------------------------|---------------------------------------------|------------------------------------------|
| Direção                  | **1 via** (servidor → cliente) ✔            | 2 vias (bidirecional)                    |
| Protocolo                | HTTP puro (EventSource) ✔                   | Upgrade/WS próprio                       |
| Reconexão automática     | Nativa no navegador ✔                       | Manual                                   |
| Headers/cookies na reconexão | Sim ✔                                  | Não por padrão                           |
| Estado no cliente        | **Server push puro, sem state** ✔           | Requer protocolo de estado/ack           |
| Suporte a proxies/Nginx  | Sim, com `proxy_buffering off` ✔            | Requires upgrade header/HTTP2 caveats    |
| Full-duplex              | Não (não precisamos)                        | Sim (não precisamos) ✔                   |

### 4.2 Decisão: **SSE** (Server-Sent Events)

O caso de uso é **unidirecional por natureza**: a status page apenas *recebe*
atualizações de status — o cliente nunca envia comandos em tempo real (RF-021,
RNF-009). SSE oferece reconexão nativa, roda sobre HTTP simples (sem estado de
conexão binária para gerenciar) e atravessa o Nginx com uma config minimalista.
WebSockets ficam como evolução se um dia houver necessidade de interação
bidirecional (ex.: painel operando ao vivo), sem mudar o contrato REST.

### 4.3 Modelo de pub/sub (Event Broker)

```
Worker (transição de estado/check novo)
   │  pub(evento{kind: status|rollup, endpoint_id, payload})
   ▼
┌──────────────────────────────────────────────┐
│  EVENT BROKER (in-memory):                   │
│  · map[channelID]map[subscriber]chan Event   │
│  · mutex p/ pub/sub — fan-out por cópia      │
│  · canais: "status" (todos os clientes)      │
└──────────────────────┬───────────────────────┘
                       ▼
HTTP handler GET /api/v1/events  (EventSource/SSE)
   · Content-Type: text/event-stream
   · fecha se o subscriber cair (deadline/timeout)
   · envia SNAPSHOT completo na abertura da conexão  ← RNF-010
   · heartbeat "ping" a cada 15 s                   (mantém proxy viva)
```

### 4.4 Semântica de reconexão (RNF-010)

1. Cliente abre `EventSource('/api/v1/events')`.
2. No `onconnect`, o handler envia **snapshot atual** (estado de todos os endpoints +
   incidentes em andamento + últimos rollups) em **1 único evento** — o navegador renderiza o
   estado verdadeiro sem depender de eventos anteriores perdidos.
3. A partir daí, eventos incrementais (`status_changed`, `rollup_updated`) fluem.
4. Se a conexão cair, o `EventSource` reconecta sozinho (com backoff interno do browser);
   na nova conexão o snapshot é reenviado ⇒ **sem divergência visual**.

### 4.5 SSE no Nginx (config-chave)

- `proxy_buffering off;` (impede bufferização do stream), `proxy_cache off;`,
  `proxy_read_timeout 60s;`, `X-Accel-Buffering: no` do backend.

### 4.6 Escalabilidade do broker

O broker in-memory serve o MVP **single-instance**. Para múltiplas réplicas (fase 2):
substituir o fan-out por **Redis Pub/Sub** (todos os workers publicam, cada réplica
subscreve e fan-out local para seus clientes SSE) — decisão postergada, sem impacto
no contrato do frontend (EventSource continua sendo a interface).

---

## 5. Banco de Dados — PostgreSQL

### 5.1 Modelagem principal

| Tabela | Propósito | Campos-chave | Notas |
|--------|-----------|--------------|-------|
| `users` | Acesso ao painel | id, email, password_hash (bcrypt), created_at | RNF-019 |
| `check_groups` | Agrupamento visual/alertas | id, name, display_order | RF-005 |
| `endpoints` | Cadastro gerenciado | id, group_id, name, url, method, headers(jsonb), body, interval_seconds, timeout_ms, lat_threshold_ms, expect_status, expect_body, active, next_check_at | RF-001..005; partial index `(active) WHERE active` |
| `checks` | Log bruto de cada checagem | id, endpoint_id, checked_at, status_class, http_status, latency_ms, error_detail | RF-014; **particionado por mês**; idx `(endpoint_id, checked_at DESC)` |
| `check_rollups_minute` | Agregação p/ gráficos | endpoint_id, bucket(ts minute), count, ok_count, avg_latency_ms, p95_latency_ms | RF-015, RNF-014; upsert por minuto |
| `incidents` | Janelas de downtime | id, endpoint_id, started_at, ended_at, duration_ms, resolution | RF-017; `ended_at NULL` = aberto |
| `notifications` | Auditoria de alertas | id, endpoint_id, incident_id, channel, payload(jsonb), delivered_at, status | RF-029, RNF-008 |

### 5.2 Estratégia de retenção e rollups

- **`checks`**: retenção **30 dias** (job de purga diária por partição — DROP PARTITION,
  sem DELETE massivo). Histórico antigo permanece nos rollups.
- **`check_rollups_minute`**: base dos gráficos (day/week/month), retenção **90 dias**;
  a partir disso, rollup diário opcional (fase 2).
- Gravação em **lote** (bulk insert a cada 100 registros ou 1s) reduz escritas e
  satura menos o WAL (RF-012, RNF-004).

### 5.3 Gráficos eficientes (RNF-014)

A API de stats nunca consulta `checks`; retorna **agregações pré-computadas** do
rollup — séries de latência (P50/P95/P99) e uptime % por bucket, com `LIMIT` de
pontos por request (ex.: máx. 1.440 pts/dia). O frontend renderiza com Recharts
sem paginação de linhas → Lighthouse ≥ 90 na status page (RNF-015).

---

## 6. Infraestrutura — Docker Compose + Nginx Reverse Proxy

### 6.1 Topologia de serviços

```
                        ┌──────────────────────────┐
       :80 / :443  ────▶│        NGINX             │  ← único ponto de entrada
                        │  · TLS termination       │     (RNF-016)
                        │  · serve estáticos       │
                        │  · proxy p/ backend      │
                        └───────┬────────┬─────────┘
                                │        │
                    /api /events│        │ / (SPA estática)
                                ▼        │
                     ┌────────────────────┴──────────────┐
                     │  backend (Go)   │  frontend (build estático)│
                     └────────────────┬───────────────────┘
                                      │ pgx pool
                                      ▼
                              ┌────────────────┐
                              │   postgres     │
                              │  volume (dados)│
                              └────────────────┘
```

### 6.2 `backend/Dockerfile` — multi-stage

```dockerfile
# ---------- STAGE 1: build ----------
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /app/monitor ./cmd/monitor

# ---------- STAGE 2: runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app/monitor /monitor
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/monitor"]
```

> **Justificativa**: `CGO_ENABLED=0` ⇒ binário estático (sem glibc); imagem
> **distroless/nonroot** ⇒ mínimo ataque de superfície (sem shell, sem pacotes);
> Go 1.22 traz `net/http` com roteamento aprimorado, reduzindo dependências.

### 6.3 `frontend/Dockerfile` — multi-stage

```dockerfile
# ---------- STAGE 1: build ----------
FROM node:20-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build          # → /app/dist (estáticos otimizados)

# ---------- STAGE 2: estáticos ----------
FROM nginx:1.25-alpine AS static
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx/frontend.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
```

> O frontend **não roda Node em produção**: build transpilado a **estáticos** servidos
> pelo Nginx edge (ou este `nginx:alpine` interno), com caching por hash e gzip.

### 6.4 `docker-compose.yml` (esboço)

```yaml
services:
  backend:
    build: ./backend
    env_file: .env
    environment:
      DB_DSN: postgres://monitor:${DB_PASSWORD}@postgres:5432/monitor?sslmode=disable
      WORKER_POOL_SIZE: ${WORKER_POOL_SIZE:-20}
      POOL_QUEUE_SIZE: ${POOL_QUEUE_SIZE:-1000}
    depends_on:
      postgres:
        condition: service_healthy
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "/monitor", "health"]
      interval: 30s
      retries: 3

  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_USER: monitor
      POSTGRES_PASSWORD: ${DB_PASSWORD}
      POSTGRES_DB: monitor
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U monitor"]
      interval: 5s
      retries: 5
    restart: unless-stopped

  nginx:
    image: nginx:1.25-alpine
    ports: ["80:80", "443:443"]
    volumes:
      - ./deploy/nginx/nginx.conf:/etc/nginx/nginx.conf:ro
      - ./deploy/nginx/certs:/etc/nginx/certs:ro
    depends_on: [backend]
    restart: unless-stopped

volumes:
  pgdata:
```

> `healthcheck` de serviço + `depends_on: condition: service_healthy` ⇒ subida
> ordenada; `restart: unless-stopped` ⇒ auto-recuperação na VPS (RNF-001).

### 6.5 Nginx edge — reverse proxy + segurança (pontos-chave)

```
# /etc/nginx/nginx.conf (resumo)
server {
    listen 443 ssl http2;
    server_name status.example.com;

    # TLS (acme.sh/certbot) + headenda de segurança (RNF-016/020)
    ssl_certificate     /etc/nginx/certs/fullchain.pem;
    ssl_certificate_key /etc/nginx/certs/privkey.pem;
    add_header Strict-Transport-Security "max-age=31536000" always;
    add_header X-Content-Type-Options nosniff always;
    add_header Content-Security-Policy "default-src 'self'" always;

    # Estáticos da status page (frontend) — cache por hash
    location / {
        root   /usr/share/nginx/html;
        try_files $uri /index.html;          # SPA fallback
        gzip_static on;
    }

    # API pública (status + stats) e SSE
    location /api/ {
        proxy_pass http://backend:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # SSE — NUNCA bufferizar o stream (RNF-009)
    location /api/events {
        proxy_pass http://backend:8080;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 60s;
        proxy_set_header X-Accel-Buffering no;
    }
}
server { listen 80; return 301 https://$host$request_uri; }  # redirect (RNF-016)
```

**Decisões de infra (pontos-chave):**

| Decisão | Detalhe | Requisito |
|---------|---------|-----------|
| Nginx como **único entrypoint** | TLS/termination, estáticos, proxy `/api` e `/api/events` | RNF-003, RNF-016 |
| **SSE sem buffer** no proxy | `proxy_buffering off` + `X-Accel-Buffering: no` | RNF-009 |
| **Multi-stage minimalista** | distroless + nginx-alpine; sem shell no runtime | RNF-003, segurança |
| **Healthchecks de container** | subida ordenada e auto-recuperação | RNF-001 |
| **Volumes persistentes** | `pgdata` p/ durabilidade do banco | durabilidade |
| Secrets via **env/.env** (não em imagem) | senhas, DSN e tokens de webhook fora do repositório | RNF-018 |

---

## 7. Matriz de Decisões ↔ Requisitos (Resumo)

| Decisão de arquitetura | Requisitos atendidos |
|------------------------|----------------------|
| Scheduler + Job Queue + Worker Pool (Go) | RF-007, RF-011, RF-012 · RNF-002, RNF-004 |
| State machine de confirmação (janela N/M) | RF-008, RF-013 · RNF-007 |
| Logs crus particionados + rollups minutais | RF-014..RF-018 · RNF-014 |
| SSE + Event Broker + snapshot de reconexão | RF-021, RF-022 · RNF-009, RNF-010, RNF-011 |
| Alertas só em transição + supressão | RF-025..RF-029 · RNF-005..RNF-008 |
| Docker Compose + multi-stage + Nginx | RNF-001, RNF-003, RNF-016, RNF-017, RNF-018 |

---

## 8. Fora de cobertura da arquitetura (fase 2)

- **Broker multi-instância** via Redis Pub/Sub (§4.6).
- **Checks multi-região** (execução em múltiplos PoPs).
- **Possível troca por TimescaleDB** para hypertables de `checks` (se o volume exigir).
- **Chart.js** em vez de Recharts, se o benchmark de renderização apontar vantagem (RNF-014).