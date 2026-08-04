# Şirket Paneli Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Şirket owner/admin'ine kendi org'unu yönetecek ayrı bir yüzey vermek — `/sirket` kabuğu + `/org/*` API; ekip (geçici parola), kullanım ve aktivite metadata'sı; platform `/yonetim` ve `/admin` dokunulmaz.

**Architecture:** JWT ve `/auth/me` org claim'leri taşır; `RequireOrgAdmin` claims üzerinden `company` + `owner|admin` + `active` kapısını kilitler. Yeni paket `mf-backend/internal/org` (admin altına gömülmez). Frontend `OrgGate` / `OrgShell` yönetim panelinin görsel dilini paylaşır ama ayrı bileşen / ayrı nav. Handler'lar path/body'den `org_id` almaz — kapsam = `claims.OrgID`.

**Tech Stack:** Go 1.26 (chi, pgx, bcrypt, jwt), Next.js 16 / React 19 / TypeScript, mevcut `node --test` (yalnızca `src/lib/*.test.ts`). Yeni bağımlılık yok.

**Spec:** [`docs/superpowers/specs/2026-08-04-sirket-paneli-design.md`](../specs/2026-08-04-sirket-paneli-design.md)

**Worktree / branch:** `.worktrees/sirket-paneli` — plan commit'inden sonra `feat/sirket-paneli` (docs dalından). Base: `origin/main` + onaylı spec.

## Global Constraints

