-- 014_audit_and_retention.sql
-- Denetim kaydı geriye dönük yazılamaz; saklama süresi tek doğruluk kaynağı
-- olmalı — env ile DB yarışırsa KVKK metnindeki sayı yalan olur.

CREATE TABLE IF NOT EXISTS audit_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    action     TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    detail     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log (created_at DESC);

-- NULL: henüz panel/boot seed etmedi. İlk açılışta RETENTION_DAYS yazar;
-- sonrasında doğruluk kaynağı bu sütun.
ALTER TABLE llm_settings
  ADD COLUMN IF NOT EXISTS retention_days INTEGER;

ALTER TABLE llm_settings DROP CONSTRAINT IF EXISTS llm_settings_retention_days_check;
ALTER TABLE llm_settings ADD CONSTRAINT llm_settings_retention_days_check
  CHECK (retention_days IS NULL OR retention_days >= 0);

-- Hesap silme published_by yüzünden takılmasın.
ALTER TABLE legal_documents DROP CONSTRAINT IF EXISTS legal_documents_published_by_fkey;
ALTER TABLE legal_documents
  ADD CONSTRAINT legal_documents_published_by_fkey
  FOREIGN KEY (published_by) REFERENCES users(id) ON DELETE SET NULL;
