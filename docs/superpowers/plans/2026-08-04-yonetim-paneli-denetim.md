# Yönetim Paneli — 5. Aşama (Denetim ve saklama) Implementation Plan

**Goal:** `audit_log`, panelde denetim listesi, saklama süresi DB’den, hesap silme — spec §6–§7, §8 madde 5.

**Base:** `feat/yonetim-belgeler` (013 + legal). Charts ayrı branch’te kalır.

**Constraints:** PR yok; kişisel veri audit detail’de yok; `RETENTION_DAYS` yalnız ilk seed.

## Tasks

### Task 1: Migration 014
- `audit_log` tablosu + index
- `llm_settings.retention_days INTEGER` (NULL = henüz seed edilmedi)

### Task 2: Audit writer + list API
- `admin/audit_store.go`, handlers `GET /admin/audit`
- Yazma: account.create/suspend/unsuspend/delete, legal.publish, settings.update, adapter.activate/deactivate
- Tests: create + publish each write a row

### Task 3: Retention from settings
- Settings + Patch `retention_days`
- Boot: NULL ise env ile doldur; sweep DB değerini kullanır
- Model panel veya ayarlar UI’da saklama günü

### Task 4: Account delete
- `DELETE /admin/accounts/{id}` — org + üyeler (CASCADE path)
- AccountsPanel onaylı silme
- Audit `account.delete`

### Task 5: Denetim UI
- adminNav `denetim`, `/yonetim/denetim`, AuditPanel liste

### Task 6: Verify + commit
- go test admin/auth/settings; npm test; local commits only
