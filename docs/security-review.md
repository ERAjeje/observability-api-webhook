# Varredura de Segurança — Fase 1/2 (Core Engine)

| Campo        | Valor                                                  |
|--------------|--------------------------------------------------------|
| **Escopo**   | Backend Go (Fases 1 e 2), infra Docker/Nginx           |
| **Data**     | Setembro/2026                                          |
| **Papel**    | Revisão devsec (roteiro + estática + manual)           |
| **Status**   | ✅ Verificações automatizadas limpas · ⚠️ Pendências documentadas |

---

## 1. Resumo executivo

O código revisado é **limpo nos pontos críticos** (queries parametrizadas,
TLS por padrão, binário distroless/nonroot, migrações idempotentes, panics
recuperados). Dois achados automatizados foram corrigidos nesta varredura
(RNG fraco no jitter; vulnerabilidade real em dependência transitiva) e a
revisão manual encontrou um **risco crítico de SSRF** no checker, já mitigado.

**Resultado das ferramentas (baseline depois das correções):**

| Ferramenta   | Antes                          | Depois                      |
|--------------|--------------------------------|-----------------------------|
| `govulncheck` | 1 vuln (GO-2026-5970, x/text)  | **0 vulnerabilidades**      |
| `gosec`      | 2 issues (G404-HIGH, G104-LOW) | **0 issues**                |
| `go test -race` | ok                           | **verde (todos os pacotes)** |

---

## 2. Achados corrigidos nesta varredura

| ID | Severidade | Achado | Correção |
|----|------------|--------|----------|
| S-01 | **Alta** | **SSRF — checker executa qualquer URL cadastrada**: em deploy público, um endpoint pode ser apontado para rede interna (loopback, RFC1918) ou metadata cloud (`169.254.169.254`), permitindo leitura de credenciais IAM e varredura interna. | `internal/checker/security.go` — `ValidateTarget` resolve o host e bloqueia loopback/link-local/multicast/RFC1918/ULA/metadata **por padrão**, com guarda no `RoundTrip` do transporte (mitiga DNS rebinding). Override explícito via `ALLOW_PRIVATE_TARGETS=true` apenas para lab/self-host controlado. |
| S-02 | **Alta** | `GO-2026-5970` — infinite loop em `golang.org/x/text@v0.29.0` (transitiva do pgx), acionável via input inválido. | `go get golang.org/x/text@v0.39.0` (fix upstream). |
| S-03 | **Média** | `gosec G404` — jitter com `math/rand/v2` (PRNG). | `crypto/rand` no `ApplyJitter` (53 bits, fonte criptográfica), com tratamento de erro no engine. |
| S-04 | **Baixa** | `gosec G104` + `GRANT ALL ON SCHEMA public TO public` no `DropAll` (privilégio excessivo). | Erro tratado; GRANT removido (o usuário conectado é owner do schema recriado). |

---

## 3. Achados pendentes (decisão / Fase 3)

