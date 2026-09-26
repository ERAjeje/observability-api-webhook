# 📡 Central de Monitoramento de APIs & Webhooks

> 🌐 Leia esta página em [English](README.en.md).

Um sistema completo de **monitoramento de endpoints**: o operador cadastra os
**health-checks** das suas APIs e webhooks, o sistema verifica em intervalos
configuráveis, classifica cada checagem (`UP`/`DOWN`/`DEGRADED`), abre/fecha
**incidentes** com janela de confirmação e entrega tudo numa **status page
pública em tempo real** — com logs, gráficos e alertas.

Duas experiências no mesmo produto:

- 🌍 **Público** — a status page (`/`) mostra cada serviço por grupo, seu estado
  atual, gráficos de latência/uptime (24h/7d/30d) e a linha do tempo de
  incidentes, atualizados **em tempo real via SSE** (UP→DOWN em ≤ 5 s sem
  refresh). Sem segredos: nada de headers/tokens vaza para fora do painel.
- 🔐 **Operador** — cria a conta em segundos e gerencia endpoints e grupos,
  vê logs brutos e estatísticas, configura alertas (e-mail/SMTP ou webhook
  Slack/Discord) e a identidade visual da status page, tudo por um painel
  seguro com JWT, rate limit e **cotas por conta**.

> **Status:** 🟢 Fases 1–5 concluídas · Setembro/2026 · stack completa em Docker
> (API em ~14 MB · PostgreSQL · SPA React · nginx). Backend em Go com
> **worker pool** e **scheduler** próprios (sem lib de terceiros), estatísticas
> 100% a partir de **rollups pré-computados** e verificação de qualidade
> **100% local** (`make verify`) — sem nenhum serviço remoto.

---

## O que o projeto faz

### Para o operador — em poucos passos

1. **Cria a conta** (signup/login com bcrypt + JWT de 24 h e rate limit por
   IP+e-mail).
2. **Cadastra um endpoint** — URL, método, headers de autenticação, intervalo
   de checagem (mínimo por conta), timeout, limite de latência, status e/ou
   trecho de body esperados. Um **teste de conectividade** valida antes de
   salvar e o **guarda anti-SSRF** bloqueia alvos internos por padrão.
3. **O scheduler dispara as checagens** com jitter anti *thundering herd* e o
   **worker pool** executa em paralelo com backpressure não-bloqueante. A
   **state machine N/M** confirma: **DOWN só após 3 falhas** e **UP após 2
   sucessos** (RF-013) — abrindo e fechando **incidentes** com duração.
4. **Acompanha tudo**: status page pública em tempo real (SSE), logs brutos
   filtráveis/paginados, gráficos 24h/7d/30d e **alertas** (SMTP ou webhook)
   com janela de supressão, retry com backoff e auditoria.

### O que a status page mostra (público)

- Cada grupo de serviços com seu estado atual (`UP`/`DOWN`/`DEGRADED`);
- Gráficos de latência (P50/P95) e uptime por janela (24h, 7d, 30d) — servidos
  de **rollups pré-computados**, nunca da tabela de checagens;
- Linha do tempo de **incidentes** (abertura, fechamento, duração);
- Identidade visual configurável pelo operador (`branding` dinâmico);
- **Tempo real**: UP→DOWN refletido em ≤ 5 s sem refresh (SSE).

> Os **headers de autenticação** dos endpoints são **cifrados em repouso**
> (AES-256-GCM, `HEADERS_ENC_KEY`) e **nunca** aparecem fora do painel admin
> autenticado — nem no HTML, nem nas rotas públicas (RNF-018).

### Alertas que o sistema envia

- Abertura e fechamento de incidentes, via **e-mail (SMTP)** e/ou **webhook**
  (Slack/Discord), com **supressão** (300 s entre avisos do mesmo endpoint),
  retry com backoff (até 3 tentativas) e auditoria em `notifications`.

---

## Stack usada na construção

