# Requisitos — Central de Monitoramento de APIs & Webhooks (Status Page + Logger)

| Campo        | Valor                                                            |
|--------------|------------------------------------------------------------------|
| **Produto**  | Central de Monitoramento de APIs & Webhooks                      |
| **Escopo**   | Health-checks periódicos, logs de latência/status, status page pública em tempo real |
| **Módulo**   | API privada (gestão) + Worker de checagens + Status page pública |
| **Versão**   | 1.0 (draft para validação)                                       |
| **Data**     | Setembro/2026                                                    |
| **Status**   | Proposto — aguardando feedback                                   |

---

## 1. Visão Geral do Produto

### 1.1 Contexto e problema

Equipes DevOps/SRE dependem de serviços de terceiros, APIs próprias e webhooks cuja
disponibilidade muda sem aviso. Sem uma camada de observação, uma queda só é descoberta
quando o usuário final reclama. Serviços de monitoramento prontos existem, mas são caros,
fechados e nem sempre permitem expor o status ao público (status page) com a marca do produto.

### 1.2 Proposta de valor

A **Central de Monitoramento** é uma aplicação self-hosted que:

1. **Verifica periodicamente** (health-check) endpoints cadastrados pelo usuário — método,
   timeout e intervalo configuráveis por endpoint.
2. **Registra logs** de latência e status a cada checagem, com histórico e agregações
   (uptime percentual, percentis de latência).
3. **Publica uma status page pública e em tempo real**, alimentada por SSE/WebSockets, com
   gráficos e timeline de incidentes — permitindo que o usuário divulgue o estado dos serviços
   aos seus próprios clientes.

### 1.3 Fluxo macro

```
Operador/DevOps ──cadastra endpoints──▶ API privada (gestão)
        ──▶ Scheduler dispara checagens periódicas
Worker ──executa HTTP check──▶ Logs de latência/status (banco)
        ──▶ Detecção de transição de estado (UP→DOWN/DOWN→UP)
        ├──▶ Alertas (e-mail/webhook)
        └──▶ Evento em tempo real (SSE/WS)
Status page pública ──renderiza gráficos + status atual / incidentes──▶ Cliente final
```

### 1.4 Escopo

| ✔️ Dentro do escopo (MVP)                       | ❌ Fora do escopo (fases futuras)              |
|-------------------------------------------------|-----------------------------------------------|
| Cadastro/edição de endpoints (URL, método, headers, timeout) | Monitoramento multi-região (diversos PoPs) |
| Health-checks periódicos com agendador/worker   | Checks sintéticos de interface (browser)       |
| Validação por status HTTP e/ou conteúdo         | Portal de incidentes com publicações manuais   |
| Logs de latência/status com agregações (uptime, percentis) | Integração com PagerDuty/Opsgenie         |
| Status page pública em tempo real (SSE/WS)      | SLA monetário (credit system)                  |
| Alertas por e-mail e webhook (Slack/Discord)    | Multi-tenancy com billing                      |
| Timeline/incidentes automáticos (início–fim)    | Métricas avançadas (redirect trace, cert expiry) |
| Docker Compose + Nginx (deploy na VPS)          | Auth OAuth/SSO (MVP usa e-mail+senha)          |

---

## 2. Personas

### 2.1 Engenheiro DevOps / SRE — "Quem opera"

| Atributo            | Detalhe                                                        |
|---------------------|----------------------------------------------------------------|
| **Quem é**          | Engenheiro(a) responsável pela disponibilidade de serviços próprios e de terceiros usados em produção. |
| **Contexto**        | Gerencia APIs, webhooks e dependências externas; viaja no tempo entre deploys, incidentes e a rotina operacional. |
| **Objetivos**       | Detectar quedas rapidamente, entender a duração do incidente, comunicar o status sem depender de reclamação de cliente. |
| **Necessidades**    | Cadastro rápido de endpoints, limites de latência/estado, alertas acionáveis (sem ruído), histórico fiel para pós-mortem. |
| **Dores**           | Descobrir queda tarde, alertas em excesso (falso positivo), sem histórico para justificar SLA e sem uma página pública para comunicação. |
| **Critério de sucesso** | Descobre a queda em **menos de 2 minutos** após a primeira falha e comprova o incidente com logs e janela de downtime precisos. |

### 2.2 Cliente Final — "Quem consome a status page"