| ID | Severidade | Achado | Recomendação |
|----|------------|--------|--------------|
| S-05 | **Alta (prod)** | ~~Headers dos endpoints monitorados em texto plano no jsonb~~ — **RESOLVIDO (hardening)**: cifragem AES-256-GCM em repouso (`internal/seal`, `HEADERS_ENC_KEY`), envelope `$seal_v1` no jsonb; decifra só no engine (para a checagem) e no admin autenticado; público nunca vaza; fail-closed sem chave. | ✅ Aplicado em 2026 (tests + validação ao vivo no Postgres). |
| S-06 | **Média** | **Credenciais default** (dev): `DB_PASSWORD=monitor`, `JWT_SECRET` raso em `.env.example`. | Gerar segredos fortes por ambiente; `JWT_SECRET` ≥ 32 bytes aleatórios; exigência já validada em `ENV=production`. |
| S-07 | **Média** | **Imagens Docker com tag móvel** (`golang:1.27-alpine`, `distroless:static-debian12`, `nginx:1.25-alpine`, `postgres:15-alpine`) — cadeia de suprimentos. | Ancorar por **digest SHA** + `docker scout cves`/Trivy no CI; renovar imagem pelo digest atualizado (Dependabot/Renovate). |
| S-08 | **Média** | ~~Limites de recurso por usuário inexistentes (auto-DoS com muitos endpoints `interval=1s`)~~ — **RESOLVIDO (hardening)**: cotas por conta na API admin. | ✅ Aplicado em 2026: `owner_id` nos endpoints (migração 000005) + `internal/quota` (máx. endpoints/conta, intervalo mínimo, projeção checks/mês) → **HTTP 429** no create/update. |
| S-09 | **Baixa** | `server_name _;` + cert self-signed de exemplo; ciphers agora explícitos. | Produção: acme.sh/certbot com DNS-01, `server_name` real, HSTS preload. |
| S-10 | **Baixa** | **Healthcheck de container trivial** (`/monitor health` retorna 0 sempre) — não verifica engine/DB. | Em produção usar probe HTTP `/healthz`+`/readyz` ou tornar `health` dependente do DB (com timeout). |
| S-11 | **Baixa** | **Controle definitivo do SSRF** depende de defesa em profundidade. | Firewall de **egress** na VPS documentado no `deploy/README.md` (ufw/nf_tables: só 80/443 externo; deny metadata/RFC1918) + `ALLOW_PRIVATE_TARGETS=false` em produção. |
| S-12 | **Info** | `endpoints.body`/`expect_body` podem conter recortes de payloads; `error_detail` logado. | Auditoria: nunca logar corpo/headers/body de endpoints em texto plano; redação em `slog` se necessário. |

---

## 4. Boas práticas já presentes (confirmadas)

- **Queries 100% parametrizadas** (`pgx` `$1..$N`) — sem SQL injection.
- **TLS verificado por padrão** no cliente HTTP do checker (sem `InsecureSkipVerify`).
- **Timeout por checagem** via `context.WithTimeout` — sem requests infinitos.
- **Leitura de corpo limitada** (64 KiB) — sem resposta pesada abusando memória.
- **Binário distroless/nonroot** no runtime e **carência de shell** no container.
- **Migrações idempotentes** em transação; `schema_migrations` rastreável.
- **Panic recovery** por job no pool — processo não morre por input malicioso.
- **Headers de segurança** no Nginx (HSTS, CSP, X-Frame-Options, nosniff) e **redirect 301 → HTTPS**.
- **Segredos fora do repositório** (`.env` gitignored; chave TLS removida do git).

---

## 5. Ferramentas e como reproduzir

```bash
# Vulnerabilidades de dependências
cd backend
go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Análise estática de segurança
go run github.com/securego/gosec/v2/cmd/gosec@latest -fmt=text -quiet ./...

# Testes com detector de corrida
go test -race ./...
```

> Recomendação para o CI: adicionar `govulncheck` + `gosec` como jobs em `develop`/`main`
> (alinhado ao RNF-021) — preferência em PRs.

---

## 6. Plano recomendado

1. **[Aplicado]** Corrigir S-01..S-04 (SSRF guard, x/text, jitter, DropAll).
2. **[Aplicado 2026]** **S-05** (headers cifrados em repouso — `internal/seal`, envelope `$seal_v1`) e **S-08** (cotas por conta — `internal/quota`, `owner_id`, 429) · S-11 (allowlist — coberto na infra do deploy).
3. **[Projeto]** S-06/S-07/S-09/S-10 — hardening de produção (secrets fortes, digests, TLS real, healthcheck real).
4. **[Aplicado]** Adicionar govulncheck + gosec ao pipeline (RNF-021).

---

# Varredura #2 — Fase 3 (API REST + SSE + Notifier)

| Campo        | Valor                                                  |
|--------------|--------------------------------------------------------|
| **Escopo**   | `auth` · `api` (CRUD/admin/stats/público/SSE) · `notifier` · migração 000002 |
| **Data**     | Setembro/2026 (pós-implementação da Fase 3)            |
| **Ferramentas** | `govulncheck` **0 chamáveis** · `gosec` **0 issues** · `go test -race` verde |
| **Status**   | ✅ Correções aplicadas na própria revisão · ↩️ herdados: S-11 (S-05/S-08 **resolvidos** depois) |

## 7. Achados novos corrigidos na revisão da Fase 3