- **Org admin `/admin/*` veya `/yonetim` görmez / çağırmaz.** Ayrı paket, ayrı gate.
- **Kapsam = actor `org_id` only.** Path'te `/org/{orgId}/...` yok. Body'de `org_id` alanı yok.
- **Vaka metni / rapor gövdesi / transcript / kriter skoru yok** — yalnızca metadata.
- **Kota / soft-cap yok.** `seat_limit` yalnızca üye eklerken tavan; şirket panelinden yükseltilmez.
- **E-posta daveti yok.** Üye = geçici parola, yanıtta bir kez.
- **Şirket yaratma yok** — yalnızca platform `/yonetim/hesaplar`.
- **Individual org'lar `/sirket` görmez.** OrgGate tip + rol + status kontrol eder.
- **Owner dokunulmaz:** rol değiştirme / çıkarma → 400. Owner oluşturma yok.
- **Cross-org üye UUID → 404** (varlığı sızdırma).
- **Prometheus yok** şirket panelinde — Postgres only.
- **Yeni npm / Go bağımlılığı yok.**
- **UI metinleri Türkçe.** Atölye dili yasak. Dark-only; CSS token'lar.
- **Frontend testleri** yalnızca `src/lib/*.test.ts`, uzantılı import.
- **Backend testleri** fake store + httptest; `go test ./...`.
- **Commit:** WHY ≤72; gövde; `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- **Deploy:** Backend önce (claim + `/org/*`), sonra frontend. Push yarım sürümdür.

---

## File Structure

| Dosya | Sorumluluk |
|---|---|
| `mf-backend/migrations/015_org_role_check.sql` | **Yeni.** `users.org_role` CHECK (`owner\|admin\|member`). |
| `mf-backend/internal/common/middleware.go` (+ test) | **Değişir.** `AuthClaims` org alanları; `RequireOrgAdmin`. |
| `mf-backend/internal/auth/jwt.go` | **Değişir.** JWT `org_id` / `org_role` / `org_type`. |
| `mf-backend/internal/auth/models.go` | **Değişir.** `User` org JSON alanları. |
| `mf-backend/internal/auth/store.go` | **Değişir.** SELECT/JOIN org alanları; refresh için güncel satır. |
| `mf-backend/internal/auth/handler.go` (+ test) | **Değişir.** Me/login/refresh org alanlarını döner; token üretimi. |
| `mf-backend/internal/org/models.go` | **Yeni.** Org me / member / stats / activity DTO'ları. |
| `mf-backend/internal/org/store.go` | **Yeni.** Postgres: org özeti, üyeler, stats, activity. |
| `mf-backend/internal/org/handler.go` | **Yeni.** HTTP handlers. |
| `mf-backend/internal/org/members.go` | **Yeni.** List/Create/SetRole/Remove + temp password. |
| `mf-backend/internal/org/stats.go` | **Yeni.** Org-scoped stats. |
| `mf-backend/internal/org/activity.go` | **Yeni.** Metadata activity feed. |
| `mf-backend/internal/org/routes.go` | **Yeni.** Mount: Auth + PasswordFresh + OrgAdmin + Timeout. |
| `mf-backend/internal/org/handler_test.go` | **Yeni.** Fake store authz + seat + owner koruması. |
| `mf-backend/internal/org/audit.go` | **Yeni.** AuditWriter arayüzü (admin.WriteAudit uyumlu). |
| `mf-backend/cmd/server/main.go` | **Değişir.** `Mount("/org", ...)`. |
| `mf-frontend/src/lib/orgAccess.ts` (+ test) | **Yeni.** OrgGate saf karar. |
| `mf-frontend/src/lib/orgNav.ts` (+ test) | **Yeni.** `/sirket` rota tablosu. |
| `mf-frontend/src/lib/types.ts` / `api.ts` | **Değişir.** User org alanları; `api.org.*`. |
| `mf-frontend/src/components/sirket/*` | **Yeni.** OrgShell, OrgGate, OrgLogin, Overview/Team/Usage/Activity panelleri. |
| `mf-frontend/src/app/sirket/**` | **Yeni.** layout + sayfalar. |
| `mf-frontend/src/components/AppShell.tsx` | **Değişir.** OrgGate geçen kullanıcıya `/sirket` linki. |

---

## Phase 1 — Identity (claims, gate, Me)

### Task 1: Migration 015 — `org_role` CHECK

**Files:**
- Create: `mf-backend/migrations/015_org_role_check.sql`
- Test: `mf-backend/migrations/migrations_test.go` (embed uniqueness otomatik)

**Interfaces:**
- Consumes: 012 `users.org_role`
- Produces: CHECK constraint `org_role IN ('owner','admin','member')`

- [ ] **Step 1: Write migration**

```sql
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
```

- [ ] **Step 2: Run migration uniqueness test**

```bash
cd mf-backend && go test ./migrations/ -run TestMigrationNumbersAreUnique -v
```

Expected: PASS; `015` listede.

- [ ] **Step 3: Commit**

```bash
git add mf-backend/migrations/015_org_role_check.sql
git commit -m "$(cat <<'EOF'
Lock org_role to owner|admin|member before the company panel writes it

Without a CHECK the panel could persist a typo and RequireOrgAdmin would
silently deny forever with no row-level explanation.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: AuthClaims + JWT org fields + RequireOrgAdmin

**Files:**
- Modify: `mf-backend/internal/common/middleware.go`
- Modify: `mf-backend/internal/common/middleware_test.go`
- Modify: `mf-backend/internal/auth/jwt.go`
- Test: extend `middleware_test.go`; JWT covered via auth handler tests in Task 3

**Interfaces:**
- Consumes: `User` with OrgID/OrgRole/OrgType (Task 3 fills store)
- Produces:
  - `AuthClaims{OrgID, OrgRole, OrgType string}` (empty string OK for legacy NULL)
  - JWT claims `org_id`, `org_role`, `org_type`
  - `RequireOrgAdmin` → 403 unless `OrgID != ""` ∧ `OrgType == "company"` ∧ `OrgRole ∈ {owner,admin}`

- [ ] **Step 1: Failing tests**

`middleware_test.go` ekle:

```go
func TestRequireOrgAdmin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := RequireOrgAdmin(next)

	cases := []struct {
		name   string
		claims AuthClaims
		code   int
	}{
		{"company owner", AuthClaims{UserID: "u", OrgID: "o1", OrgRole: "owner", OrgType: "company"}, 204},
		{"company admin", AuthClaims{UserID: "u", OrgID: "o1", OrgRole: "admin", OrgType: "company"}, 204},
		{"member", AuthClaims{UserID: "u", OrgID: "o1", OrgRole: "member", OrgType: "company"}, 403},
		{"individual owner", AuthClaims{UserID: "u", OrgID: "o1", OrgRole: "owner", OrgType: "individual"}, 403},
		{"no org", AuthClaims{UserID: "u"}, 403},
		{"platform admin alone", AuthClaims{UserID: "u", Role: RoleAdmin}, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/org/me", nil)
			r = r.WithContext(ContextWithClaims(r.Context(), tc.claims))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatalf("got %d want %d", w.Code, tc.code)
			}
		})
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
cd mf-backend && go test ./internal/common/ -run TestRequireOrgAdmin -count=1
```

Expected: FAIL — `RequireOrgAdmin` undefined.

- [ ] **Step 3: Implement**

`AuthClaims` genişlet:

```go
type AuthClaims struct {
	UserID        string
	Email         string
	Role          string
	PasswordReset bool
	OrgID         string
	OrgRole       string
	OrgType       string
}
```

```go
// RequireOrgAdmin opens /org/* to company owner/admin only.
// Platform admin role alone is not enough — this is a customer surface.
func RequireOrgAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok {
			Error(w, ErrUnauthorized("authentication required"))
			return
		}
		if claims.OrgID == "" || claims.OrgType != "company" {
			Error(w, ErrForbidden("company admin access required"))
			return
		}
		if claims.OrgRole != "owner" && claims.OrgRole != "admin" {
			Error(w, ErrForbidden("company admin access required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

`accessClaims` + `GenerateAccess` + `Verify`:

```go
type accessClaims struct {
	Email    string `json:"email"`
	Role     string `json:"role"`
	PwdReset bool   `json:"pwd_reset,omitempty"`
	OrgID    string `json:"org_id,omitempty"`
	OrgRole  string `json:"org_role,omitempty"`
	OrgType  string `json:"org_type,omitempty"`
	jwt.RegisteredClaims
}
```

`GenerateAccess` User'dan org alanlarını yazar; `Verify` AuthClaims'e kopyalar.

- [ ] **Step 4: Run — expect PASS**

```bash
cd mf-backend && go test ./internal/common/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add mf-backend/internal/common/middleware.go mf-backend/internal/common/middleware_test.go mf-backend/internal/auth/jwt.go
git commit -m "$(cat <<'EOF'
Put org identity on the access token so /org never guesses from the body

RequireOrgAdmin reads claims only — path/body org_id would make
cross-tenant probes a parsing bug away.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: User org fields on Me / login / refresh

**Files:**
- Modify: `mf-backend/internal/auth/models.go`, `store.go`, `handler.go`, `handler_test.go`

**Interfaces:**
- Consumes: JWT GenerateAccess (Task 2)
- Produces:
  - `User.OrgID *string \`json:"org_id"\`` (null if unset)
  - `User.OrgRole string \`json:"org_role"\``
  - `User.OrgType string \`json:"org_type"\`` (from organizations.type JOIN)
  - `User.OrgStatus` already exists (json:"-"); keep for login suspend
  - All SELECTs that build User for tokens must JOIN org and fill these
  - Refresh reloads row → fresh org_role/org_type in new access token

- [ ] **Step 1: Failing tests**

```go
func TestMeIncludesOrgClaims(t *testing.T) {
	// seed user with org_id, org_role=owner, org type=company
	// GET /auth/me → org_id, org_role, org_type present
}

func TestGenerateAccessEmbedsOrgFields(t *testing.T) {
	// GenerateAccess(User{OrgID, OrgRole, OrgType}) → Verify → AuthClaims match
}

func TestRefreshReadsFreshOrgRole(t *testing.T) {
	// user org_role changes in store between login and refresh
	// refresh access token carries new org_role
}
```

Fake store / real TokenService — mevcut `handler_test.go` desenini izle.

- [ ] **Step 2: Run — expect FAIL**

```bash
cd mf-backend && go test ./internal/auth/ -count=1
```

- [ ] **Step 3: Implement**

`models.go`:

```go
type User struct {
	// ...existing...
	OrgID    *string `json:"org_id"`
	OrgRole  string  `json:"org_role"`
	OrgType  string  `json:"org_type"`
	OrgStatus string `json:"-"`
}
```

Store sorgularına ekle:

```sql
u.org_id, COALESCE(u.org_role, ''), COALESCE(o.type, ''), COALESCE(o.status, 'active')
```

`GenerateAccess` User.OrgID/OrgRole/OrgType kullanır (`OrgID` nil → JWT'de boş).

- [ ] **Step 4: PASS + commit**

```bash
cd mf-backend && go test ./internal/auth/ ./internal/common/ -count=1
git add mf-backend/internal/auth/
git commit -m "$(cat <<'EOF'
Surface org_id/role/type on /auth/me so the company gate can decide

Frontend OrgGate cannot invent membership; Me and refresh must carry
the same fields the JWT already embeds.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Frontend `orgAccess` + User type

**Files:**
- Create: `mf-frontend/src/lib/orgAccess.ts`, `orgAccess.test.ts`
- Modify: `mf-frontend/src/lib/types.ts` (`User` org alanları)

**Interfaces:**
- Consumes: `User` with `org_id`, `org_role`, `org_type` (optional until backend deploy)
- Produces:
  - `type OrgGateState = "booting" | "login" | "redirect" | "allow"`
  - `function orgGate(input: { loading: boolean; user: OrgGateUser | null }): OrgGateState`
  - `function canAccessOrgPanel(user: OrgGateUser | null): boolean`

- [ ] **Step 1: Failing test**

```typescript
import { test } from "node:test";
import assert from "node:assert/strict";
import { orgGate, canAccessOrgPanel } from "./orgAccess.ts";

test("company owner/admin allow", () => {
  assert.equal(orgGate({ loading: false, user: { org_id: "o", org_role: "owner", org_type: "company" } }), "allow");
  assert.equal(orgGate({ loading: false, user: { org_id: "o", org_role: "admin", org_type: "company" } }), "allow");
});

test("member and individual redirect", () => {
  assert.equal(orgGate({ loading: false, user: { org_id: "o", org_role: "member", org_type: "company" } }), "redirect");
  assert.equal(orgGate({ loading: false, user: { org_id: "o", org_role: "owner", org_type: "individual" } }), "redirect");
});

test("loading then login", () => {
  assert.equal(orgGate({ loading: true, user: null }), "booting");
  assert.equal(orgGate({ loading: false, user: null }), "login");
});

test("platform admin alone cannot open sirket", () => {
  assert.equal(canAccessOrgPanel({ role: "admin", org_id: null, org_role: "owner", org_type: "individual" }), false);
});
```

- [ ] **Step 2: FAIL → implement → PASS**

```typescript
export type OrgGateState = "booting" | "login" | "redirect" | "allow";

export type OrgGateUser = {
  org_id?: string | null;
  org_role?: string | null;
  org_type?: string | null;
  role?: string;
};

export function canAccessOrgPanel(user: OrgGateUser | null): boolean {
  if (!user?.org_id) return false;
  if (user.org_type !== "company") return false;
  return user.org_role === "owner" || user.org_role === "admin";
}

export function orgGate(input: {
  loading: boolean;
  user: OrgGateUser | null;
}): OrgGateState {
  if (input.loading) return "booting";
  if (!input.user) return "login";
  return canAccessOrgPanel(input.user) ? "allow" : "redirect";
}
```

`types.ts` User:

```typescript
  org_id: string | null;
  org_role: string;
  org_type: string;
```

- [ ] **Step 3: Commit**

```bash
cd mf-frontend && npm test
git add src/lib/orgAccess.ts src/lib/orgAccess.test.ts src/lib/types.ts
git commit -m "$(cat <<'EOF'
Decide şirket panel access from org type and role, not platform admin

Individual owners and company members must never see /sirket; the gate
mirrors RequireOrgAdmin without trusting the UI alone.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

## Phase 2 — Shell (`/sirket` + `/org/me`)

### Task 5: `orgNav` rota tablosu

**Files:**
- Create: `mf-frontend/src/lib/orgNav.ts`, `orgNav.test.ts`

**Interfaces:**
- Produces:
  - `type OrgSection = "ozet" | "ekip" | "kullanim" | "aktivite"`
  - `ORG_SECTIONS: readonly { id; label; path }[]`
  - `sectionFromPath(pathname: string): OrgSection`

Paths: `/sirket`, `/sirket/ekip`, `/sirket/kullanim`, `/sirket/aktivite`. Labels: Özet, Ekip, Kullanım, Aktivite.

- [ ] **Step 1–5:** yönetim `adminNav` Task 1 deseninin birebir kopyası (path'ler `/sirket*`). Bilinmeyen alt yol → `ozet`.

```bash
cd mf-frontend && npm test
git add src/lib/orgNav.ts src/lib/orgNav.test.ts
git commit -m "$(cat <<'EOF'
Keep the şirket panel route table in one place

Sidebar and page titles must agree on the four sections without
importing the yönetim nav by accident.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: OrgShell + OrgGate + stub pages

**Files:**
- Create: `mf-frontend/src/components/sirket/OrgShell.tsx`, `OrgGate.tsx`, `OrgLogin.tsx`
- Create: `mf-frontend/src/app/sirket/layout.tsx`, `page.tsx`, `ekip/page.tsx`, `kullanim/page.tsx`, `aktivite/page.tsx`
- Modify: `mf-frontend/src/components/AppShell.tsx` — `canAccessOrgPanel(user)` iken `/sirket` linki (Yönetim linkinin yanında, birbirinin yerine değil)

**Interfaces:**
- Consumes: `orgGate`, `ORG_SECTIONS`, `sectionFromPath`
- Produces: Working `/sirket` shell; stubs show section title only until later tasks

**Notes:**
- `PanelShell` / `PanelLogin` **import etme** — kopyala/uyarla; marka metni "Şirket", nav `ORG_SECTIONS`.
- OrgGate: booting/login/redirect/allow — redirect → `/` (mesaj yok).
- Terms/password gates ürün tarafında kalır; panel layout'u Auth provider miras alır.
- Stub pages: `<h1>{label}</h1>` yeterli.

- [ ] **Step 1: Implement shell + stubs**
- [ ] **Step 2: `npm test && npm run lint`** (build optional if slow; prefer `npm run build` before PR)
- [ ] **Step 3: Commit**

```bash
git add mf-frontend/src/components/sirket mf-frontend/src/app/sirket mf-frontend/src/components/AppShell.tsx
git commit -m "$(cat <<'EOF'
Give company admins a /sirket shell separate from /yonetim

Sharing PanelShell would leak yönetim nav into the customer surface;
a copied language keeps the gates and labels apart.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Backend `internal/org` + `GET /org/me`

**Files:**
- Create: `mf-backend/internal/org/{models,store,handler,routes,handler_test}.go`
- Modify: `mf-backend/cmd/server/main.go` — mount `/org`

**Interfaces:**
- Consumes: `RequireAuth`, `RequirePasswordFresh`, `RequireOrgAdmin`
- Produces:
  - `GET /org/me` → `{ org: { id, name, type, seat_limit, status, member_count }, role: string }`
  - Scope from `claims.OrgID` only
  - `pwd_reset` token → 403 on `/org/*`

Mount:

```go
r.Mount("/org", orgHandler.Routes(tokens.Verify, cfg.RequestTimeout))
```

```go
func (h *Handler) Routes(verify common.TokenVerifier, timeout time.Duration) http.Handler {
	r := chi.NewRouter()
	r.Use(common.RequireAuth(verify))
	r.Use(common.RequirePasswordFresh)
	r.Use(common.RequireOrgAdmin)
	r.Use(common.Timeout(timeout))
	r.Get("/me", h.Me)
	return r
}
```

- [ ] **Step 1: Failing handler tests**

```go
func TestOrgMeRequiresOrgAdmin(t *testing.T) { /* member → 403 */ }
func TestOrgMeReturnsActorOrgOnly(t *testing.T) { /* claims.OrgID A; store has A+B; response is A */ }
func TestOrgMePasswordResetBlocked(t *testing.T) { /* PasswordReset true → 403 password_change_required */ }
```

Fake `OrgStore` interface with `GetOrgSummary(ctx, orgID) (OrgSummary, error)`.

- [ ] **Step 2–4:** FAIL → implement store SQL + handler → PASS
- [ ] **Step 5: Commit**

```bash
git add mf-backend/internal/org mf-backend/cmd/server/main.go
git commit -m "$(cat <<'EOF'
Mount /org/me behind RequireOrgAdmin before the shell calls it

