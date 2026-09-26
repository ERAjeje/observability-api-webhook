-- S-08 — cotas por conta: vínculo de dono nos endpoints.
-- owner_id = conta admin que cadastrou (users.id). 0 = legado/sem dono.
ALTER TABLE endpoints ADD COLUMN IF NOT EXISTS owner_id BIGINT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_endpoints_owner ON endpoints (owner_id);

-- Backfill idempotente: endpoints legados (sem dono) passam para a primeira
-- conta admin existente — assim entram nas cotas e na contabilidade do S-08.
UPDATE endpoints
   SET owner_id = (SELECT id FROM users ORDER BY id LIMIT 1)
 WHERE owner_id = 0
   AND EXISTS (SELECT 1 FROM users);