| Atributo            | Detalhe                                                        |
|---------------------|----------------------------------------------------------------|
| **Quem é**          | Usuário do produto/serviço que o operador monitora (consumidor da API, cliente da empresa). |
| **Contexto**        | Acessa a página pública de status para saber se uma indisponibilidade é problema dele ou do sistema. |
| **Objetivos**       | Ver, rápido e sem login, se o serviço está operacional, se há incidente em andamento e há quanto tempo. |
| **Necessidades**    | Página leve, autoexplicativa, atualizada em tempo real, com status por serviço e histórico de uptime. |
| **Dores**           | Página que não reflete o momento real, sem histórico, ou que exige suporte/ticket para descobrir o estado. |
| **Critério de sucesso** | Em **menos de 5 segundos** entende o status atual do serviço e se existe incidente — sem recarregar a página. |

---

## 3. Requisitos Funcionais

Identificadores: **RF-XXX** (rastreáveis pelos casos de uso UC-01..UC-05).

### 3.1 Cadastro de Endpoints

| ID      | Requisito                                                                                                                      | Prioridade |
|---------|--------------------------------------------------------------------------------------------------------------------------------|------------|
| RF-001  | O sistema deve permitir **cadastrar um endpoint** com: nome, URL, método HTTP , headers opcionais, corpo opcional, intervalo de checagem e timeout. | Alta |
| RF-002  | O sistema deve permitir **listar, editar, ativar/desativar e excluir** endpoints cadastrados.                                  | Alta |
| RF-003  | O sistema deve **validar a URL** no cadastro (scheme `http/https`, host válido) e oferecer **teste manual** de conectividade antes de salvar. | Alta |
| RF-004  | O usuário deve poder definir **limites de latência** (alert threshold) para classificar degradação de performance por endpoint. | Média |
| RF-005  | O usuário deve poder **agrupar endpoints** (ex.: "API de Pagamentos", "Webhook Stripe") para exibição organizada na status page e alertas por grupo. | Média |
| RF-006  | Toda rota de gestão de endpoints deve ser **protegida por autenticação**; sem sessão válida retorna **401** sem expor dados.      | Alta |

### 3.2 Verificação Periódica / Health-checks

| ID      | Requisito                                                                                                                      | Prioridade |
|---------|--------------------------------------------------------------------------------------------------------------------------------|------------|
| RF-007  | O sistema deve executar **checagens periódicas** por endpoint conforme o intervalo configurado (ex.: 1, 5, 15, 30 ou 60 minutos). | Alta |
| RF-008  | O sistema deve **classificar o resultado** como `UP` (success), `DOWN` (falha) ou `DEGRADED` (latência acima do limite RF-004), a partir do status HTTP e da latência. | Alta |
| RF-009  | O sistema deve **respeitar o timeout** por endpoint: sem resposta no prazo, a checagem é registrada como falha.                 | Alta |
| RF-010  | O usuário deve poder definir **validação avançada** por endpoint: status HTTP esperado e/ou conteúdo esperado no corpo da resposta (regex/texto). | Média |
| RF-011  | O agendador não deve permitir **execuções sobrepostas** do mesmo endpoint: se a checagem anterior ainda está em andamento, a nova é pulada e registrada. | Alta |
| RF-012  | As checagens devem continuar rodando mesmo com degradação de partes do sistema (ex.: falha temporária de banco não derruba o agendador). | Alta |
| RF-013  | O sistema deve usar **confirmação por janela**: declarar `DOWN` apenas após **N falhas consecutivas** (configurável, default 3) e `UP` após **M sucessos consecutivos** (configurável, default 2), evitando falso positivo. | Alta |

### 3.3 Armazenamento de Logs de Latência/Status

| ID      | Requisito                                                                                                                      | Prioridade |
|---------|--------------------------------------------------------------------------------------------------------------------------------|------------|
| RF-014  | Cada checagem deve gerar um **log** com: timestamp, endpoint, status classificado, código HTTP, latência (ms) e detalhe de erro. | Alta |
| RF-015  | O sistema deve **agregar** os logs por período (ex.: média/P95 de latência e contagem por minuto/hora) para consultas de gráficos eficientes. | Alta |
| RF-016  | O sistema deve calcular **uptime percentual** (diário, semanal, mensal) por endpoint a partir dos logs.                          | Alta |
| RF-017  | O sistema deve **consolidar incidentes**: ao declarar `DOWN`/`DEGRADED`, abrir janela com início; ao recuperar, fechar com fim e duração. | Alta |
| RF-018  | O sistema deve expor **API de consulta de logs** paginada e filtrável (por endpoint, período, status) para o painel administrativo. | Média |

