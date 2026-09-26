# Plano de Execução — Central de Monitoramento de APIs & Webhooks

| Campo        | Valor                                            |
|--------------|--------------------------------------------------|
| **Fonte**    | `docs/requirements.md` · `docs/architecture.md`  |
| **Versão**   | 1.0 (draft)                                      |
| **Data**     | Setembro/2026                                    |
| **Status**   | Proposto — pendente de execução                  |

---

## Legenda e Convenções

- **`[ ]`** = pendente · **`[x]`** = concluída
- Cada tarefa referencia requisitos (**RF/RNF**) e seções da arquitetura (**§**)
- **DoD (Definition of Done)** comum a toda tarefa de código:
  - Backend: compila (`go build ./...`), testes passam (`go test ./... -race`)
  - Frontend: build de produção (`npm run build`), testes relevantes verdes
  - Commit seguindo Conventional Commits, integrado via **GitFlow** em `develop`
- Estrutura do monorepo (arquitetura §1.2/§6): `backend/` (Go), `frontend/` (React + Vite + TS), `deploy/` (Docker/Nginx), `scripts/`, `docs/`; `Makefile` na raiz agrega comandos.
- Stack (arquitetura §2): **Go ≥ 1.22 + Chi + pgx** · **React ≥ 18 + Tailwind CSS + Recharts** · **PostgreSQL ≥ 15** · **Docker Compose + Nginx** · tempo real via **SSE**.

> **Ordem das fases por dependência:** 1 (Setup) → 2 (Core Engine) → 3 (API + SSE) → 4 (Frontend).
> A Fase 4 consome a API da Fase 3; a Fase 2 é pré-requisito da 3 (o SSE publica eventos gerados pelo worker).

---

## Fase 1 — Setup & Boilerplate (Go · Banco · Docker Compose)

> Arquitetura: **§2 Stack** · **§5 Banco** · **§6 Infraestrutura** · RNF-001, RNF-003, RNF-019

### 1.1 Repositório e estrutura

- [x] **T1.1** Materializar a estrutura do monorepo: `backend/` (Go), `frontend/` (React), `deploy/` (Docker/Nginx), `scripts/`, `Makefile` raiz, `.env.example` por ambiente (dev/prod) e `.gitignore`.
  - **Critério de aceitação:** árvore reflete a arquitetura §1.2; `make help` lista os alvos; `.env.example` documenta `DB_DSN`, `WORKER_POOL_SIZE`, `SSE_*`.
- [x] **T1.2** Inicializar o módulo Go: `backend/go.mod` (ex.: `backend/api`), estrutura `cmd/monitor` + `internal/{config,scheduler,worker,checker,broker,storage,api,notifier}`, `migrations/` (embed) e `main.go` mínimo com `net/http` no ar.
  - **Critério de aceitação:** `go build ./...` compila; servidor responde na porta 8080; pacotes `internal/*` existem conforme §1.2.

### 1.2 Configuração

- [x] **T1.3** Implementar carregamento de configuração via env (`internal/config`) com defaults (`WORKER_POOL_SIZE=20`, `POOL_QUEUE_SIZE=1000`, `SSE_HEARTBEAT=15s`) e validação de obrigatórios (`DB_DSN`, `JWT_SECRET`).
  - **Critério de aceitação:** processo falha com erro claro se `DB_DSN` ausente; defaults aplicados para env parciais; nada de segredo impresso em log (RNF-020).

### 1.3 Banco de dados — conexão e migrações

- [x] **T1.4** Configurar pool PostgreSQL com `pgxpool` (limites de conexão, timeouts, ping) e endpoints `/healthz` (liveness) e `/readyz` (checa o banco).
  - **Critério de aceitação:** `db.Ping` ok; `/readyz` retorna **503** quando o banco está fora; `/healthz` nunca falha por dependência (RNF-004).
- [x] **T1.5** Criar migração inicial `000001` com o DDL da arquitetura §5.1: `users`, `check_groups`, `endpoints` (partial index `WHERE active`), `checks` **particionada por mês**, `check_rollups_minute`, `incidents`, `notifications` + índices `(endpoint_id, checked_at DESC)`.
  - **Critério de aceitação:** `make migrate-up` aplica em PostgreSQL limpo e `make migrate-down` reverte; particionamento por mês verificado (`\d+ checks`); índices presentes.

### 1.4 Docker e orquestração local

- [x] **T1.6** Criar `backend/Dockerfile` multi-stage (build `CGO_ENABLED=0` → imagen distroless `nonroot`) com entrypoint que aplica migrações ao subir (arquitetura §6.2).
  - **Critério de aceitação:** imagem < 30 MB; contêiner aplica migrações e responde `/healthz`.
