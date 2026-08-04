# Yönetim Paneli — 2. Aşama (Hesaplar) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Organizasyon modeli, admin hesap açma (tek seferlik geçici parola), sunucu tarafında zorunlu parola yenileme, askıya alma, ve `/yonetim/hesaplar` paneli — özellik seti spec §3; hesap silme 5. aşamada.

**Architecture:** Migration `012_organizations.sql` org + `users.org_id` / `must_change_password` ekler. JWT `pwd_reset` claim'i + `RequirePasswordFresh` ürün alt ağaçlarını kilitler (`/auth` açık kalır). Admin CRUD `/admin/accounts` altında; geçici parola yalnızca `POST` yanıtında bir kez döner. Frontend `adminNav`'a `hesaplar` ekler, liste/detay/oluştur UI'sı ve ürün tarafında terms kapısına benzer parola değiştirme kapısı koyar.

**Tech Stack:** Go 1.26 (chi, pgx, bcrypt, jwt), Next.js 16 / React 19 / TypeScript, mevcut `node --test` (yalnızca `src/lib/*.test.ts`). Yeni bağımlılık yok.

**Spec:** [`docs/superpowers/specs/2026-08-04-yonetim-paneli-design.md`](../specs/2026-08-04-yonetim-paneli-design.md) §3, §8 madde 2, §9 (hesaplar + `pwd_reset` + askıya alma testleri), §10 (backend önce).

## Global Constraints