### 3.4 Página Pública de Status

| ID      | Requisito                                                                                                                      | Prioridade |
|---------|--------------------------------------------------------------------------------------------------------------------------------|------------|
| RF-019  | A **status page** deve ser pública (sem autenticação) e listar os endpoints com status atual (`UP`/`DOWN`/`DEGRADED`).          | Alta |
| RF-020  | A página deve exibir **gráficos de latência e uptime** por endpoint (dia/semana/mês), renderizados eficientemente.              | Alta |
| RF-021  | A página deve ser **atualizada em tempo real** (SSE ou WebSockets) quando um novo resultado de checagem for produzido, sem recarga. | Alta |
| RF-022  | A página deve exibir **timeline de incidentes** (início, fim, duração, endpoints afetados).                                     | Alta |
| RF-023  | A página deve ser **customizável** (título, descrição, logotipo e cores) para refletir a marca do usuário.                      | Média |
| RF-024  | A página deve ter **bom contraste e baixa latência de percepção** (formato leito text-based) para leitura em poucos segundos.    | Média |

### 3.5 Alertas e Notificações

| ID      | Requisito                                                                                                                      | Prioridade |
|---------|--------------------------------------------------------------------------------------------------------------------------------|------------|
| RF-025  | O sistema deve **notificar o usuário em transições de estado**: `UP→DOWN`, `DOWN→UP` e `DEGRADED` (conforme RF-013).            | Alta |
| RF-026  | O sistema deve suportar canais de alerta: **e-mail** e **webhook** (ex.: Slack/Discord) com URL configurável.                    | Alta |
| RF-027  | O sistema deve **suprimir alertas repetidos** dentro de uma janela configurável (evitar enxurrada/ruído em flutuações).        | Alta |
| RF-028  | O alerta de **recuperação** deve incluir resumo do incidente (início, fim, duração e motivo), e o de **queda** deve incluir endpoint, erro e horário da primeira falha confirmada. | Alta |
| RF-029  | O sistema deve **registrar todas as notificações enviadas** (canal, destino, payload, status de entrega) para auditoria.         | Média |

---

## 4. Requisitos Não Funcionais

Identificadores: **RNF-XXX**.

### 4.1 Alta Disponibilidade

| ID       | Requisito                                                                                                                  | Alcance/Limite                    |
|----------|----------------------------------------------------------------------------------------------------------------------------|-----------------------------------|
| RNF-001  | A **plataforma** (API + status page) deve ter **SLA de 99,5%** de uptime mensal.                                            | 99,5% mensal                      |
| RNF-002  | O **worker de checagens** deve ser resiliente a crashes e reinicializações: retomar o agendamento sem perder ou duplicar checagens (idempotência). | retomada segura               |
| RNF-003  | Deploy via **Docker Compose com atualização sem downtime** (rolling / múltiplas réplicas) e **Nginx reverse proxy** com health-check de upstream. | zero-downtime deploy         |
| RNF-004  | Falha temporária de um componente (banco, fila) **não deve interromper** o critério de execução das checagens (degradação parcial aceitável). | degradação parcial            |

### 4.2 Notificações (Alertas)

| ID       | Requisito                                                                                                                  | Alcance/Limite                    |
|----------|----------------------------------------------------------------------------------------------------------------------------|-----------------------------------|
| RNF-005  | A **notificação de transição de estado** deve ser entregue em até **60 segundos** após a confirmação da transição (RF-013).   | ≤ 60 s                            |
| RNF-006  | Os **webhooks de alerta** devem ter **timeout curto e retry com backoff** (ex.: 3 tentativas com crescente), sem bloquear o worker. | 3 retries, backoff           |
| RNF-007  | Deve existir **janela de supressão** global (RNF complementar ao RF-027) para que flutuações de 1 checagem não gerem múltiplos alertas. | supressão configurável       |
| RNF-008  | Falha de entrega de um canal (ex.: e-mail offline) **não deve impedir** a entrega por outro canal configurado (tentativa paralela/fallback). | multi-canal                  |

### 4.3 Atualização em Tempo Real da Interface