- [x] **T1.7** Criar `docker-compose.yml` na raiz: serviços `backend`, `postgres`, `nginx` com healthchecks (`pg_isready`, `/healthz`), `depends_on: condition: service_healthy`, volume `pgdata` e `restart: unless-stopped` (arquitetura §6.4).
  - **Critério de aceitação:** `docker compose up --build` sobe a stack limpa; `backend` só fica healthy após `postgres`; volume persiste dados após `down`.
- [x] **T1.8** Criar `deploy/nginx/nginx.conf`: reverse proxy único, redirect HTTP→HTTPS, proxy `/api` e **`/api/events` com SSE sem buffer** (`proxy_buffering off`, `X-Accel-Buffering: no`), estáticos do frontend com SPA fallback, headers de segurança (HSTS, CSP).
  - **Critério de aceitação:** `GET /api/healthz` via Nginx responde; endpoint SSE configurado sem buffer (arquitetura §6.5); redirect `:80 → :443` ativo.
- [x] **T1.9** Criar `Makefile` raiz com alvos: `build`, `test`, `lint`, `migrate-up`, `migrate-down`, `docker-up`, `frontend-dev`.
  - **Critério de aceitação:** `make build && make test && make lint` verdes; comandos documentados no README.

> **✅ Barreira da Fase 1:** stack Docker sobe saudável (`/healthz` e `/readyz` verdes via Nginx), migração aplicada com particionamento, `make test` passa, imagem do backend builda em multi-stage.

---

## Fase 2 — Core Engine de Monitoramento (Scheduler + Workers em Go)

> Arquitetura: **§3 Checagens em Massa** · RF-007..RF-017 · RNF-002, RNF-004

### 2.1 Domínio e state machine

- [x] **T2.1** Implementar domínio puro em `internal/domain`: `Endpoint`, `CheckResult`, `StatusClass` (`UP/DOWN/DEGRADED`), `Incident` e a **state machine de confirmação** — `DOWN` após N falhas consecutivas, `UP` após M sucessos (defaults 3/2), `DEGRADED` por latência > limite.
  - **Critério de aceitação:** testes de tabela provam UC-01/02/03 — falha isolada **não** declara DOWN; 3 falhas consecutivas declaram DOWN com horário da 1ª falha; 2 sucessos recuperam UP (RF-013).
- [x] **T2.2** Implementar repositório de endpoints (`internal/storage`): CRUD (RF-001..003) e query de **endpoints ativos com `next_check_at <= now`**, sem sobreposição (guarda `in_flight`).
  - **Critério de aceitação:** CRUD reflete em `endpoints`; query retorna somente endpoints ativos e atrasados; nenhum endpoint com execução em voo é re-enfileirado (RF-011).

### 2.2 Scheduler

- [x] **T2.3** Implementar `internal/scheduler`: goroutine com ticker (default 10s) que consulta endpoints atrasados, aplica **jitter ±20%** no `next_check_at` (anti thundering herd) e enfileira jobs no canal bufferizado (`POOL_QUEUE_SIZE`).
  - **Critério de aceitação:** em teste com endpoints de intervalos distintos (1/5/15 min), cada endpoint é agendado conforme o intervalo; nenhum job sobreposto; buffer cheio **não bloqueia** o tick (backpressure registra déficit) (RF-007, RF-011, RNF-002).
- [x] **T2.4** Implementar **graceful shutdown**: `SIGTERM` → para o scheduler → fecha canal → `WaitGroup` aguarda jobs em voo (com deadline) → drena → sai códig 0.
  - **Critério de aceitação:** `kill -TERM` durante checagens encerra sem erro e sem processos órfãos; ao re-subir, o agendador recalcula atrasados de forma **idempotente** (sem checagem duplicada) (RNF-002).

### 2.3 Checker HTTP e logs de latência

- [x] **T2.5** Implementar `internal/checker`: execução HTTP com **timeout por endpoint** (RF-009), método/headers/body configuráveis, validação de **status HTTP esperado e conteúdo do corpo** (RF-010) e medição de latência (ms).
  - **Critério de aceitação:** testes com `httptest.Server` — 200 → success; 5xx → falha; timeout simulado → falha com `timeout`; body esperado/inesperado classificam corretamente (RF-008).
- [x] **T2.6** Implementar pool de workers (`internal/worker`): K goroutines consumindo o canal, cada uma executa `checker`, classifica e **grava em lote** (bulk insert de `checks` + upsert de `check_rollups_minute` a cada 100 registros ou 1s).
  - **Critério de aceitação:** 100 checagens gravadas em lote único; rollup por minuto atualizado (média latência, P95); **falha temporária de banco não derruba o worker** — batch re-tentado com backoff (RF-012, RNF-004).
