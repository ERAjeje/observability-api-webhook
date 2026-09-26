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
| S-05 | **Alta (prod)** | **Headers dos endpoints monitorados** (`auth`, tokens) armazenados em **texto plano** no jsonb `endpoints.headers` (RNF-018). | Cifrar em repouso (age/AES-GCM com chave do env) ou externalizar segredos (vault/secrets manager). A API da Fase 3 **nunca** deve devolver `headers` ao frontend. |
| S-06 | **Média** | **Credenciais default** (dev): `DB_PASSWORD=monitor`, `JWT_SECRET` raso em `.env.example`. | Gerar segredos fortes por ambiente; `JWT_SECRET` ≥ 32 bytes aleatórios; exigência já validada em `ENV=production`. |
| S-07 | **Média** | **Imagens Docker com tag móvel** (`golang:1.27-alpine`, `distroless:static-debian12`, `nginx:1.25-alpine`, `postgres:15-alpine`) — cadeia de suprimentos. | Ancorar por **digest SHA** + `docker scout cves`/Trivy no CI; renovar imagem pelo digest atualizado (Dependabot/Renovate). |
| S-08 | **Média** | **Limites de recurso por usuário** inexistentes: endpoint com `interval=1s` + muitos endpoints = auto-DoS (RF-007 sem teto). | Na Fase 3: rate limits por conta (máx. endpoints, intervalo mínimo, quota de checks/mês) → **429**. |
| S-09 | **Baixa** | `server_name _;` + cert self-signed de exemplo; ciphers agora explícitos. | Produção: acme.sh/certbot com DNS-01, `server_name` real, HSTS preload. |
| S-10 | **Baixa** | **Healthcheck de container trivial** (`/monitor health` retorna 0 sempre) — não verifica engine/DB. | Em produção usar probe HTTP `/healthz`+`/readyz` ou tornar `health` dependente do DB (com timeout). |
| S-11 | **Baixa** | **Controle definitivo do SSRF** depende de defesa em profundidade. | Firewall de **egress** na VPS (nf_tables: só 80/443 externo; deny 169.254/16, RFC1918) + allowlist de IPs por conta na Fase 3. |
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
2. **[Fase 3]** Aplicar S-05 (segredos cifrados), S-08 (limites por conta), S-11 (allowlist de IPs).
3. **[Projeto]** S-06/S-07/S-09/S-10 — hardening de produção (secrets fortes, digests, TLS real, healthcheck real).
4. **[CI]** Adicionar govulncheck + gosec ao pipeline (RNF-021).