| ID       | Requisito                                                                                                                  | Alcance/Limite                    |
|----------|----------------------------------------------------------------------------------------------------------------------------|-----------------------------------|
| RNF-009  | A **status page** deve refletir uma nova checagem em até **5 segundos** após a persistência do resultado (via SSE/WebSockets). | ≤ 5 s                             |
| RNF-010  | O cliente em tempo real deve reconectar automaticamente com **exponential backoff** e, ao reconectar, receber o **snapshot atual** (sem divergência visual). | backoff + snapshot            |
| RNF-011  | O mecanismo de tempo real (SSE recomendado) deve suportar o **volume esperado de eventos** sem perda: checagens de dezenas de endpoints com intervalos de 1 min. | ≥ 100 eventos/s              |
| RNF-012  | A janela de atualização em tempo real deve fechar quando o usuário desativar o endpoint (evento de remoção/suspensão imediato). | remoção imediata             |

### 4.4 Performance e Gráficos

| ID       | Requisito                                                                                                                  | Alcance/Limite                    |
|----------|----------------------------------------------------------------------------------------------------------------------------|-----------------------------------|
| RNF-013  | A **status page** e a **API de consulta** devem responder com **P95 < 300 ms** em condições normais.                          | P95 < 300 ms                      |
| RNF-014  | Os **gráficos** devem renderizar a partir de **dados agregados** (RF-015) com paginação/limite de pontos, sem carregar logs brutos na UI. | agregação + limite de pontos |
| RNF-015  | A interface deve atingir **Lighthouse Performance ≥ 90** (móvel, 4G) na status page.                                         | performance ≥ 90                  |

### 4.5 Segurança

| ID       | Requisito                                                                                                                  | Alcance/Limite                    |
|----------|----------------------------------------------------------------------------------------------------------------------------|-----------------------------------|
| RNF-016  | Toda comunicação deve ser **HTTPS/TLS** (terminada no Nginx); tráfego em claro é redirecionado.                               | TLS ≥ 1.2 (1.3 preferido)       |
| RNF-017  | Endpoints da **área administrativa** devem ser separados da status page pública e **protegidos por autenticação + rate limit**. | rotas privadas + rate limit   |
| RNF-018  | **Segredos** (tokens de autenticação dos endpoints monitorados, URLs de webhook) armazenados de forma segura e nunca expostos na status page ou nos logs. | segredos protegidos           |
| RNF-019  | Senhas do painel com **hash forte (Argon2id/bcrypt)** e sem armazenamento em texto claro.                                    | Argon2id / bcrypt                |
| RNF-020  | Logs de auditoria **sem dados sensíveis** (headers, tokens, payload de webhook); erros não vazam stack para o cliente.        | redação automática              |

---

## 5. Casos de Uso (BDD) — Detecção de Quedas, Recuperação e Alertas

### UC-01 — Detecção de queda (downtime) confirmada e alertada

```gherkin
Funcionalidade: Detecção de queda de endpoint

  Cenário: Endpoint falha de forma consistente e o sistema declara DOWN
    Dado que existe um endpoint "API de Pagamentos" monitorado a cada 1 minuto
    E que o limite de falhas consecutivas para declarar DOWN é 3
    Quando o endpoint passar a responder com status HTTP 5xx de forma contínua
    E 3 checagens consecutivas falharem dentro de 3 minutos
    Então o sistema deve classificar o endpoint como DOWN
    E deve abrir um incidente com horário de início igual à primeira falha confirmada
    E deve enviar alerta de queda (e-mail e/ou webhook) em até 60 segundos
    E o evento de mudança de estado deve ser propagado à status page em até 5 segundos
    E o endpoint deve aparecer como "Indisponível" publicamente
```

> Rastreabilidade: **UC-01 → RF-007, RF-008, RF-013, RF-017, RF-025, RF-028, RNF-005, RNF-009**.

### UC-02 — Falso positivo evitado (falha isolada não derruba o status)

```gherkin
Funcionalidade: Robustez contra falsos positivos

  Cenário: Uma única falha transitória não declara queda
    Dado que o limite de falhas consecutivas para declarar DOWN é 3
    Quando o endpoint "Webhook de Pedidos" falhar exatamente 1 vez (timeout transitório)
    E a checagem seguinte responder com status 200 em 80 ms
    Então o sistema deve manter o estado como UP
    E deve registrar a falha isolada apenas no log de checagens
    E não deve abrir incidente
    E não deve enviar alerta de queda
```

