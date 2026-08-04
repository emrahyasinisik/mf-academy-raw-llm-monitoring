-- 015_org_role_check.sql
-- 012 org_role'ü serbest TEXT bıraktı. Şirket paneli rol değiştirince
-- geçersiz değer yazılmasını DB'de de kilitlemek için CHECK eklenir.
-- Mevcut satırlar yalnızca owner/admin/member olmalı (CreateCompany + register).

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'users_org_role_check'
  ) THEN
    ALTER TABLE users
      ADD CONSTRAINT users_org_role_check
      CHECK (org_role IN ('owner', 'admin', 'member'));
  END IF;
END $$;