Backend-first deploy: the gate and summary must exist before the
frontend treats missing org claims as a permanent deny.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

## Phase 3 — Team API + UI

### Task 8: Members list + create (seat_limit + temp password)

**Files:**
- Create/Modify: `mf-backend/internal/org/members.go`, `models.go`, `store.go`, `handler.go`, `routes.go`, `handler_test.go`

**Interfaces:**
- Produces:
  - `GET /org/members` → `{ members: [{ id, email, name, org_role, created_at, last_activity_at? }] }`
  - `POST /org/members` body `{ name, email, org_role: "admin"|"member" }` → 201 `{ member, temporary_password }`
  - 409 if `member_count >= seat_limit`
  - 409/conflict if email taken
  - `org_role: owner` in body → 400
  - New user: `org_id = claims.OrgID`, `must_change_password = true`, bcrypt temp password

- [ ] **Step 1: Failing tests**

```go
func TestCreateMemberBelowSeat(t *testing.T) { /* 201 + temporary_password non-empty; must_change true in store */ }
func TestCreateMemberAtSeatLimit(t *testing.T) { /* count==limit → 409 */ }
func TestCreateMemberRejectsOwnerRole(t *testing.T) { /* 400 */ }
func TestListMembersScopedToActorOrg(t *testing.T) { /* org B members absent */ }
```

