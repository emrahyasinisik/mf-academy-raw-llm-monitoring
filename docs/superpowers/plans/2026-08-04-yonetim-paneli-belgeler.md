# Yönetim Paneli — 4. Aşama (Belgeler) Implementation Plan

> **For agentic workers:** Use subagent-driven-development or execute task-by-task. Checkboxes track progress.

**Goal:** Hukuki metinleri (`gizlilik`, `kosullar`) DB’de append-only tutmak; public `GET /legal/{slug}`; admin editör/yayın; onay kapısını `TermsVersion` sabitinden versiyon karşılaştırmasına taşımak — spec §5, §8 madde 4.

**Architecture:** Migration `013_legal_documents.sql` tablo + seed. Store `admin` (veya `legal`) paketinde; public mount `/legal` auth yanında. `auth.TermsVersion` kalkar; register/accept ve kapı, yayınlanmış `kosullar` versiyonunu her seferinde DB’den okur. Frontend `RichText` ile public sayfalar + `/yonetim/belgeler` editör.

**Tech Stack:** Mevcut Go/Next — yeni bağımlılık yok.

**Spec:** [`docs/superpowers/specs/2026-08-04-yonetim-paneli-design.md`](../specs/2026-08-04-yonetim-paneli-design.md) §5, §8.4, §9 (publish, gate, public GET), §10 (backend önce).

## Global Constraints

- Yeni npm/Go bağımlılığı yok.
- Append-only publish; taslak aynı tabloda (`is_draft`).
- Seed olmadan deploy gizlilik sayfasını boşaltır — seed zorunlu.
- `TermsVersion` sabiti kalkar; önbellek yok.
- Audit yazımı 5. aşamada; bu planda audit_log yok.
- Arayüz Türkçe; dark-only; workshop dili yok.
- Frontend testleri yalnızca `src/lib/*.test.ts`.
- PR açma (kullanıcı talimatı) — local commit yeterli.

## File Structure

| Dosya | Sorumluluk |
|---|---|
| `mf-backend/migrations/013_legal_documents.sql` | Tablo + seed |
| `mf-backend/internal/admin/legal.go` | Admin handlers |
| `mf-backend/internal/admin/legal_store.go` | SQL + public GetPublished |
| `mf-backend/internal/admin/legal_test.go` | Fake-store handler tests |
| `mf-backend/internal/admin/routes.go` | `/admin/legal` |
| `mf-backend/internal/auth/*` | TermsVersion kaldır; RequiredVersion; AcceptTerms reconsent |
| `mf-backend/cmd/server/main.go` | `GET /legal/{slug}` public |
| `mf-frontend/src/lib/terms.ts` (+test) | Versiyon karşılaştırması |
| `mf-frontend/src/lib/adminNav.ts` (+test) | `belgeler` |
| `mf-frontend/src/lib/api.ts` / `types.ts` | Legal API |
| `mf-frontend/src/components/views/{Gizlilik,Kosullar}View.tsx` | Fetch + RichText |
| `mf-frontend/src/components/yonetim/LegalPanel.tsx` | Editör |
| `mf-frontend/src/app/yonetim/belgeler/page.tsx` | Sayfa |

---

### Task 1: Migration 013 + seed

**Files:** `mf-backend/migrations/013_legal_documents.sql`

- [x] Tablo + index (spec SQL)
- [x] Seed: `gizlilik` + `kosullar`, version `2026-08-01`, `is_draft=false`, `published_at=now()`, body = mevcut view metinlerinin Markdown hali
- [x] `go test ./migrations/` unique numbers

### Task 2: Legal store + public GET + admin CRUD

**Files:** `legal_store.go`, `legal.go`, `legal_test.go`, `routes.go`, `main.go`, Handler wire

- [x] Models: Document, publish req (`requires_reconsent`)
- [x] GetPublished(slug); List; GetSlug (history+draft); SaveDraft; Publish; DeleteDraft
- [x] Publish: reconsent → yeni `YYYY-MM-DD` (+suffix); değilse önceki version
- [x] Public `GET /legal/{slug}` — draft dönmez; 404 boş
- [x] Admin routes under `/admin/legal`
- [x] Tests: publish bump vs keep; public hides draft; empty DB safe

### Task 3: Auth kapısı TermsVersion → DB

**Files:** `auth/models.go`, `handler.go`, `store.go`, `handler_test.go`

- [x] `TermsVersion` const kaldır
- [x] Store/handler: `RequiredTermsVersion(ctx) (string, error)` — latest published `kosullar`
- [x] Register + AcceptTerms bu versiyonu yazar
- [x] AcceptTerms: reconsent için `terms_accepted_at IS NULL` kısıtı kalkar (version her kabulde güncellenir; ilk kabulde tarih set)
- [x] User JSON’a `terms_version` ekle
- [x] Tests güncelle

### Task 4: Frontend terms + public views

**Files:** `terms.ts`, `terms.test.ts`, types, Gizlilik/Kosullar views, api

- [x] `needsTermsGate(user, requiredVersion)`
- [x] Views: `GET /legal/{slug}` → RichText; yüklenirken skeleton; 404 notice
- [x] AppShell/OnayView wire

### Task 5: Admin Belgeler paneli

**Files:** adminNav, LegalPanel, page, api

- [x] Nav `belgeler`
- [x] Yan yana textarea + RichText önizleme
- [x] Kaydet / Yayınla (reconsent checkbox) / Taslağı at
- [x] `npm test` + `go test ./internal/admin/... ./internal/auth/...`

### Task 6: Closeout

- [x] Full focused suites green
- [x] Local commits (no PR)
- [x] Ledger note if using SDD

---

## Spec coverage

§5 migration/seed → T1; API → T2; onay kapısı → T3–T4; editör → T5. Audit/retention → aşama 5, burada yok.