| ID | Severidade | Achado | Correção |
|----|------------|--------|----------|
| S-16 | **Alta** | **Panic em rotas admin** quando `JWT_SECRET` ausente/inválido (dev sem auth): `s.auth` nil → `requireAuth`/`login`/`signup` panics (500 vazio). | Guards: admin → **401**, auth → **503** "autenticação não configurada". |
| S-17 | **Média** | **Credenciais SMTP em claro**: se o servidor não anunciasse `STARTTLS`, `auth` era tentado em texto puro. | **Fail-closed**: recusa autenticar sem TLS (`tlsOK` obrigatório quando há usuário). |
| S-18 | **Média** | **Amplificação SSE**: clientes podiam abrir conexões `text/event-stream` sem limite (cada uma = subscrições + goroutines + canais). | `sseSlots` (semáforo de **256** streams) → **503** quando cheio. |
| S-19 | **Baixa** | Payload de auditoria do notifier continha `env` preenchido com `SMTP_FROM` (campo com valor inesperado/enganoso no JSON). | Removido do payload; auditoria carrega só dados do evento. |
| S-20 | **Baixa** | `headers` ausente no JSON de criação → `NULL` em `jsonb NOT NULL` (Pg) → **500** sem causa clara. | `toEndpoint` normaliza `nil → {}` + handlers **logam o erro do store** (client recebe erro genérico). |
| S-21 | **Média** | Endpoint recém-criado por API tinha runtime com `status=""`; a state machine tratava `""` no `default` e **nunca persistia o status** (STATUS ficava `unknown` para sempre). | `normalizedStatus` (`""→unknown`) em `SyncEndpoint` e no reload; `MemStore` preserva status no update. |

## 8. Verificações positivas da Fase 3 (confirmação)

- **JWT**: algoritmo restrito a **HMAC** (anti *alg-confusion*), `WithExpirationRequired`, issuer fixo, chave ≥ 16 bytes validada.
- **Senhas**: `bcrypt` cost 10; mínimo 8 chars e teto de 72 bytes; **respostas 401 genéricas** (sem enumeração de e-mail — RNF-019).
- **Rate limit**: login/signup por **IP+e-mail** → **429** (RNF-017); nginx `limit_req` em `/api/`.
- **SSRF**: a guarda do checker (S-01) é **reutilizada** no webhook do notifier e na **validação de escrita** de endpoints (ampla defesa).
- **SSE**: eventos e snapshot **sem url/headers/body/password** (RNF-018); JSON snake_case consistente com a API.
- **Input**: `MaxBytesReader(1 MiB)` + `DisallowUnknownFields` no decode.
- **SMTP**: STARTTLS quando disponível; dial com timeout e deadline por operação; retry/backoff sem bloquear o worker (RNF-006).

## 9. Pendências / riscos aceitos da Fase 3

| ID | Severidade | Item | Ação |
|----|------------|------|------|
| S-22 | **Info** | `GO-2026-5932` (`x/crypto/openpgp` — "unsafe by design, sem fix"); **não é usado** (apenas `bcrypt`) e o govulncheck confirma "not called". | **Aceito** — monitorar; remover `x/crypto/openpgp` do build não se aplica (faz parte do módulo). |
| S-23 | **Média** | **Single-tenant**: rotas admin são globais (sem ownership por usuário). OK para self-host; em multi-tenant haveria **IDOR**. | Documentar como decisão de produto; revisar antes de multiusuário. |
| S-24 | **Info** | Rotação de `JWT_SECRET` e migração para **EdDSA/RS256** não implementadas (chave única HS256). | Roadmap de hardening p/ produção (VPS). |
| S-05 | **Alta** | (herdado) Headers/tokens dos endpoints monitorados em **texto plano** no jsonb `headers`. | Cifrar em repouso ou externalizar; **nunca** retornar `headers` fora do admin. |
| S-08 | **Média** | (herdado) Limites de recurso por conta (auto-DoS com muitos endpoints `interval=1s`). | ✅ **Aplicado (hardening): cotas por conta → 429** (`owner_id` + `internal/quota`; env MAX_ENDPOINTS_PER_ACCOUNT, MIN_CHECK_INTERVAL_SECONDS, CHECKS_PER_MONTH_QUOTA). |
| S-11 | **Média** | (herdado) Controle definitivo do SSRF = firewall de egress na VPS. | `nf_tables` no deploy (deny 169.254/16 e RFC1918 p/ o backend). |