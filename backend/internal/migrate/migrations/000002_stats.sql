-- Migration 000002 — P50 de latência nos rollups (RF-018 / T3.3)
-- Os gráficos do painel exigem P50/P95; o P95 já existe desde a Fase 2.

ALTER TABLE check_rollups_minute
    ADD COLUMN IF NOT EXISTS p50_latency_ms bigint NOT NULL DEFAULT 0;