| Camada | Tecnologia | Por quê |
|---|---|---|
| Backend | **Go** (Chi + pgx) | concorrência de goroutines no worker pool, binário distroless de ~14 MB, sem deps pesadas |
| Scheduler + Pool | **implementação própria** | tick global com jitter ±20% (anti *thundering herd*), guarda `in_flight` (RF-011) e backpressure não-bloqueante — sem lib externa |
| Banco | **PostgreSQL 15** | `checks` **particionada por mês** + `rollups` por minuto = retenção e dashboards escaláveis |
| Tempo real | **SSE** (`/api/v1/events`) | snapshot na conexão + heartbeat (15 s): UP→DOWN em ≤ 5 s com servidor simples |
| Frontend | **Vite + React + TS + Tailwind + Recharts** | SPA leve com **code-split** (Recharts fora do bundle público — RNF-015) |
| Autenticação | **bcrypt + JWT (24 h)** | senhas com hash custoso; rotas admin protegidas + rate limit (RNF-017) |
| Alertas | **SMTP + webhook** (fail-closed) | supressão + retry/backoff + auditoria; settings dinâmicos salvos (fallback por env) |
| Segurança em repouso | **AES-256-GCM** (`HEADERS_ENC_KEY`) | headers/tokens dos endpoints cifrados no Postgres; fail-closed sem a chave (S-05) |
| Cotas por conta | **`internal/quota`** | máx. endpoints, intervalo mínimo e projeção de checks/mês → **429** (S-08) |
| Observabilidade | **`/metrics` (Prometheus text)** | registrador próprio, sem lib externa (RF-012) |
| Deploy | **Docker Compose** + nginx | 1 comando sobe tudo; Dockerfile multi-stage → distroless nonroot; TLS (acme.sh) |
| Verificação | **`make verify`** (100% local) | vet + testes `-race` + build/audit do frontend; **sem GitHub Actions / CI remoto** |

### Decisões de engenharia que valem menção

- **Estatísticas só de rollups pré-computados (RNF-014).** Nenhum dashboard
  varre a tabela de checagens (particionada por mês): o engine agrega por
  minuto (`count`, `ok_count`, soma de latência, P50/P95) e as rotas de gráfico
  leem só essas séries. Consulta constante em qualquer volume.
- **State machine de confirmação N/M (RF-013).** Transições não são reativas a
  uma única falha: exigem 3 falhas consecutivas para `DOWN` e 2 sucessos para
  `UP`, com janela configurável — incidentes só abrem de verdade quando há
  consenso, evitando alarmes falsos.
- **Backpressure que não perde trabalho.** A fila do pool é bufferizada e o
  `Submit` é não-bloqueante; quando estoura, o scheduler `reprocessa` nos
  próximos ticks. Validado num teste de carga que **pegou um double-run real**
  (snapshot obsoleto do scheduler) — o fix revalida o vencimento de cada job
  antes de enfileirar.
- **SSE com snapshot + heartbeat.** Ao conectar, o cliente recebe o estado
  atual de todos os endpoints; a partir daí, eventos incrementais. Reconexão é
  trivial (novo snapshot) e um heartbeat de 15 s mantém a conexão viva atrás
  de proxies (nginx sem buffer no caminho).
- **Segredos cifrados em repouso (S-05).** Headers de autenticação vão para o
  Postgres como um envelope `$seal_v1` AES-256-GCM; são descriptografados só
  no engine (para checar) e no painel admin. Sem a chave, o sistema segue
  **sem headers** — nunca vaza o segredo.
- **Cotas por conta (S-08).** Um operador não pode auto-atacar a plataforma
  com 1.000 endpoints a cada 1 s: há teto de endpoints, intervalo mínimo e
  projeção de checagens/mês, com resposta `429` clara no create/update.
- **Certificados de qualidade de verificação local.** A trilha de validação é
  `make verify` (vet + testes `-race` + build/audit do front), `make load-test`
  (memória) e `make load-test-pg` (PostgreSQL real), mais E2E Playwright —
  com local simulation do deploy de VPS (`SKIP_TLS=1`).
- **SSRF guard por padrão (S-01).** Alvos loopback/RFC1918/metadata de cloud
  são bloqueados no cadastro, salvo `ALLOW_PRIVATE_TARGETS=true` (lab).