- **Yeni npm / Go bağımlılığı yok.** Mevcut stack yeterli.
- **Arayüz metinleri Türkçe.** Atölye dili yasak (GPU, tünel, kart, eğitim koşusu). Positioning: "AI karar verir" / "en iyi model" / "otomatik" yok.
- **Dark-only;** renkler CSS custom property'lerden.
- **`org_id` bu turda sorgu filtresi değil.** Kapsam hâlâ `user_id`. Org durumu yalnızca askıya alma / giriş reddinde okunur (spec §3).
- **Kullanıcı taklidi yok.** Detayda vaka metni / bulgu yok — yalnızca metadata.
- **Hesap silme bu planda yok** — spec §8 madde 5. Detayda sil butonu konmaz.
- **Frontend testleri** yalnızca `src/lib/*.test.ts`, uzantılı import.
- **Backend testleri** sahte store + `httptest`; `go test ./...`.
- **Commit mesajları:** WHY ilk satır ≤72; gövde açıklar; son satır `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- **Deploy:** Backend önce (migration + uçlar), sonra frontend. Push yarım sürümdür — Render elle tetiklenir.

---

## File Structure

| Dosya | Sorumluluk |
|---|---|
| `mf-backend/migrations/012_organizations.sql` | **Yeni.** `organizations` + `users` sütunları + geri doldurma. |
| `mf-backend/internal/common/middleware.go` | **Değişir.** `AuthClaims.PasswordReset`; `RequirePasswordFresh`. |
| `mf-backend/internal/common/errors.go` | **Değişir.** `ErrPasswordChangeRequired()`. |
| `mf-backend/internal/auth/jwt.go` | **Değişir.** `pwd_reset` claim üret/oku. |
| `mf-backend/internal/auth/models.go` | **Değişir.** `User.MustChangePassword`. |
| `mf-backend/internal/auth/store.go` | **Değişir.** Flag oku/yaz; org ile kullanıcı yarat; askılı org kontrolü; oturum iptali. |
| `mf-backend/internal/auth/handler.go` | **Değişir.** Login askı reddi; ChangePassword bayrağı düşürür; Register org yaratır. |
| `mf-backend/internal/auth/handler_test.go` | **Değişir.** `pwd_reset` + askı + register org testleri. |
| `mf-backend/internal/admin/accounts.go` | **Yeni.** Liste / oluştur / detay / askıya al handler + request modelleri. |
| `mf-backend/internal/admin/accounts_store.go` | **Yeni.** Org SQL (transaction'lı şirket yaratma). |
| `mf-backend/internal/admin/accounts_test.go` | **Yeni.** Handler testleri (fake store). |
| `mf-backend/internal/admin/routes.go` | **Değişir.** `/accounts` rotaları. |
| `mf-backend/internal/{llm,analysis,wiki,decision,mcp}/*routes*` | **Değişir.** `RequireAuth` sonrası `RequirePasswordFresh`. |
| `mf-backend/cmd/server/main.go` | **Değişir.** `/mcp-servers` client rotasına aynı middleware. |
| `mf-frontend/src/lib/adminNav.ts` (+ test) | **Değişir.** `hesaplar` bölümü. |
| `mf-frontend/src/lib/passwordGate.ts` (+ test) | **Yeni.** `password_change_required` / kullanıcı bayrağı → kapı. |
| `mf-frontend/src/lib/api.ts` / `types.ts` | **Değişir.** Accounts API + `changePassword`; User alanları. |
| `mf-frontend/src/store/auth.tsx` | **Değişir.** `changePassword`; User tipine bayrak. |
| `mf-frontend/src/components/views/ParolaView.tsx` | **Yeni.** Zorunlu parola değiştirme ekranı. |
| `mf-frontend/src/components/AppShell.tsx` | **Değişir.** Terms'ten önce/sonra parola kapısı. |
| `mf-frontend/src/components/yonetim/AccountsPanel.tsx` | **Yeni.** Liste + oluştur + detay. |
| `mf-frontend/src/app/yonetim/hesaplar/page.tsx` | **Yeni.** İnce sayfa sarmalayıcı. |

---

### Task 1: Migration 012

**Files:**
- Create: `mf-backend/migrations/012_organizations.sql`
- Modify: `mf-backend/migrations/migrations_test.go` (numara çakışması zaten var; 012 dosyası eklenince otomatik kapsanır — ek assertion: `012_organizations.sql` embed içinde)

**Interfaces:**
- Consumes: yok.
- Produces: `organizations` tablosu; `users.org_id`, `users.org_role`, `users.must_change_password`; mevcut kullanıcılar için bireysel org geri doldurma.

- [x] **Step 1: Migration dosyasını yaz**

`mf-backend/migrations/012_organizations.sql` — spec §3 SQL'si, geri doldurma dahil:

```sql
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
```

- [x] **Step 2: Test — migration numarası benzersiz**

```bash
cd mf-backend && go test ./migrations/ -run TestMigrationNumbersAreUnique -v
```

Expected: PASS; `012` listede.

- [x] **Step 3: Commit**

```bash
git add mf-backend/migrations/012_organizations.sql
git commit -m "$(cat <<'EOF'
Give every account an organization before the panel needs one

Without a row per user the accounts surface has nothing to list, and
backfilling later under a NOT NULL constraint would hide incomplete rows.

org_id is not a query filter in this phase — scope stays user_id.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: `pwd_reset` claim + `RequirePasswordFresh`

**Files:**
- Modify: `mf-backend/internal/common/middleware.go`, `errors.go`
- Modify: `mf-backend/internal/auth/jwt.go`, `models.go`, `store.go`, `handler.go`, `handler_test.go`
- Modify: `mf-backend/internal/llm/routes.go`, `analysis/routes.go`, `wiki/handler.go` (Routes), `decision/handler.go` (Routes), `mcp/routes.go`
- Modify: `mf-backend/cmd/server/main.go` (`/mcp-servers` satırı)

**Interfaces:**
- Consumes: `User` store satırından `must_change_password`.
- Produces:
  - `common.AuthClaims.PasswordReset bool`
  - `common.RequirePasswordFresh` → 403 + code `password_change_required`
  - `common.ErrPasswordChangeRequired()`
  - JWT claim `pwd_reset`
  - `User.MustChangePassword bool \`json:"must_change_password"\``
  - `UpdatePassword` bayrağı `false` yapar
  - Login/Refresh/Register token'ları bayrağı taşır

- [x] **Step 1: Failing tests in `handler_test.go`**

`fakeStore`'a `mustChangePassword map[string]bool` ekle. Testler:

```go
func TestPasswordResetClaimBlocksProductButNotAuth(t *testing.T) {
	// 1) GenerateAccess with MustChangePassword true → Verify → PasswordReset true
	// 2) RequirePasswordFresh on a stub handler → 403, code password_change_required
	// 3) ChangePassword clears flag; next GenerateAccess has PasswordReset false
}

func TestRefreshPreservesPasswordResetFlag(t *testing.T) {
	// User with must_change_password true; Refresh → access token still has pwd_reset
}
```

Also add a small `common` test file OR test RequirePasswordFresh via httptest in auth tests:

```go
func TestRequirePasswordFresh(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := common.RequirePasswordFresh(next)

	// PasswordReset true → 403
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(common.ContextWithClaims(r.Context(), common.AuthClaims{
		UserID: "u1", PasswordReset: true,
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 { t.Fatalf(...) }
	// body code == password_change_required

	// PasswordReset false → 204
}
```

- [x] **Step 2: Run — expect FAIL**

```bash
cd mf-backend && go test ./internal/common/ ./internal/auth/ -count=1
```

- [x] **Step 3: Implement**

`errors.go`:

```go
func ErrPasswordChangeRequired() *APIError {
	return &APIError{
		Status:  http.StatusForbidden,
		Code:    "password_change_required",
		Message: "password change required",
	}
}
```

`AuthClaims` + middleware:

```go
type AuthClaims struct {
	UserID         string
	Email          string
	Role           string
	PasswordReset  bool
}

func RequirePasswordFresh(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok {
			Error(w, ErrUnauthorized("authentication required"))
			return
		}
		if claims.PasswordReset {
			Error(w, ErrPasswordChangeRequired())
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

`accessClaims`:

```go
type accessClaims struct {
	Email    string `json:"email"`
	Role     string `json:"role"`
	PwdReset bool   `json:"pwd_reset,omitempty"`
	jwt.RegisteredClaims
}
```

`GenerateAccess` / `Verify` map `u.MustChangePassword` ↔ `PwdReset` ↔ `AuthClaims.PasswordReset`.

`User` struct: `MustChangePassword bool \`json:"must_change_password"\``.

Store: tüm `SELECT` listelerine `must_change_password` ekle; `UpdatePassword`:

```sql
UPDATE users SET password_hash = $2, must_change_password = false, updated_at = now()
WHERE id = $1
```

Route wiring — **her** `RequireAuth(verify)` ürün grubunun hemen ardından:

```go
pr.Use(common.RequireAuth(verify))
pr.Use(common.RequirePasswordFresh)
```

Dosyalar: `llm/routes.go`, `analysis/routes.go`, `wiki` Routes, `decision` Routes, `mcp/routes.go`, ve `main.go`:

```go
pr.With(common.RequireAuth(tokens.Verify), common.RequirePasswordFresh).Get("/mcp-servers", ...)
```

**`/admin` ve `/auth` bu middleware'i almaz** (spec: ürün alt ağaçları; `/auth` açık kalmalı yoksa ChangePassword kilitlenir).

- [x] **Step 4: Run — expect PASS**

```bash
cd mf-backend && go test ./internal/common/ ./internal/auth/ ./internal/llm/ ./internal/analysis/ ./internal/mcp/ -count=1
```

- [x] **Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
Force a password change before a temp password becomes a back door

An admin who creates an account knows the temporary password. Without a
server-side gate they could sign in as that user; pwd_reset on the access
token closes the product surface until ChangePassword clears the flag.
Refresh must re-read the row or one rotation would mint a clean token.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Askıya alma girişte + Register bireysel org

**Files:**
- Modify: `mf-backend/internal/auth/store.go`, `handler.go`, `handler_test.go`

**Interfaces:**
- Consumes: `organizations.status`, `users.org_id`.
- Produces:
  - `Login` askılı org üyesini reddeder (aynı "invalid email or password" mesajı — hesap varlığını sızdırma).
  - `Register` / `CreateUser` transaction içinde `individual` org + `org_id` yazar.
  - `IsOrgSuspended(ctx, userID) (bool, error)` veya login sorgusunda JOIN.

- [x] **Step 1: Failing tests**

```go
func TestLoginRejectsSuspendedOrgMember(t *testing.T) { ... }
func TestRegisterCreatesIndividualOrg(t *testing.T) { ... }
```

- [x] **Step 2: Implement**

Login path — `GetUserByEmailWithHash` sonrası veya sorguya JOIN:

```sql
SELECT u.id, u.email, ..., u.must_change_password, u.password_hash,
       COALESCE(o.status, 'active') AS org_status
FROM users u
LEFT JOIN organizations o ON o.id = u.org_id
WHERE u.email = $1
```

`org_status == "suspended"` → bcrypt decoy yolundan geçtikten sonra yine `invalid email or password` (timing korunur).

`CreateUser`: BEGIN → INSERT organization → INSERT user with org_id, must_change_password=false → COMMIT.

- [x] **Step 3: Test + commit**

```bash
cd mf-backend && go test ./internal/auth/ -count=1
```

```bash
git commit -m "$(cat <<'EOF'
Refuse login when the organization is suspended, and give registrants an org

Suspend only works if every member is locked out at the door. Self-serve
registration must create the same individual org the backfill did, or new
users land with a NULL org_id the panel cannot list.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Admin accounts API

**Files:**
- Create: `mf-backend/internal/admin/accounts.go`, `accounts_store.go`, `accounts_test.go`
- Modify: `mf-backend/internal/admin/routes.go`, `handler.go` (store interface genişletmesi)

**Interfaces:**
- Consumes: admin `RequireAuth`+`RequireRole`; bcrypt cost from handler (auth ile aynı cost — `Handler`'a cost enjekte et veya `admin` hesap yaratırken `auth` paketindeki cost'u `main` üzerinden geçir).
- Produces endpoints (timeout grubu `localRoutes` içinde):

```
GET    /admin/accounts?q=&type=&status=&page=1&limit=20
POST   /admin/accounts
GET    /admin/accounts/{id}
POST   /admin/accounts/{id}/suspend
POST   /admin/accounts/{id}/unsuspend
```

**Request `POST /admin/accounts`:**

```go
type CreateAccountRequest struct {
	Type      string `json:"type"` // individual | company
	Name      string `json:"name"` // bireysel: kişi adı; şirket: unvan
	Email     string `json:"email"`
	TaxID     string `json:"tax_id"`
	SeatLimit int    `json:"seat_limit"`
	// Şirkette sahip = Name+Email; bireyselde tek üye.
}
```

**Response create (password yalnızca burada):**

```go
type CreateAccountResponse struct {
	Account            AccountSummary `json:"account"`
	TemporaryPassword  string         `json:"temporary_password"`
	Owner              AccountMember  `json:"owner"`
}
```

**AccountSummary** (liste/detay): `id`, `name`, `type`, `tax_id`, `seat_limit`, `status`, `member_count`, `assessment_count`, `last_activity_at`, `created_at`.

**Detay ekleri:** `members[]` (id, email, name, org_role, created_at — **vaka metni yok**), `sessions[]` (aktif oturum metadata: id, user_agent, ip, created_at, expires_at).

**Geçici parola:** 16 byte `crypto/rand` → base64.RawURLEncoding; bcrypt hash store; `must_change_password=true`; düz metin yalnızca response.

**Şirket yaratma:** tek transaction — org + owner user; biri fail → ikisi de yok. Duplicate email → 409.

**Suspend:** `organizations.status='suspended'`; org üyelerinin tüm session'ları revoke (`RevokeAllSessionsForUser` her üye için). Unsuspend: `status='active'` (oturumlar geri gelmez — yeniden giriş).

- [x] **Step 1: Write failing handler tests with fake `AccountStore` interface**

```go
type AccountStore interface {
	ListAccounts(ctx context.Context, q AccountListQuery) (AccountListResult, error)
	CreateIndividual(ctx context.Context, name, email, hash string) (AccountSummary, AccountMember, error)
	CreateCompany(ctx context.Context, orgName, taxID string, seats int, ownerName, ownerEmail, hash string) (AccountSummary, AccountMember, error)
	GetAccount(ctx context.Context, id string) (AccountDetail, error)
	SetAccountStatus(ctx context.Context, id, status string) error
	ListMemberIDs(ctx context.Context, orgID string) ([]string, error)
}
```

Tests from spec §9:
- şirket create: org+owner aynı tx (fake'te "fail after org" simülasyonu)
- temporary password response'ta var, store'a hash gidiyor
- suspend calls revoke for each member

- [x] **Step 2: Implement store + handlers; wire routes; inject bcrypt cost**

`main.go` / `NewHandler`: accounts store = aynı `admin.Store` (pool). Bcrypt cost: `cfg.BcryptCost` — `admin.Handler`'a `bcryptCost int` alanı ekle (veya `CreateAccount` içinde cost parametresi).

`localRoutes`:

```go
r.Route("/accounts", func(ar chi.Router) {
	ar.Get("/", h.ListAccounts)
	ar.Post("/", h.CreateAccount)
	ar.Get("/{id}", h.GetAccount)
	ar.Post("/{id}/suspend", h.SuspendAccount)
	ar.Post("/{id}/unsuspend", h.UnsuspendAccount)
})
```

- [x] **Step 3: `go test ./internal/admin/ -count=1` PASS**

- [x] **Step 4: Commit**

```bash
git commit -m "$(cat <<'EOF'
Let operators open accounts without an email pipeline

There is no mailer on .vercel.app, so the temporary password has to ride
the create response once. Company rows and their owner share one
transaction so a half-created firm cannot appear in the list.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Frontend nav + API client + passwordGate

**Files:**
- Modify: `mf-frontend/src/lib/adminNav.ts`, `adminNav.test.ts`
- Create: `mf-frontend/src/lib/passwordGate.ts`, `passwordGate.test.ts`
- Modify: `mf-frontend/src/lib/types.ts`, `api.ts`

**Interfaces:**
- Produces: `PanelSection` includes `"hesaplar"`; path `/yonetim/hesaplar`
- `needsPasswordGate(user)` / `isPasswordChangeRequired(err: ApiError)`
- `api.changePassword`, `api.admin.accounts.*`

- [x] **Step 1: Failing adminNav + passwordGate tests**

```typescript
test("hesaplar yolu kendi bölümüne çözülür", () => {
  assert.equal(sectionFromPath("/yonetim/hesaplar"), "hesaplar");
});

test("password_change_required kodu kapıyı açar", () => {
  assert.equal(
    isPasswordChangeRequired({ status: 403, code: "password_change_required", message: "x" }),
    true,
  );
});

test("must_change_password bayraklı kullanıcı kapıya düşer", () => {
  assert.equal(needsPasswordGate({ must_change_password: true }), true);
  assert.equal(needsPasswordGate({ must_change_password: false }), false);
  assert.equal(needsPasswordGate(null), false);
});
```

- [x] **Step 2: Implement nav + gate + types + api**

`PANEL_SECTIONS` — Genel'den hemen sonra:

```typescript
{ id: "hesaplar", label: "Hesaplar", path: "/yonetim/hesaplar" },
```

`User` type: `must_change_password: boolean`.

`api.ts`:

```typescript
changePassword: (current_password: string, new_password: string) =>
  request<TokenPair>("/auth/change-password", {
    method: "POST",
    body: JSON.stringify({ current_password, new_password }),
  }),
// admin.accounts: list, create, get, suspend, unsuspend
```

- [x] **Step 3: `npm test` PASS; commit**

```bash
git commit -m "$(cat <<'EOF'
Point the panel at accounts and teach the client the password gate code

A section that is not in adminNav never appears in the shell. The gate
decision stays pure TypeScript so the test runner can see it.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Parola kapısı UI + auth store

**Files:**
- Create: `mf-frontend/src/components/views/ParolaView.tsx`
- Modify: `mf-frontend/src/store/auth.tsx`, `AppShell.tsx`
- Modify: `mf-frontend/src/lib/api.ts` request path — 403 `password_change_required` sonrası global sinyal **zorunlu değil**; AppShell `user.must_change_password` ile kapıyı açar. Token yenileme sonrası `/auth/me` bayrağı taşır.

**Interfaces:**
- `useAuth().changePassword(current, next)` → token pair sakla, user güncelle
- AppShell: `needsPasswordGate(user)` → `<ParolaView />` (terms kapısından **önce** — yoksa terms kabulü ürünü açmadan parola zorunluluğunu atlar… Aslında terms de ürün. Sıra: booting → auth → **password gate** → terms gate → app. Password reset'li kullanıcı terms'i zaten kabul etmiş olabilir (admin create'de terms_version set et).

**Admin create user terms:** CreateAccount owner'a `terms_accepted_at = now()`, `terms_version = auth.TermsVersion` yaz — aksi halde parola değişince terms kapısına düşerler. Plan bunu Task 4 store insert'ine ekler.

- [x] **Step 1: ParolaView** — e-posta readonly, mevcut (geçici) parola, yeni parola, Türkçe metin: "İlk girişte parolanızı değiştirmeniz gerekiyor." Kayıt linki yok.

- [x] **Step 2: AppShell wiring**

```tsx
if (needsPasswordGate(user)) return <ParolaView />;
if (needsTermsGate(user)) return <OnayView />;
```

- [x] **Step 3: lint/test/build; commit**

```bash
git commit -m "$(cat <<'EOF'
Show a password door when the server says the temporary one still works

The gate is enforced on the API; this screen only matches it so the user
is not staring at a product that answers 403 on every click.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: AccountsPanel UI

**Files:**
- Create: `mf-frontend/src/components/yonetim/AccountsPanel.tsx`
- Create: `mf-frontend/src/app/yonetim/hesaplar/page.tsx`

**Interfaces:**
- Consumes: `api.admin.accounts.*`
- Liste: arama, tür/durum filtresi, sayfalama; sütunlar spec §3.
- Yeni hesap formu: bireysel | şirket; başarıda `temporary_password` bir kez göster (kopyala); uyarı: "Bu parola bir daha gösterilmeyecek."
- Detay: üyeler, metadata sayıları, oturumlar, Askıya al / Askıyı kaldır. **Sil yok. Vaka metni yok.**

Dark-only, CSS variables, Türkçe, workshop copy yok.

- [x] **Step 1: page.tsx**

```tsx
import { AccountsPanel } from "@/components/yonetim/AccountsPanel";
export default function YonetimHesaplar() {
  return <AccountsPanel />;
}
```

- [x] **Step 2: AccountsPanel** — mevcut panel dilini izle (`OverviewPanel` kart / tablo sınıfları: `card`, `btn`, `input`, `label`, `notice`).

- [x] **Step 3: `npm run lint && npm test && npm run build`** — build çıktısında `/yonetim/hesaplar` görünmeli.

- [x] **Step 4: Commit**

```bash
git commit -m "$(cat <<'EOF'
Give operators an accounts desk that shows metadata, not case contents

Listing and suspending firms is the job; opening a user's analyses would
contradict the data-sovereignty pitch, so the detail view stops at counts.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Aşamayı kapat

**Files:**
- Modify: this plan — checkboxes `[x]`

- [x] **Step 1: Full verification**

```bash
cd mf-backend && go test ./...
cd mf-frontend && npm test && npm run lint && npm run build
```

- [x] **Step 2: Backend diff is intentional; frontend+backend both change**

```bash
git diff main --stat
```

- [x] **Step 3: Grep guards**

```bash
# org_id must not appear as a data-scope filter in analysis/llm queries this phase
rg "org_id" mf-backend/internal/analysis mf-backend/internal/llm mf-backend/internal/decision || true
# impersonation / "taklit" yok
rg -i "impersonat|taklit|act.?as" mf-frontend/src/components/yonetim || true
```

Expected: `org_id` yalnızca auth/admin accounts yollarında; analysis/llm/decision'da filtre yok.

- [x] **Step 4: Deploy note (rapora yaz)**

1. Render'da backend deploy'u elle tetikle; migration 012 uygulandığını doğrula.
2. Sonra Vercel frontend.
3. Smoke: admin `/yonetim/hesaplar` → hesap aç → temp parola bir kez → o kullanıcıyla giriş → ParolaView → ürün açılır; askıya alınmış org giriş yapamaz.

- [x] **Step 5: Plan kutucuklarını işaretle + commit**

```bash
git add docs/superpowers/plans/2026-08-04-yonetim-paneli-hesaplar.md
git commit -m "$(cat <<'EOF'
Close out phase two: accounts without pretending we have mail

Operators can open and suspend organizations; temporary passwords die at
first change; case contents never cross into the panel.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

## Spec coverage (self-review)

| Spec §3 / §9 gereksinimi | Task |
|---|---|
| Migration 012 + backfill | 1 |
| `pwd_reset` + RequirePasswordFresh + refresh | 2 |
| ChangePassword bayrağı düşürür | 2 |
| Askıya alma → giriş reddi + session revoke | 3 + 4 |
| Register bireysel org | 3 |
| Liste / create / detail API | 4 |
| Temp password bir kez | 4 |
| Şirket tx atomicity | 4 |
| Kullanıcı taklidi yok / içerik yok | 4 + 7 |
| Hesap silme | **bilinçli dışarıda** (§8.5) |
| Frontend nav + panel | 5 + 7 |
| Parola kapısı UI | 5 + 6 |
| Backend-before-frontend deploy | 8 |

## Out of scope (do not implement)

- Chart'lar / `GET /admin/stats` (aşama 3)
- Belgeler / KVKK editör (aşama 4)
- Denetim kaydı yazma yüzeyi + hesap silme (aşama 5) — create/suspend için audit write da 5'te; bu planda audit insert yok
- Çok kiracılı `org_id` sorgu filtresi
- E-posta gönderimi
---
