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

## 📌 Próximo passo — Integração final (CI · Deploy · Hardening)

✅ **Fases 1, 2, 3 e 4 concluídas** (setup + core engine + API REST/SSE/notifier + frontend). Restam os itens do checklist final:

- **[ ]** **CI** verde em `develop`: lint + testes (`-race`) + build backend/frontend + E2E Playwright (RNF-021)
- **[ ]** **Deploy** na VPS: `docker compose up -d` com TLS real (certbot/acme) (RNF-003, RNF-016)
- **[ ]** **Sobrecarga** validada: N endpoints × intervalo 1 min sem stackar worker (RNF-002, RNF-011)
- **[ ]** **Observabilidade**: logs estruturados + métricas do worker (RF-012)
- **[ ]** Segurança pendente do `docs/security-review.md`: **S-05** (cifrar headers dos endpoints), **S-08** (cotas por conta), **S-11** (firewall de egress)

## 📌 Decisões abertas (a definir durante o desenvolvimento)

- CI/CD: GitHub Actions vs GitLab CI (o repo é local; decidir no deploy).
- Multi-tenant vs single-tenant (rotas admin globais hoje — ver S-23 no security-review).