---

## Estrutura do monorepo

```
project-3/
├── backend/
│   ├── cmd/
│   │   ├── monitor/              # entrypoint HTTP (graceful shutdown + health)
│   │   └── migrate/              # runner de migrations (up/down)
│   ├── internal/
│   │   ├── api/                  # handlers REST admin/público + SSE + /metrics
│   │   ├── auth/                 # bcrypt + JWT + rate limit (RNF-017)
│   │   ├── broker/               # pub/sub in-memory alimentando o SSE
│   │   ├── checker/              # execução HTTP + timeout + anti-SSRF
│   │   ├── config/               # env → config validada
│   │   ├── domain/               # entidades + state machine N/M (RF-013)
│   │   ├── engine/               # orquestrador: scheduler + pool + persistência
│   │   ├── metrics/              # recursório Prometheus text (RF-012)
│   │   ├── migrate/              # migrations embarcadas (go:embed)
│   │   ├── notifier/             # alertas SMTP + webhook, supressão/retry
│   │   ├── quota/                # cotas por conta → 429 (S-08)
│   │   ├── scheduler/            # produção de jobs (tick + jitter + guard)
│   │   ├── seal/                 # cifragem AES-256-GCM de headers (S-05)
│   │   ├── settings/             # config dinâmica (branding/alertas)
│   │   ├── storage/              # Store: MemStore · PgStore (lote + rollups)
│   │   └── worker/               # pool de goroutines (backpressure)
│   ├── Dockerfile                # multi-stage → distroless nonroot (~14 MB)
├── frontend/                     # SPA Vite + React + TS + Tailwind + Recharts
│   └── tests/e2e/                # Playwright — 4 cenários contra a stack Docker
├── deploy/
│   ├── nginx/nginx.conf          # reverse proxy TLS + SSE sem buffer + /metrics
│   └── provision.sh              # bootstrapping da VPS (Docker + TLS acme.sh)
├── docs/                         # requirements · architecture · tasks · security-review
├── .env.example                  # modelo de configuração (sem credenciais)
├── docker-compose.yml            # postgres + backend + nginx
└── Makefile                      # orquestrador (build, test, verify, load-test*…)
```

### API REST (`/api/v1`)

| Grupo | Rotas principais |
|---|---|
| Público | `GET /status` · `GET /incidents` · `GET /config` · `GET /status/{id}/stats/summary` · `GET /status/{id}/stats/series` · `GET /events` (SSE) |
| Painel (`/admin`, JWT) | CRUD `/endpoints` (+ `POST /test`) · CRUD `/groups` · `GET /checks` · `GET /stats/*` · `GET|PUT /settings` · `GET /notifications` |
| Auth | `POST /signup` · `POST /login` |
| Sondas | `GET /healthz` · `GET /readyz` · `GET /metrics` (Prometheus) |

```bash
# Exemplo: criar conta e monitorar um endpoint
TOKEN=$(curl -sk https://localhost/api/v1/auth/login \
  -d '{"email":"adm@ex.com","password":"senha-segura-123"}' | jq -r .token)
curl -sk -H "Authorization: Bearer $TOKEN" \
  https://localhost/api/v1/admin/endpoints/ \
  -d '{"name":"api-gw","url":"https://example.com/health","method":"GET","interval_seconds":60}'
```

Detalhes de contratos, modelo de dados e decisões: [docs/architecture.md](docs/architecture.md).

---

## Como rodar em ambiente de desenvolvimento

### Pré-requisitos

- **Docker com Compose v2** (caminho recomendado — não precisa de Go, Node nem
  PostgreSQL instalados);
- Git (para clonar). Para rodar o backend nativo, **Go ≥ 1.22**; para o
  frontend, **Node ≥ 20** / npm.

### Passo a passo (recomendado)

```bash
# 1) clone e entre no projeto
git clone <url-do-repositorio> && cd project-3

# 2) gera o .env (segredos nunca versionados)
cp .env.example .env        # edite JWT_SECRET (ou deixe para o provision.sh)

# 3) compila a SPA e sobe tudo: postgres + backend + nginx (80/443)
make frontend-build
make docker-up              # ou: docker compose up --build -d
```