> Rastreabilidade: **UC-02 → RF-008, RF-013, RF-014, RF-017**.

### UC-03 — Recuperação: endpoint volta a responder e incidente é fechado

```gherkin
Funcionalidade: Detecção de recuperação

  Cenário: Endpoint em DOWN volta a responder com confirmação
    Dado que o endpoint "API de Pagamentos" está com estado DOWN
    E que um incidente está aberto desde "14:02" (primeira falha confirmada)
    E que o limite de sucessos consecutivos para declarar UP é 2
    Quando o endpoint voltar a responder com status 200
    E 2 checagens consecutivas forem bem-sucedidas
    Então o sistema deve classificar o endpoint como UP
    E deve fechar o incidente registrando fim e duração (tempo de downtime)
    E deve enviar alerta de recuperação com resumo (início, fim, duração)
    E a status page deve exibir o endpoint como operacional e o incidente resolvido
    E o uptime percentual deve considerar a janela de downtime no cálculo
```

> Rastreabilidade: **UC-03 → RF-013, RF-016, RF-017, RF-025, RF-028, RNF-005**.

### UC-04 — Alertas sem enxurrada (supressão de ruído)

```gherkin
Funcionalidade: Supressão de alertas repetidos

  Cenário: Flutuações rápidas não geram alertas repetidos em sequência
    Dado que o endpoint "Webhook de Pedidos" está com estado DOWN confirmado
    E que já foi enviado um alerta de queda há 30 segundos
    Quando o endpoint oscilar e a próxima checagem voltar a falhar (segundo DOWN consecutivo)
    Então o sistema não deve enviar novo alerta de queda dentro da janela de supressão
    E deve registrar a nova falha apenas no log e no incidente em andamento
    E deve voltar a notificar apenas em **transição de estado** (recuperação ou novo ciclo)
```

> Rastreabilidade: **UC-04 → RF-014, RF-025, RF-027, RNF-007**.

### UC-05 — Status page em tempo real no cliente final (SSE/WebSocket)

```gherkin
Funcionalidade: Atualização em tempo real da status page

  Cenário: Cliente final vê a transição UP→DOWN sem recarregar a página
    Dado que um cliente final está visualizando a status page pública
    E que o endpoint "API de Checkout" está operacional (UP) na tela
    Quando o sistema confirmar a queda do endpoint (DOWN)
    Então a status page deve atualizar o status para "Indisponível" em até 5 segundos
    E deve destacar o incidente em andamento na timeline
    E o gráfico de uptime do dia deve refletir a nova janela de downtime no próximo tick
    E se a conexão em tempo real cair, o cliente deve reconectar automaticamente
    E ao reconectar deve receber o snapshot atual com o estado verdadeiro
```

> Rastreabilidade: **UC-05 → RF-019, RF-020, RF-021, RF-022, RNF-009, RNF-010, RNF-011**.

---

## 6. Matriz de Rastreabilidade (Resumo)

| Caso de Uso | Requisitos Funcionais | Requisitos Não Funcionais |
|-------------|------------------------|---------------------------|
| UC-01       | RF-007, RF-008, RF-013, RF-017, RF-025, RF-028 | RNF-005, RNF-009 |
| UC-02       | RF-008, RF-013, RF-014, RF-017                  | —                 |
| UC-03       | RF-013, RF-016, RF-017, RF-025, RF-028          | RNF-005           |
| UC-04       | RF-014, RF-025, RF-027                          | RNF-007           |
| UC-05       | RF-019, RF-020, RF-021, RF-022                  | RNF-009, RNF-010, RNF-011 |

---

## 7. Fora de Escopo (Fase 2 — não bloqueiam o MVP)

- **Monitoramento multi-região** (execução de checagens a partir de múltiplos PoPs/Datacenters).
- **Checks sintéticos de UI** (navegação de browser real simulando usuário).
- **Publicação manual de incidentes** ("manutenção programada") além dos incidentes automáticos.
- **Integrações de alerta avançadas** (PagerDuty, Opsgenie) além de e-mail e webhook.
- **Certificado SSL / expiração e redirect tracing** como métricas monitoradas.
- **Multi-tenancy com billing** (a versão atual não fatura por tenant).
- **Auth via OAuth/SSO** (MVP usa e-mail + senha).
- **Correlação de causa raiz** entre múltiplos endpoints.