- [ ] **Step 2–4:** implement (reuse `temporaryPassword` logic — duplicate small helper in org package; do not import admin internals for YAGNI/cycle)
- [ ] **Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
Let company admins add seats with a one-time password, not email

Seat_limit is a ceiling checked at insert; the panel never raises it —
that stays on the platform accounts surface.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: Set role + remove member (owner protect, cross-org 404)

**Files:**
- Modify: `members.go`, `handler_test.go`, `routes.go`

**Interfaces:**
- `PATCH /org/members/{id}` body `{ org_role: "admin"|"member" }`
- `DELETE /org/members/{id}`
- Rules:
  1. Target must belong to `claims.OrgID` else **404**
  2. Target `org_role == owner` → **400** (role or delete)
  3. Handler **re-reads** target row (stale JWT / race)
  4. Hard DELETE user row (CASCADE)
  5. Actor cannot use body org_id

- [ ] **Step 1: Failing tests**

```go
func TestPatchMemberCrossOrg404(t *testing.T) {}
func TestDeleteMemberCrossOrg404(t *testing.T) {}
func TestCannotChangeOrDeleteOwner(t *testing.T) {}
func TestPatchAdminToMember(t *testing.T) {}
```

- [ ] **Step 2–5:** implement + commit

```bash
git commit -m "$(cat <<'EOF'
Refuse owner demotion and hide cross-org member IDs as 404

A company admin probing another tenant's UUID must learn nothing;
owner remains the invariant CreateCompany already established.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: Audit writes on member mutations

**Files:**
- Create: `mf-backend/internal/org/audit.go`
- Modify: handlers to call `WriteAudit` after success
- Wire: main.go passes `adminStore` (or thin adapter) as `AuditWriter`

**Interfaces:**
- `type AuditWriter interface { WriteAudit(ctx, actorID, action, target string, detail map[string]any) error }`
- Actions: `org.member.create`, `org.member.role`, `org.member.remove`
- Detail metadata-only: `{ "org_role": "..." }` — no email/password/case text
- Audit failure: log + continue (admin deseni)

- [ ] **Step 1: Test with recordingAudit** (mirror `admin/audit_write_test.go`)
- [ ] **Step 2–5:** implement + commit

---

### Task 11: Frontend Team panel + `api.org`

**Files:**
- Modify: `mf-frontend/src/lib/api.ts`, `types.ts`
- Create: `mf-frontend/src/components/sirket/TeamPanel.tsx`
- Modify: `mf-frontend/src/app/sirket/ekip/page.tsx`
- Optional lib helper test: `seatFull(memberCount, seatLimit) → boolean` in `src/lib/orgTeam.ts` + test (add button disabled)

**UI:**
- Liste: ad, e-posta, rol, kayıt tarihi
- Ekle form: ad, e-posta, rol admin|member; seat full → disabled + 409 mesajı
- Geçici parola bir kez göster + kopyala (AccountsPanel deseni)
- Rol değiştir select; owner satırında disabled
- Çıkar: confirm dialog; owner yok

- [ ] Implement + `npm test` + commit

```bash
git commit -m "$(cat <<'EOF'
Ship /sirket/ekip so owners can seat staff without the platform panel