Aguarde o backend ficar `healthy` (10–40 s no primeiro build da imagem
distroless) e confirme com as sondas:

```bash
curl -k https://localhost/healthz       # → ok
curl -k https://localhost/readyz        # → {"status":"ok"}
```

> As **migrações rodam automaticamente** (`AUTO_MIGRATE`) no entrypoint da API.
> O TLS local usa cert self-signed (gere em `deploy/nginx/certs/` ou use o
> `provision.sh`).

### O que fica acessível

| O quê | URL | Observação |
|---|---|---|
| 🌍 Status page pública | `https://localhost/` | estado em tempo real por grupo + gráficos |
| 🔐 Painel admin | `https://localhost/admin/login` | criar conta / login |
| 📊 Métricas | `https://localhost/metrics` | formato Prometheus (proteja por firewall — S-11) |
| 🗄️ PostgreSQL | interno ao Docker | não exposto na rede do host; use `docker compose exec postgres psql` |

### Conta de demonstração

O projeto **não semeia dados de demonstração** — criar a conta e os endpoints
faz parte do roteiro abaixo. A suíte E2E usa `e2e@monitor.test` /
`e2e-senha-segura-123` e cria tudo sozinha.

### Alternativas de execução

| Opção | Comando | Quando usar |
|---|---|---|
| **Stack completa em Docker** | `make docker-up` | ✨ recomendada para avaliar tudo |
| **Backend nativo (MemStore)** | `make run` | mexer no backend em `:8080` sem banco |
| **Sobrecarga (memória)** | `make load-test` | valida burst de 800 endpoints exatamente-once |
| **Sobrecarga (PostgreSQL real)** | `make load-test-pg` | idem contra um container efêmero do Postgres |
| **Verificação local completa** | `make verify` | vet + testes `-race` + build/audit do frontend |

### Configuração (`.env`)

Nada de senha no repositório; o `provision.sh` gera `DB_PASSWORD`, `JWT_SECRET`
e `HEADERS_ENC_KEY` aleatórios. Variáveis-chave para testar as regras de
negócio:

| Variável | Default | Efeito |
|---|---|---|
| `JWT_SECRET` | — | assinatura dos tokens (obrigatória em produção) |
| `DB_PASSWORD` | `monitor` | senha do Postgres |
| `HEADERS_ENC_KEY` | vazio (texto plano em dev) | cifra headers em repouso (S-05) — 64 hex |
| `MAX_ENDPOINTS_PER_ACCOUNT` | `50` | cota de endpoints por conta (S-08) |
| `MIN_CHECK_INTERVAL_SECONDS` | `10` | intervalo mínimo por conta (S-08) |
| `CHECKS_PER_MONTH_QUOTA` | `0` (derivada) | teto de checagens/mês projetado (S-08) |
| `ALLOW_PRIVATE_TARGETS` | `false` | libera alvos internos no cadastro (SSRF guard) |
| `FAIL_THRESHOLD` / `SUCCESS_THRESHOLD` | `3` / `2` | janela de confirmação da state machine (RF-013) |
| `SMTP_*` · `NOTIFY_WEBHOOK_URL` · `NOTIFY_ENABLED` | desligado | canais de alerta (T3.7) |

Modelo completo com comentários: [.env.example](.env.example).

---

## Como usar o produto (roteiro de demonstração)

> **10 segundos:** acesse `https://localhost/` e veja a status page pública — e
> em `https://localhost/admin/login` crie sua conta e cadastre o primeiro
> endpoint.

**Roteiro completo para mostrar a plataforma frente a frente:**

1. **Crie uma conta** em `https://localhost/admin/login` (signup com e-mail e
   senha). O painel abre com a lista de endpoints.
2. **Cadastre 2 endpoints** (ex.: `https://api.github.com/zen` e
   `https://example.com/health`) com intervalos de 1–5 min. Use o **teste de
   conectividade** e repare que o painel lista o estado, os próximos checks e
   os gráficos começando a se formar.
