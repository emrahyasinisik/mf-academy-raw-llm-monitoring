-- 012_organizations.sql
-- Hesap / org modeli. org_id bu turda SORGU FİLTRESİ DEĞİL — kapsam hâlâ
-- user_id. Buraya bakıp "kapsam org bazlı" varsayan bir sorgu yazmak, bir
-- şirketin raporunu başka bir şirkete gösterir.

CREATE TABLE IF NOT EXISTS organizations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    type       TEXT NOT NULL DEFAULT 'individual'
               CHECK (type IN ('individual', 'company')),
    tax_id     TEXT NOT NULL DEFAULT '',
    seat_limit INTEGER NOT NULL DEFAULT 1,
    status     TEXT NOT NULL DEFAULT 'active'
               CHECK (status IN ('active', 'suspended')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);
ALTER TABLE users ADD COLUMN IF NOT EXISTS org_role TEXT NOT NULL DEFAULT 'owner';
ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT false;

-- Geri doldurma: org_id'si NULL her kullanıcıya kendi bireysel org'u.
-- İsimle JOIN etme — çakışmada yanlış satıra bağlanır. Döngü kullanıcı başına
-- org yaratır. org_id NOT NULL yapılmıyor: NULL görünür bir eksiklik kalsın.
DO $$
DECLARE
  r RECORD;
  new_org UUID;
BEGIN
  FOR r IN SELECT id, email, name FROM users WHERE org_id IS NULL LOOP
    INSERT INTO organizations (name, type, seat_limit)
    VALUES (COALESCE(NULLIF(r.name, ''), r.email), 'individual', 1)
    RETURNING id INTO new_org;
    UPDATE users SET org_id = new_org, org_role = 'owner', updated_at = now()
    WHERE id = r.id;
  END LOOP;
END $$;
