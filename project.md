# Project 3 — Central de Monitoramento de APIs & Webhooks

## 📋 Descrição

Aplicação que realiza **verificações periódicas de health-check** em serviços/endpoints cadastrados pelo
usuário, armazena **logs de latência/status** e fornece uma **página pública de status em tempo real**
(status page).

## 🧱 Stack

| Camada | Tecnologia |
|---|---|
| Frontend | React, TypeScript, Recharts / Chart.js, Tailwind CSS |
| Backend | Go **ou** Node.js com agendador de tarefas (Cron / Worker Threads / Goroutines) |
| Banco de Dados | PostgreSQL **ou** Redis + TimescaleDB |
| Deploy | Docker Compose com Dockerfile multi-stage e Nginx Reverse Proxy |

## 🎯 O que o projeto comprova

1. **Trabalhos assíncronos / background jobs** e **mensageria / eventos em tempo real** (WebSockets ou SSE).
2. **Construção e renderização eficiente de gráficos** e métricas de desempenho.
3. **Arquitetura resiliente de backend** e **alta aderência a práticas DevOps na VPS**.

## 🧩 Funcionalidades centrais

- Cadastro de serviços/endpoints (nome, URL, intervalo de checagem, timeouts, etc.).
- Verificação periódica de health-check (agendador/worker).
- Armazenamento de logs de latência/status por checagem.
- Página pública de status em tempo real (SSE/WebSockets) com gráficos (uptime, latência, histórico).

## 📌 Próximo passo — validar CI/Deploy reais (infra)

✅ **Fases 1–4 + Checklist + Hardening S-05/S-08** · ✅ Sobrecarga **memória e
PostgreSQL real** (`make load-test-pg`) · ✅ Deploy validado por **smoke local**
(`SKIP_TLS=1` — provision.sh end-to-end, sem infra). Restam — todos dependem
de infraestrutura externa:

- **[ ]** **Push para o remoto** → rodar o CI de verdade (GitHub Actions) e o **E2E em runner limpo**
- **[ ]** **VPS**: executar `deploy/provision.sh` (TLS real via acme.sh, `DOMAIN`/`EMAIL`),
  validar operação + backups (RNF-003)
- **[ ]** Segurança do `docs/security-review.md`: restam apenas pendências **de infra/produto** —
  **S-06/S-07/S-09/S-10** (hardening de produção no deploy: digests de imagem, secrets fortes,
  TLS real, healthcheck real) — **S-05 e S-08 resolvidos** em código, **S-11 coberto** na infra
  do `deploy/README.md`, **TLS self-signed** já coberto pelo smoke.

> ⚠️ Ao publicar no remoto, remover certificados locais de `deploy/nginx/certs` (gitignored) e
> garantir que `frontend/dist` esteja atualizado (`make frontend-build`).

## 📌 Decisões abertas (a definir durante o desenvolvimento)

- CI/CD: GitHub Actions (workflow já incluído) vs GitLab CI (repo é local — decidir no push).
- Multi-tenant vs single-tenant (rotas admin globais hoje — ver S-23 no security-review).