- [x] **T2.7** Aplicar a state machine sobre resultados e **materializar incidentes**: abrir `incidents` na 1ª falha confirmada (RF-013) e fechar com `ended_at` + `duration_ms` na recuperação (RF-017).
  - **Critério de aceitação:** simulação de outage → incidente aberto com início correto; simulação de recuperação → incidente fechado com duração precisa (UC-01, UC-03).

> **✅ Barreira da Fase 2:** worker monitora endpoints de teste (httptest) em runtime real — checagens periódicas, logs de latência persistidos, rollups e incidentes corretos comprovados por teste integrado.

---

## Fase 3 — API REST e Mensageria (SSE)

> Arquitetura: **§4 Tempo Real (SSE)** · RF-001..006, RF-018..022, RF-025..029 · RNF-005..013

### 3.1 API administrativa (protegida)

- [x] **T3.1** Implementar autenticação: `signup`/`login` com bcrypt, **JWT** com expiração, middleware de proteção das rotas admin e **rate limit** em login/signup.
  - **Critério de aceitação:** rota admin sem token → **401** e sem vazamento de dados (RF-006); senha força bruta → **429** (RNF-017, RNF-019).
- [x] **T3.2** Implementar CRUD de `endpoints` e `check_groups` (REST `/api/v1/admin/...`): validação de URL (scheme/host) e **teste manual de conectividade** antes de salvar (RF-001..005).
  - **Critério de aceitação:** endpoint criado persiste e é observável no scheduler (cache invalidado na edição); ativar/desativar reflete imediatamente no agendamento (RF-016); desativado não gera checagem.
- [x] **T3.3** Implementar API de **logs e stats** (RF-018): listagem paginada de `checks` (filtro por endpoint/período/status) e séries de latência (`P50/P95`) + uptime % (dia/semana/mês) **somente de rollups** pré-computados.
  - **Critério de aceitação:** resposta usa rollups (nunca logs brutos — RNF-014); `LIMIT` de pontos por request; P95 de resposta < 300 ms com dados de teste (RNF-013).

### 3.2 API pública (status page)

- [x] **T3.4** Implementar endpoints públicos (sem auth): status atual de todos os endpoints, incidentes em aberto e timeline histórica (RF-019, RF-022).
  - **Critério de aceitação:** respostas sem qualquer config/segredo (RNF-018); formato enxuto consumido pela status page; rota pública separada da admin (RNF-017).

### 3.3 Mensageria — Event Broker + SSE

- [x] **T3.5** Implementar `internal/broker` (pub/sub in-memory com mutex, fan-out por cópia) e handler **SSE** `/api/v1/events`: `text/event-stream`, **snapshot completo no connect**, heartbeat a cada 15s (RF-021, RNF-009, RNF-010).
  - **Critério de aceitação:** 2 clientes conectados recebem o mesmo evento; snapshot enviado na abertura da conexão; heartbeat mantém o stream vivo no Nginx (RNF-009).
- [x] **T3.6** Publicar eventos de transição: `status_changed`, `incident_opened`, `incident_closed`, `rollup_updated` — conectados ao worker (Fase 2) sem acoplamento.
  - **Critério de aceitação:** cada transição confirmada gera exatamente 1 evento; evento **sem dados sensíveis**; cliente reconectado recebe snapshot e não diverge (RNF-010, UC-05).
- [x] **T3.7** Implementar `internal/notifier`: alertas em transição por **e-mail (SMTP)** e **webhook** (Slack/Discord), **janela de supressão** (RF-027), retry com backoff (RNF-006), multi-canal com fallback (RNF-008) e auditoria em `notifications`.
  - **Critério de aceitação:** transição UP→DOWN entrega alerta ≤ **60 s** (RNF-005); repetição dentro da janela suprimida (UC-04); falha de um canal não impede o outro; payload registrado para auditoria (RF-029).

> **✅ Barreira da Fase 3:** fluxo completo comprovado via `curl` — checagem → log → incidente → alerta e evento SSE; autenticação protege rotas admin; API de stats responde com rollups.

---

## Fase 4 — UI/UX Frontend (Dashboard Admin + Status Page)

> Arquitetura: **§2 Stack** · RF-019..RF-024, RF-001..006, RF-018 · RNF-009..RNF-015

### 4.1 Base frontend

- [x] **T4.1** Scaffold `frontend/` com **React + Vite + TypeScript + Tailwind CSS + React Router** + React Query; layout base responsivo e tema customizável (título, descrição, cores) (RF-023).
  - **Critério de aceitação:** `npm run build` passa; tema carregado de config; layout mobile-first (RNF-015).
