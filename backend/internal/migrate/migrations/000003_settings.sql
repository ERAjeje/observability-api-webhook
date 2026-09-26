-- Migration 000003 — Configurações dinâmicas (RF-023 / T4.5)
-- Branding da status page (título, descrição, cor primária) e canais de
-- alerta editáveis do painel. SMTP_PASS permanece apenas em env (S-05).

CREATE TABLE IF NOT EXISTS app_settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);