3. **Tempo real (o diferencial)** — no lab com `ALLOW_PRIVATE_TARGETS=true`,
   cadastre um alvo local e **derrube-o**: em até ~5 s a status page pública
   (em outra aba, via SSE) marca `DOWN`, um **incidente** abre na timeline e
   volta a fechar quando você religar o alvo (state machine: 3 falhas / 2
   sucessos).
4. **Alertas** — em *Settings* configure um webhook (Slack/Discord) ou SMTP e
   veja os disparos listados na auditoria `notifications`. Dica: use o próprio
   endpoint `https://discord.com/api/webhooks/...` como alvo para ver o alerta
   chegando.
5. **Cotas (S-08)** — tente cadastrar o endpoint nº `MAX+1` ou um com
   `interval_seconds` menor que o mínimo da conta: a API responde **429** com
   mensagem amigável que aparece no próprio formulário do painel.
6. **Métricas** — `https://localhost/metrics` mostra pool, fila, checagens por
   resultado, latência P50/P95 e eventos SSE (formato Prometheus).
7. **Sobrecarga** — `make load-test` e `make load-test-pg` provam o pipeline
   sob burst (800 endpoints, exatamente-one, 0 drops).

---

## Qualidade e testes

- **Backend**: `make verify` → `go vet` + `go test -race ./...`. Todos os
  pacotes verdes: `api, auth, broker, checker, domain, engine, metrics,
  notifier, quota, scheduler, seal, storage, worker`.
- **Sobrecarga**: `make load-test` (memória) e `make load-test-pg`
  (PostgreSQL real) — **800 endpoints em burst**: 0 drops, 0 double-runs,
  pico in-flight ≤ pool; backpressure sem deadlock. O teste `load` travou um
  double-run real do scheduler (fix documentado no código).
- **E2E**: Playwright — 4 cenários contra a stack Docker (UC-01/UC-05:
  incidente + status page via SSE, status page/branding, login admin + CRUD,
  settings de alertas). `cd frontend && npx playwright test`.
- **Segurança**: varreduras documentadas em
  [docs/security-review.md](docs/security-review.md) (S-01..S-24) com
  govulncheck + gosec na verificação local — sem CI remoto.
- **Deploy**: `make verify` local + smoke `SKIP_TLS=1` do
  [deploy/provision.sh](deploy/provision.sh) validando `.env` → build → stack →
  readyz em sandbox.

---

## Documentação

| Documento | Conteúdo |
|---|---|
| [docs/requirements.md](docs/requirements.md) | Requisitos funcionais (RF-001..029) e não funcionais (RNF-001..020), casos de uso |
| [docs/architecture.md](docs/architecture.md) | Arquitetura, modelo de dados (partições, rollups), SSE, ADRs |
| [docs/tasks.md](docs/tasks.md) | Plano de execução em 5 fases (checkboxes + critérios de aceite) |
| [docs/security-review.md](docs/security-review.md) | Varreduras de segurança (S-01..S-24) e status dos achados |
| [deploy/README.md](deploy/README.md) | Deploy na VPS: provision, TLS acme.sh, operação, firewall |
| [project.md](project.md) | Mapa do projeto e estado atual |

---

## Fluxo de trabalho (Git)

`develop` é a trilha de integração (commits **Conventional Commits**); o
remoto é **opcional e usado apenas como backup** — deliberadamente **sem
GitHub Actions / CI** (toda validação é local via `make verify`). Novas
features seguem commits pequenos com mensagens claras (`feat:`/`fix:`/`docs:`).

## Releases

| Tag | Conteúdo | Data |
|---|---|---|
| `v1.0.0` (planejada) | Fases 1–5 — engine completo, API + SSE + alertas, frontend status page + painel, hardening S-05/S-08 | Set/2026 |

> Identidade visual da status page **configurável pelo operador** (título e
> branding via `PUT /api/v1/admin/settings`) — RF-023; a SPA usa tema claro
> padrão com paleta fácil de ajustar em `frontend/`.