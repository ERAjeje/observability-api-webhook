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

## 📌 Próximo passo — Fase 4 (Frontend: Dashboard + Status Page)

✅ **Fases 1, 2 e 3 concluídas** (setup + core engine + API REST/SSE/notifier). Próxima etapa:

- **[ ]** **T4.1/T4.2** Scaffold React+Vite+Tailwind+React Query + cliente API/SSE
- **[ ]** **T4.3** Login e CRUD de endpoints/grupos no dashboard admin
- **[ ]** **T4.4** Logs/stats (tabela paginada + gráficos de latência/uptime)
- **[ ]** **T4.5** Configuração de alertas (canais, supressão, limites)
- **[ ]** **T4.6/T4.7** Status page pública (cards + Recharts) + SSE em tempo real
- **[ ]** **T4.8/T4.9** E2E Playwright do fluxo completo (UC-01/UC-05)

> ⚠️ Antes da Fase 4: revisar `docs/security-review.md` (achados S-05/S-08/S-11
> recomendados para acompanhar a API).

## 📌 Decisões abertas (a definir durante o desenvolvimento)

- Frontend: Vite + React Router vs Next.js (SPA estático no Nginx é o padrão assumido).
- Painel de alertas: permitir criar alertas por endpoint individual ou só global (T4.5).
- Detalhamento da stack DevOps (CI/CD, observabilidade).