Temporary passwords stay on-screen once; email invite remains out of
scope until mail infrastructure exists.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

## Phase 4 — Usage / stats

### Task 12: `GET /org/stats`

**Files:**
- Create: `mf-backend/internal/org/stats.go`
- Modify: routes, models, handler_test

**Interfaces:**
- `GET /org/stats?window=30d|90d` (invalid → 400)
- Response (org-scoped; mirror admin boxes subset):

```go
type OrgStats struct {
	Boxes struct {
		Members        MemberSeatBox `json:"members"` // count, seat_limit
		ReportsLast24h StatBox       `json:"reports_last_24h"`
		ReportsWindow  StatBox       `json:"reports_window"`
		SchemaValidity SchemaBox     `json:"schema_validity"` // rate only
	} `json:"boxes"`
	AssessmentsPerDay []DayPoint     `json:"assessments_per_day"`
	SchemaValidPerDay []DayPoint     `json:"schema_valid_per_day"`
	RunsByTarget      []TargetSeries `json:"runs_by_target"`
	MemberActivity    []MemberAct    `json:"member_activity"` // user id/name, count, last_at — no case text
}
```

SQL: member ids `WHERE org_id = $actor`; filter `assessments` / `llm_runs` by those user_ids. **No Prometheus.**

- [ ] **Step 1: Test** — seed org A + B assessments; stats for A exclude B
- [ ] **Step 2–5:** implement + commit

