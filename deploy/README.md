# Deploy em produção (VPS)

> Checklist final de integração (RNF-003, RNF-016). Terminal com sudo.

## 1. Provisionamento automático

```bash
DOMAIN=status.suaempresa.com \
EMAIL=ops@suaempresa.com \
REPO_URL=git@github.com:seuorg/monitor.git \
bash deploy/provision.sh
```

O script instala Docker, clona o repo em `/opt/monitor`, gera `.env` com
`DB_PASSWORD` e `JWT_SECRET` aleatórios (openssl), builda o frontend, sobe a
stack e emite certificado TLS real via **acme.sh** (modo standalone, porta 80
livre por alguns segundos) — instalando como `fullchain.pem`/`privkey.pem` no
layout que o nginx já espera.

> **Modo local** (sem `REPO_URL`): copia o diretório atual — útil para quem
> roda direto na VPS. Se não houver Node no host, usará o `frontend/dist`
> commitado (requer `make frontend-build` antes de commitar mudanças de UI).

## 2. Pós-deploy — checagens obrigatórias

```bash
docker compose ps                # postgres/backend/nginx Up (healthy)
curl -fsS https://status.x/healthz   # {"status":"ok"}
curl -fsS https://status.x/metrics   # métricas do worker/motor (RF-012)
```

## 3. Operação

| Item | Como |
|------|------|
| Atualizar | `git pull && make frontend-build && docker compose up -d --build` |
| Logs | `docker compose logs -f --tail=200` |
| Backup `pgdata` | `docker run --rm -v project-3_pgdata:/data -v $PWD:/backup alpine tar czf /backup/pgdata-$(date +%F).tgz -C /data .` |
| Certificado | renovação automática via cron do acme.sh; `--reloadcmd` reinicia o nginx |

## 4. Rede / segurança (referência ao security-review)

- **`/metrics` sem auth** por design (scrape Prometheus) → **restrinja por
  firewall/ufw** a origem do Prometheus (achado **S-11** aplicado na infra):
  ```bash
  ufw allow from 10.0.0.0/8  to any port 443 proto tcp  # exemplos
  ufw deny 443/tcp  # se coletar só por SSH túnel, o resto
  
  # exemplo de bloqueio amplo na camada de rede p/ o /metrics:
  docker compose port nginx 443   # descubra a porta mapeada local
  ```
- **`ALLOW_PRIVATE_TARGETS`**: manter `false` em produção (bloqueia SSRF —
  S-01) a menos que haja necessidade real de monitorar a rede interna.
- **Firewall mínimo**: liberar 80 (redirect + ACME) e 443; SSH apenas por chave.
- **Backups**: agendar o dump do `pgdata` (RNF-003 — retomada após crash).

## 5. Verificação do deploy (roda da sua máquina)

```bash
curl -sS https://status.x/api/v1/status | head
# Esperar: {"generated_at": ..., "endpoints": [...], "incidents_open": [...]}
```