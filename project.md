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

## 📌 Próximo passo — Fase 3 (API REST + SSE + Alertas)

✅ **Fases 1 e 2 concluídas** (setup + core engine). Próxima etapa:

- **[ ]** **T3.1** Auth JWT + bcrypt + rate limit (rotas admin)
- **[ ]** **T3.2** CRUD REST de endpoints/grupos + teste manual de conectividade
- **[ ]** **T3.3** API de logs/stats (rollups, P50/P95, uptime)
- **[ ]** **T3.4** API pública de status (sem auth) + timeline de incidentes
- **[ ]** **T3.5/T3.6** SSE `/api/v1/events` (broker + snapshot no connect + heartbeat)
- **[ ]** **T3.7** Notifier (e-mail/webhook, supressão, retry/backoff, auditoria)

> ⚠️ Antes de iniciar a Fase 3: aplicar as correções da **varredura de segurança**
> (ver `docs/security-review.md`).

## 📌 Decisões abertas (a definir durante o desenvolvimento)

- Backend: Go vs Node.js.
- Banco: PostgreSQL vs Redis + TimescaleDB.
- Detalhamento da stack DevOps (Nginx, HTTPS, CI/CD).