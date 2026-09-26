-- Migration 000001 — Esquema inicial (arquitetura §5.1, T1.5)
-- Central de Monitoramento de APIs & Webhooks

-- Ajuda a criar as partições mensais da tabela checks.
-- Parâmetros: nome da partição, data de início (YYYY-MM-DD), data de fim.
CREATE OR REPLACE FUNCTION create_check_partition(part_name text, start_date text, end_date text)
RETURNS void AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF checks FOR VALUES FROM (%L) TO (%L)',
            part_name, start_date, end_date
        );
    END IF;
END;
$$ LANGUAGE plpgsql;

-- ─── Usuários (painel administrativo — RF-006/RNF-019) ───────────────────
CREATE TABLE users (
    id            bigserial PRIMARY KEY,
    email         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- ─── Grupos de endpoints (RF-005) ────────────────────────────────────────
CREATE TABLE check_groups (
    id            bigserial PRIMARY KEY,
    name          text NOT NULL,
    display_order int NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- ─── Endpoints (RF-001..RF-005) ──────────────────────────────────────────
CREATE TABLE endpoints (
    id                bigserial PRIMARY KEY,
    group_id          bigint REFERENCES check_groups(id) ON DELETE SET NULL,
    name              text NOT NULL,
    url               text NOT NULL,
    method            text NOT NULL DEFAULT 'GET',
    headers           jsonb NOT NULL DEFAULT '{}',
    body              text NOT NULL DEFAULT '',
    interval_seconds  integer NOT NULL DEFAULT 300 CHECK (interval_seconds >= 1),
    timeout_ms        integer NOT NULL DEFAULT 10000 CHECK (timeout_ms > 0),
    lat_threshold_ms  integer NOT NULL DEFAULT 0,
    expect_status     integer NOT NULL DEFAULT 0,
    expect_body       text NOT NULL DEFAULT '',
    active            boolean NOT NULL DEFAULT true,
    status            text NOT NULL DEFAULT 'unknown',
    next_check_at     timestamptz NOT NULL DEFAULT now(),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- Agendamento eficiente: só ativos, só vencidos (T2.3).
CREATE INDEX idx_endpoints_active_due ON endpoints (next_check_at) WHERE active;

-- ─── Checks — log bruto de cada checagem (RF-014), particionada por mês ───
CREATE TABLE checks (
    id           bigserial,
    endpoint_id  bigint NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    checked_at   timestamptz NOT NULL,
    result       text NOT NULL CHECK (result IN ('ok','degraded','fail')),
    http_status  integer,
    latency_ms   bigint NOT NULL,
    error_detail text NOT NULL DEFAULT ''
) PARTITION BY RANGE (checked_at);

-- Consultas por endpoint + recência (T2.6 / RF-018).
CREATE INDEX idx_checks_endpoint_time ON checks (endpoint_id, checked_at DESC);

-- Partição default (segurança) + do mês corrente criada em runtime.
CREATE TABLE checks_default PARTITION OF checks DEFAULT;

-- ─── Rollups por minuto (RF-015 / RNF-014) ───────────────────────────────
CREATE TABLE check_rollups_minute (
    endpoint_id    bigint NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    bucket         timestamptz NOT NULL,
    count          bigint NOT NULL DEFAULT 0,
    ok_count       bigint NOT NULL DEFAULT 0,
    sum_latency_ms bigint NOT NULL DEFAULT 0,
    p95_latency_ms bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (endpoint_id, bucket)
);

-- ─── Incidents — janelas de downtime (RF-017) ────────────────────────────
CREATE TABLE incidents (
    id          bigserial PRIMARY KEY,
    endpoint_id bigint NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    started_at  timestamptz NOT NULL,
    ended_at    timestamptz,
    duration_ms bigint,
    resolution  text NOT NULL DEFAULT ''
);

CREATE INDEX idx_incidents_endpoint ON incidents (endpoint_id, started_at DESC);

-- ─── Notifications — auditoria de alertas (RF-029) ───────────────────────
CREATE TABLE notifications (
    id           bigserial PRIMARY KEY,
    endpoint_id  bigint NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    incident_id  bigint REFERENCES incidents(id) ON DELETE SET NULL,
    channel      text NOT NULL,
    payload      jsonb NOT NULL DEFAULT '{}',
    delivered_at timestamptz,
    status       text NOT NULL DEFAULT 'pending'
);