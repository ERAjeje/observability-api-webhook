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

## 📌 Próximo passo — Hardening pendente + validar CI/Deploy reais

✅ **Fases 1–4 + Checklist de Integração** (CI workflow + `make ci`, deploy script/doc,
Sobrecarga com fix de double-run, Observabilidade `/metrics`). Restam:

- **[ ]** **Push para o remoto** → rodar o CI de verdade (GitHub Actions) e o **E2E em runner limpo**
- **[ ]** **VPS**: executar `deploy/provision.sh` (TLS real), validar operação + backups (RNF-003)
- **[ ]** **Sobrecarga em ambiente real**: N endpoints × 1 min contra o PostgreSQL (o load-test roda em memória)
- **[ ]** Segurança do `docs/security-review.md`: **S-05** (cifrar headers dos endpoints em repouso),
  **S-08** (cotas por conta), **S-11** (firewall de egress — aplicável na infra do deploy)

> ⚠️ Ao publicar no remoto, remover certificados locais de `deploy/nginx/certs` (gitignored) e
> garantir que `frontend/dist` esteja atualizado (`make frontend-build`).

## 📌 Decisões abertas (a definir durante o desenvolvimento)

- CI/CD: GitHub Actions (workflow já incluído) vs GitLab CI (repo é local — decidir no push).
- Multi-tenant vs single-tenant (rotas admin globais hoje — ver S-23 no security-review).