- [x] **T4.2** Implementar cliente de API (`lib/api.ts`) e **cliente SSE** com `EventSource` + consumo do snapshot no connect e reconexão automática (RNF-010).
  - **Critério de aceitação:** reconexão após queda não diverge do estado real (snapshot aplicado); interface única de eventos no frontend.

### 4.2 Dashboard administrativo

- [x] **T4.3** Implementar tela de **login** e **CRUD de endpoints/grupos** (formulário com URL, método, headers, intervalo, timeout, expect status/body), consumindo a API admin (RF-001..006).
  - **Critério de aceitação:** sem sessão → redirect para login; CRUD funcional contra a API real; toggle ativar/desativar reflete no agendamento.
- [x] **T4.4** Implementar tela de **logs/stats**: tabela de checagens paginada com filtros (endpoint, período, status) e gráficos de latência (RF-018).
  - **Critério de aceitação:** filtros refletem na request; paginação funcional; gráficos renderizam a partir dos rollups da API.
- [x] **T4.5** Implementar tela de **alertas**: canais (e-mail/webhook), janela de supressão e limites de latência (RF-025..027, RF-004).
  - **Critério de aceitação:** configuração salva é usada pelo notifier (teste de integração); validação de URL de webhook.

### 4.3 Status page pública

- [x] **T4.6** Implementar **status page pública**: cards por grupo/endpoint com status atual (`UP`/`DOWN`/`DEGRADED`), indicadores acessíveis e legíveis em poucos segundos (RF-019, RF-024).
  - **Critério de aceitação:** sem login; status perceptível em < 5 s; contraste WCAG AA (RNF-005 visual).
- [x] **T4.7** Implementar **gráficos com Recharts**: série de latência P50/P95/P99 e uptime % (dia/semana/mês) a partir dos rollups, com `LIMIT` de pontos (RF-020, RNF-014).
  - **Critério de aceitação:** renderiza até 1.440 pontos sem queda de FPS; Lighthouse **Performance ≥ 90** na status page (RNF-015).
- [x] **T4.8** Integrar **tempo real via SSE**: transições de status, abertura/fechamento de incidente e atualização de gráficos sem recarga (RF-021, RF-022).
  - **Critério de aceitação:** transição UP→DOWN aparece na página em ≤ **5 s** sem refresh (RNF-009, UC-05); timeline de incidentes atualiza ao vivo.

### 4.4 Qualidade e E2E

- [x] **T4.9** Implementar teste **E2E** (Playwright) do fluxo completo: checagem falha → incidente → alerta físico (webhook de teste) → status page reflete DOWN em todos os clientes.
  - **Critério de aceitação:** cenários dos UC-01 e UC-05 verdes ponta a ponta contra a stack Docker completa.

> **✅ Barreira da Fase 4:** status page pública funcional em produção local (Docker) — gráficos reais, tempo real SSE, dashboard admin operando CRUD e alertas; Lighthouse ≥ 90; E2E verde.

---

## Checklist Final de Integração

- [x] **CI** verde para backend/frontend (lint, teste, build) (RNF-021): `.github/workflows/ci.yml`
  (backend vet+tests-race+vulncheck · frontend build+audit · E2E na stack) e `make ci` local.
  *Execução real depende do repo remoto (hoje local).*
- [x] **Deploy** na VPS: `deploy/provision.sh` + `deploy/README.md` — Docker, `.env` com
  segredos aleatórios, frontend build, stack up e **TLS real via acme.sh** (RNF-003, RNF-016).
  *Validação real depende da VPS; fluxo e script prontos.*
- [x] **Sobrecarga** validada (`make load-test`): 800 endpoints em burst **processados exatamente 1×**
  com 0 drops e pico in-flight ≤ pool; backpressure (fila=8) → `queue_starved>0` e recuperação total
  sem deadlock. **Corrigido um double-run real durante a validação** (snapshot obsoleto do scheduler —
  revalida due no tempo atual, RF-011). (RNF-002, RNF-011)
- [x] **Observabilidade** (RF-012): `GET /metrics` (Prometheus text, sem lib externa) — pool
  (`queue_depth`, `queue_starved_total`, jobs, in-flight), engine (checks por resultado, histograma de
  latência P50/P95, transições, eventos, incidentes), endpoints por estado e runtime Go. Logs já
  estruturados (slog). nginx expõe em `location = /metrics` (restrição por rede — S-11).
- [x] **Hardening S-05** (headers em repouso): `internal/seal` AES-256-GCM (`HEADERS_ENC_KEY`, 64 hex),
  envelope `$seal_v1` no jsonb; decifra no engine (checagem) e no admin autenticado; público nunca vaza
  (RNF-018); update re-cifra; **fail-closed** sem chave com blob presente. Validado ao vivo no Postgres.