---

### Task 13: Kullanım UI + özet kutuları

**Files:**
- Create: `UsagePanel.tsx`, wire `kullanim/page.tsx`
- Create/Modify: `OverviewPanel.tsx` (özet) — boxes from `/org/stats` + link to ekip/kullanım
- Reuse `TimeChart` from `components/ui/TimeChart.tsx`

- [ ] Implement + commit

---

## Phase 5 — Activity feed

### Task 14: `GET /org/activity`

**Files:**
- Create: `mf-backend/internal/org/activity.go`

**Interfaces:**
- `GET /org/activity?limit=&before=` (before = RFC3339 cursor; default limit 50, max 100)
- Items metadata only, e.g.:

```go
type ActivityItem struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"` // member.joined | analysis.completed | analysis.schema_invalid | session.login
	At        time.Time `json:"at"`
	ActorName string    `json:"actor_name,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"` // counts/flags only
}
```

Sources (union, newest first): recent member creates (users.created_at in org), assessments completed (id + schema_valid + user name, **no title/body**), sessions created. No deep link to reports.

- [ ] Cross-org exclusion test + commit

---

### Task 15: Aktivite UI + özet kısa liste

**Files:**
- Create: `ActivityPanel.tsx`; wire `aktivite/page.tsx`; Overview shows last N

- [ ] Implement + commit

---

## Verification (every phase end)

```bash
cd mf-backend && go test ./internal/org/ ./internal/auth/ ./internal/common/ ./migrations/ -count=1
cd mf-frontend && npm test && npm run lint
```

Full `go test ./...` before PR.

---

## Spec coverage checklist (self-review)

| Spec § | Task(s) |
|---|---|
| Rol modeli / OrgGate | 2–4, 6 |
| Claims JWT + Me | 2–3 |
| RequireOrgAdmin + pwd_reset | 2, 7 |
| `/sirket` shell + nav | 5–6 |
| `/org/me` | 7 |
| Ekip CRUD + seat + temp pwd | 8–9, 11 |
| Owner koruması + cross-org 404 | 9 |
| Audit member writes | 10 |
| Stats Postgres only | 12–13 |
| Activity metadata | 14–15 |
| No /admin for org admins | Global + routes isolation |
| Individual closed | 4, 2 |
| No company create / email / quotas | Global — no tasks add them |

---

## Execution

Plan complete. **Subagent-Driven Development** (user said go): fresh implementer per task, task review between, feature branch `feat/sirket-paneli` from this docs branch after the plan commit.

**Priority cut line:** Tasks 1–11 = MVP (identity + shell + team). Tasks 12–15 if time; else leave unchecked.
