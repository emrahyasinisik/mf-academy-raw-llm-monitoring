# Kayıtta KVKK ve kullanım sözleşmesi onayı — uygulama planı

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Kaydolan kişi kullanım sözleşmesini kabul etsin, aydınlatma metnini okuduğunu teyit etsin, ve bu kabul hangi metne ait olduğuyla birlikte kaydedilsin.

**Architecture:** Kabul, kullanıcıyı yaratan `INSERT`'ün içinde yazılır — böylece kabulü olmayan bir kullanıcı hiç var olamaz. Mevcut kullanıcılar girişte bir kapıyla karşılaşır; kapı `GET /auth/me`'nin döndürdüğü alandan açılır, yeni bir kontrol uç noktası yoktur.

**Tech Stack:** Go 1.26.5 (chi, pgx v5, bcrypt), PostgreSQL, Next.js 16 + React, TypeScript.

**Spec:** [`docs/superpowers/specs/2026-08-01-kayit-onayi-design.md`](../specs/2026-08-01-kayit-onayi-design.md)

## Global Constraints

- **Migration numarası 011.** 010 KVKK saklama işinde kullanıldı; 009 `feat/persona-history` dalında duruyor.
- **Veritabanı destekli test YOK.** Testcontainers, sqlmock ya da canlı Postgres eklenmeyecek. Handler'lar `UserStore` arayüzü üzerinden sahtelerle test edilir — mevcut desen bu, `internal/auth/handler_test.go`'ya bak.
- **Frontend testleri yalnızca `src/lib/*.test.ts`**, Node'un yerleşik koşucusuyla (`npm test`). Bileşen testi altyapısı yok, eklenmeyecek; `npm run lint` ve `npm run build` ile doğrulanır.
- **API alan adları snake_case** (`accepted_terms`, `terms_accepted_at`).
- **UI metinleri Türkçe**, donanım/GPU/model adı geçmez, yalnızca koyu tema.
- **Sürüm sabiti `2026-08-01`**, Go tarafında `auth.TermsVersion`.
- **Sunucu kabulü varsaymaz:** `accepted_terms` eksikse ya da `false` ise kayıt **400** ile reddedilir.
- **Metinlerde hukukçu onayından geçmediği yazılı olacak.** Spec'te yazması yetmez.

---

### Task 1: Şema, model ve kabulü yazan INSERT

**Files:**
- Create: `mf-backend/migrations/011_terms.sql`
- Modify: `mf-backend/internal/auth/models.go`
- Modify: `mf-backend/internal/auth/store.go`
- Modify: `mf-backend/internal/auth/handler.go` (yalnızca `UserStore` arayüzü ve `CreateUser` çağrısı)

**Interfaces:**
- Consumes: yok (ilk görev)
- Produces: `users.terms_accepted_at TIMESTAMPTZ`, `users.terms_version TEXT NOT NULL DEFAULT ''`; `auth.User.TermsAcceptedAt *time.Time` (json `terms_accepted_at`); `auth.TermsVersion` sabiti; `UserStore.CreateUser(ctx context.Context, email, passwordHash, name, termsVersion string) (User, error)`.

- [ ] **Step 1: Migration'ı yaz**

`mf-backend/migrations/011_terms.sql`:

```sql
-- Acceptance of the terms and of having read the privacy notice.
--
-- Two columns rather than one boolean. The record exists to answer "what did
-- they accept", and a timestamp alone cannot: edit the text and every past
-- acceptance silently becomes an acceptance of the new wording. The version
-- pins it.
--
-- Nullable rather than NOT NULL DEFAULT now(): existing rows have not accepted
-- anything, and defaulting them would manufacture a record of something that
-- never happened. They are gated at login instead.
ALTER TABLE users ADD COLUMN IF NOT EXISTS terms_accepted_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS terms_version     TEXT NOT NULL DEFAULT '';
```

- [ ] **Step 2: Migration numara testini koş**

Run: `cd mf-backend && go test ./migrations/ -v`
Expected: PASS. Bu test 010 işinde eklendi ve iki dosyanın aynı numarayı paylaşmasını yakalar.

- [ ] **Step 3: Modeli ve sürüm sabitini ekle**

`mf-backend/internal/auth/models.go` — `User` struct'ına, `UpdatedAt`'ten sonra:

```go
	// Nil for an account that predates the terms, which is what the login gate
	// keys on. The accepted version is stored but not returned: nothing in the
	// product reads it yet, and a field the client cannot act on is one more
	// thing to keep in sync.
	TermsAcceptedAt *time.Time `json:"terms_accepted_at"`
```

Aynı dosyanın başına, tipi olmayan bir sabit olarak:

```go
// TermsVersion identifies the text a user accepted. Bump it when the wording
// changes in a way a reasonable person would want to re-read — not for typos.
const TermsVersion = "2026-08-01"
```

- [ ] **Step 4: Store'u güncelle**

`mf-backend/internal/auth/store.go`, `CreateUser`:

```go
// CreateUser inserts a user together with the acceptance that created them.
//
// One statement, not two. A separate acceptance write could fail after the
// account exists, leaving a user who never agreed to anything and no way to
// tell that apart from a user who registered before the terms existed.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash, name, termsVersion string) (User, error) {
	var u User
	err := s.db.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, terms_accepted_at, terms_version)
		 VALUES ($1, $2, $3, now(), $4)
		 RETURNING id, email, name, role, created_at, updated_at, terms_accepted_at`,
		email, passwordHash, name, termsVersion,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt, &u.TermsAcceptedAt)
	return u, err
}
```

Sonra `store.go` içinde `User` döndüren **her** sorguyu bul (`GetUserByEmailWithHash`, `GetUserByID`, `UpdateName`, ve varsa diğerleri) ve her birine `terms_accepted_at` sütununu **ve** `&u.TermsAcceptedAt` tarama hedefini ekle. pgx konuma göre tarar: sütunu ekleyip taramayı unutmak sonraki alana yanlış değer yazar ve derleme hatası vermez.

- [ ] **Step 5: Arayüzü ve çağrıyı güncelle**

`mf-backend/internal/auth/handler.go` — `UserStore` arayüzünde:

```go
	CreateUser(ctx context.Context, email, passwordHash, name, termsVersion string) (User, error)
```

Ve `Register` içindeki çağrı:

```go
	user, err := h.store.CreateUser(r.Context(), req.Email, string(hash), strings.TrimSpace(req.Name), TermsVersion)
```

- [ ] **Step 6: Derle ve testleri koş**

Run: `cd mf-backend && go build ./... && go test ./... -count=1`
Expected: PASS. `handler_test.go`'daki sahte store'un `CreateUser`'ı da yeni imzaya uymalı.

- [ ] **Step 7: Commit**

```bash
git add mf-backend/migrations/011_terms.sql mf-backend/internal/auth/
git commit -m "feat(auth): record what was accepted, in the insert that creates the user"
```

---

### Task 2: Kayıtta zorunlu onay ve `POST /auth/accept-terms`

**Files:**
- Modify: `mf-backend/internal/auth/models.go`
- Modify: `mf-backend/internal/auth/store.go`
- Modify: `mf-backend/internal/auth/handler.go`
- Modify: `mf-backend/internal/auth/routes.go`
- Test: `mf-backend/internal/auth/handler_test.go`

**Interfaces:**
- Consumes: Task 1'in `TermsVersion` sabiti ve `User.TermsAcceptedAt` alanı.
- Produces: `RegisterRequest.AcceptedTerms bool` (json `accepted_terms`); `UserStore.AcceptTerms(ctx context.Context, userID, version string) error`; `Handler.AcceptTerms(w, r)` ve `POST /auth/accept-terms` (kimlikli, gövdesiz, **204**).

- [ ] **Step 1: Başarısız testleri yaz**

`mf-backend/internal/auth/handler_test.go` sonuna. Dosyadaki mevcut sahte store'u kullan; yoksa oradaki desene uyan bir tane tanımla:

```go
func TestRegisterRefusesWithoutAcceptance(t *testing.T) {
	for _, body := range []string{
		`{"email":"a@b.co","password":"parola12345","name":"A"}`,
		`{"email":"a@b.co","password":"parola12345","name":"A","accepted_terms":false}`,
	} {
		w := postJSON(t, newTestHandler(t).Register, "/auth/register", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %s -> status %d, want 400", body, w.Code)
		}
	}
}

// Kabul edilmemis bir kayit hic olusmamali: 400 donup kullaniciyi yine de
// yaratmak, kabulu olmayan bir hesap birakir ve kapi onu yakalamaz.
func TestRegisterDoesNotCreateUserWhenRefused(t *testing.T) {
	h, st := newTestHandlerWithStore(t)
	postJSON(t, h.Register, "/auth/register",
		`{"email":"a@b.co","password":"parola12345","name":"A"}`)
	if st.created != 0 {
		t.Errorf("CreateUser called %d times, want 0", st.created)
	}
}

func TestAcceptTermsRecordsAndIsIdempotent(t *testing.T) {
	h, st := newTestHandlerWithStore(t)
	for i := 0; i < 2; i++ {
		w := authedPost(t, h.AcceptTerms, "/auth/accept-terms", "user-1")
		if w.Code != http.StatusNoContent {
			t.Fatalf("call %d -> status %d, want 204", i+1, w.Code)
		}
	}
	if st.acceptedVersion != TermsVersion {
		t.Errorf("stored version %q, want %q", st.acceptedVersion, TermsVersion)
	}
}
```

`newTestHandler`, `newTestHandlerWithStore`, `postJSON` ve `authedPost` yardımcıları dosyada zaten varsa onları kullan; yoksa mevcut testlerin kullandığı yapıyı taklit ederek ekle. Kimlikli istek için `common.ContextWithClaims(ctx, common.AuthClaims{UserID: "user-1"})` kullanılır.

- [ ] **Step 2: Testleri koş, düştüğünü gör**

Run: `cd mf-backend && go test ./internal/auth/ -run "TestRegisterRefuses|TestRegisterDoesNot|TestAcceptTerms" -v`
Expected: FAIL — `AcceptedTerms` alanı ve `AcceptTerms` handler'ı yok.

- [ ] **Step 3: İsteği ve doğrulamayı ekle**

`mf-backend/internal/auth/models.go`, `RegisterRequest`:

```go
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	// No omitempty and no default: an acceptance the server infers is not an
	// acceptance. Absent reads as false and is refused like an explicit false.
	AcceptedTerms bool `json:"accepted_terms"`
}
```

`handler.go`, `Register` içinde — `validateCredentials` çağrısının **hemen ardından**, bcrypt'ten önce (reddedilecek bir isteğe 50 ms bcrypt harcamanın anlamı yok):

```go
	if !req.AcceptedTerms {
		common.Error(w, common.ErrBadRequest(
			"kullanım koşulları kabul edilmeden kayıt yapılamaz"))
		return
	}
```

- [ ] **Step 4: Store metodunu ve handler'ı yaz**

`store.go`:

```go
// AcceptTerms records acceptance for a user who registered before the terms
// existed.
//
// The WHERE clause keeps the first acceptance: re-accepting must not move the
// date, because the date is the record. A second call changes nothing and is
// not an error — the caller asked for a state that already holds.
func (s *Store) AcceptTerms(ctx context.Context, userID, version string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET terms_accepted_at = now(), terms_version = $2, updated_at = now()
		  WHERE id = $1 AND terms_accepted_at IS NULL`, userID, version)
	return err
}
```

`handler.go`, `UserStore` arayüzüne `AcceptTerms(ctx context.Context, userID, version string) error` ekle ve handler'ı yaz:

```go
// AcceptTerms records that the caller accepted the current terms.
// POST /auth/accept-terms
func (h *Handler) AcceptTerms(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	if err := h.store.AcceptTerms(r.Context(), claims.UserID, TermsVersion); err != nil {
		common.Error(w, common.ErrInternal("could not record acceptance"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Rotayı bağla**

`routes.go` — kimlik doğrulamalı grupta, `pr.Get("/me", h.Me)` satırının yanına:

```go
		pr.Post("/accept-terms", h.AcceptTerms)
```

- [ ] **Step 6: Testleri koş**

Run: `cd mf-backend && go build ./... && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add mf-backend/internal/auth/
git commit -m "feat(auth): refuse a registration that accepted nothing"
```

---

### Task 3: Kullanım koşulları sayfası ve `#kosullar` rotası

**Files:**
- Create: `mf-frontend/src/components/views/KosullarView.tsx`
- Modify: `mf-frontend/src/components/AppShell.tsx`

**Interfaces:**
- Consumes: yok.
- Produces: `KosullarView` bileşeni ve `#kosullar` rotası.

- [ ] **Step 1: Sayfayı yaz**

`mf-frontend/src/components/views/KosullarView.tsx`:

```tsx
"use client";

// Kullanım koşulları. Gizlilik metni ayrı bir sayfa ve orada kalıyor: bu belge
// hizmetin hangi şartlarla verildiğini, o belge veriyle ne yapıldığını
// anlatıyor. Saklama süresi gibi sayılar yalnızca birinde yazılı olmalı, yoksa
// ikisi ilk düzenlemede birbirinden ayrılır.
export function KosullarView() {
  return (
    <div className="mx-auto max-w-2xl px-4 sm:px-5 py-6">
      <h1 className="text-lg">Kullanım koşulları</h1>

      <h2 className="eyebrow mt-6">Bu hizmet nedir</h2>
      <p className="mt-2 text-sm">
        Bir vaka metni giriyorsunuz, önceden tanımlı bir rubriğe göre
        puanlanmış bir rapor alıyorsunuz. Puanı model vermiyor: model rubriği
        dolduruyor, ağırlıklı toplam bizim tarafımızda hesaplanıyor, ve her
        kriterin dayandığı alıntılar raporda gösteriliyor.
      </p>

      <h2 className="eyebrow mt-6">Rapor ne değildir</h2>
      <p className="mt-2 text-sm">
        Rapor bir yatırım tavsiyesi değildir ve sizin yerinize karar vermez.
        Bir ön eleme aracıdır: aynı ölçütü her vakaya aynı şekilde uygular ve
        gerekçesini gösterir. Kararı veren ve sonucundan sorumlu olan sizsiniz.
      </p>

      <h2 className="eyebrow mt-6">Garanti verilmiyor</h2>
      <p className="mt-2 text-sm">
        Bu bir demo. Doğruluk, kesintisizlik veya erişilebilirlik taahhüdü
        yok; hizmet önceden haber verilmeden değişebilir ya da durabilir.
        Üretilen raporların doğru olduğunu garanti etmiyoruz — ürünün amacı
        zaten değerlendirmeyi denetlenebilir kılmak, denetimi ortadan
        kaldırmak değil.
      </p>

      <h2 className="eyebrow mt-6">Girdiğiniz içerikten siz sorumlusunuz</h2>
      <p className="mt-2 text-sm">
        Yapıştırdığınız metni buraya girmeye yetkili olduğunuzu beyan etmiş
        olursunuz. Bu, üçüncü kişilere ait belgeler için de geçerlidir: bir
        başkasının şirketine ait bir dokümanı yüklüyorsanız, onu paylaşma
        hakkına sahip olduğunuzu varsayıyoruz.
      </p>

      <h2 className="eyebrow mt-6">Verileriniz</h2>
      <p className="mt-2 text-sm">
        Ne sakladığımız, ne kadar süreyle sakladığımız ve nasıl
        sildirebileceğiniz ayrı bir sayfada:{" "}
        <a href="#gizlilik">Verileriniz ve gizlilik</a>.
      </p>

      {/* Okuyanın görmediği sınır, sınır değildir. Bu cümle spec'te de var ama
          asıl yeri burası. */}
      <h2 className="eyebrow mt-6">Bu metnin sınırı</h2>
      <p className="mt-2 text-sm">
        Bu koşullar bir hukukçu tarafından hazırlanmadı. Demo için, ürünün
        gerçekte ne yaptığından türetilerek yazıldı. Gerçek müşteri verisiyle
        kullanılmadan önce gözden geçirilmesi gerekiyor.
      </p>
    </div>
  );
}
```

- [ ] **Step 2: Rotayı tanınır kıl**

`mf-frontend/src/components/AppShell.tsx`:

- `MasterView` birliğine `| "kosullar"` ekle.
- `OFF_NAV` dizisine `"kosullar"` ekle (`NAV`'a **ekleme** — nav çalışma araçları için).
- Görünüm anahtarlamasına `{view === "kosullar" && <KosullarView />}` ekle.
- Alt bilgideki gizlilik bağlantısının yanına ikincisini koy:

```tsx
  <a href="#kosullar">Kullanım koşulları</a>
```

- [ ] **Step 3: Doğrula**

Run: `cd mf-frontend && npm run lint && npm run build`
Expected: temiz.

- [ ] **Step 4: Commit**

```bash
git add mf-frontend/src/components/views/KosullarView.tsx mf-frontend/src/components/AppShell.tsx
git commit -m "feat(auth): write down the terms the service is actually offered under"
```

---

### Task 4: Kayıt formunda onay ve girişte kapı

**Files:**
- Modify: `mf-frontend/src/lib/types.ts`
- Modify: `mf-frontend/src/lib/api.ts`
- Create: `mf-frontend/src/lib/terms.ts`
- Test: `mf-frontend/src/lib/terms.test.ts`
- Modify: `mf-frontend/src/store/auth.tsx`
- Modify: `mf-frontend/src/components/views/AuthView.tsx`
- Create: `mf-frontend/src/components/views/OnayView.tsx`
- Modify: `mf-frontend/src/components/AppShell.tsx`

**Interfaces:**
- Consumes: Task 2'nin `accepted_terms` alanı ve `POST /auth/accept-terms`; Task 3'ün `#kosullar` rotası.
- Produces: `needsTermsGate(user: { terms_accepted_at: string | null } | null): boolean`; `api.acceptTerms(): Promise<void>`; `AuthState.acceptTerms: () => Promise<void>`.

- [ ] **Step 1: Kapı kararının testini yaz**

`mf-frontend/src/lib/terms.test.ts`:

```ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { needsTermsGate } from "./terms.ts";

test("kabul etmemiş kullanıcı kapıya düşer", () => {
  assert.equal(needsTermsGate({ terms_accepted_at: null }), true);
});

test("kabul etmiş kullanıcı uygulamayı görür", () => {
  assert.equal(needsTermsGate({ terms_accepted_at: "2026-08-01T10:00:00Z" }), false);
});

// Oturum yokken kapı yok: o durumda giriş ekranı gösteriliyor ve kapıyı da
// göstermek, henüz kim olduğunu bilmediğimiz birinden kabul istemek olurdu.
test("oturum yoksa kapı yok", () => {
  assert.equal(needsTermsGate(null), false);
});
```

- [ ] **Step 2: Testi koş, düştüğünü gör**

Run: `cd mf-frontend && npm test`
Expected: FAIL — `./terms.ts` yok.

- [ ] **Step 3: Yardımcıyı yaz**

`mf-frontend/src/lib/terms.ts`:

```ts
/** Kabul kapısı gerekiyor mu — tek karar, tek yerde. */
export function needsTermsGate(
  user: { terms_accepted_at: string | null } | null,
): boolean {
  return user !== null && user.terms_accepted_at === null;
}
```

- [ ] **Step 4: Testi koş**

Run: `cd mf-frontend && npm test`
Expected: PASS.

- [ ] **Step 5: Tipleri ve API'yi genişlet**

`mf-frontend/src/lib/types.ts` — `User` arayüzüne:

```ts
  terms_accepted_at: string | null;
```

`mf-frontend/src/lib/api.ts` — `register` çağrısının gövdesine `accepted_terms` ekle ve imzasına dördüncü parametreyi koy; ayrıca yeni çağrıyı ekle:

```ts
  acceptTerms: () =>
    request<void>("/auth/accept-terms", { method: "POST" }),
```

- [ ] **Step 6: Provider'a kabulü ekle**

`mf-frontend/src/store/auth.tsx` — `AuthState` arayüzüne:

```ts
  register: (email: string, password: string, name: string, acceptedTerms: boolean) => Promise<void>;
  acceptTerms: () => Promise<void>;
```

`register` gövdesinde bayrağı `api.register`'a geçir. Ve:

```tsx
  // Kabulden sonra kullanıcıyı sunucudan yeniden okuyoruz, elde düzeltmiyoruz:
  // kapının dayandığı alan sunucunun yazdığı alan, ve ikisini ayrı ayrı doğru
  // tutmaya çalışmak tam olarak bu tür bir kapının bozulma şekli.
  const acceptTerms = useCallback(async () => {
    await api.acceptTerms();
    setUser(await api.me());
  }, []);
```

`acceptTerms`'ü provider'ın `value`'suna ekle.

- [ ] **Step 7: Kayıt formuna onay kutusunu koy**

`mf-frontend/src/components/views/AuthView.tsx` — `sub === "register"` kolunda, gönderim düğmesinden hemen önce:

```tsx
{sub === "register" && (
  <label className="flex items-start gap-2 text-xs" style={{ color: "var(--text-faint)" }}>
    <input
      type="checkbox"
      checked={accepted}
      onChange={(e) => setAccepted(e.target.checked)}
      className="mt-0.5"
    />
    <span>
      <a href="#kosullar" onClick={() => setShowDoc("kosullar")}>Kullanım koşullarını</a>{" "}
      kabul ediyorum ve{" "}
      <a href="#gizlilik" onClick={() => setShowDoc("gizlilik")}>aydınlatma metnini</a>{" "}
      okudum.
    </span>
  </label>
)}
```

`const [accepted, setAccepted] = useState(false);` ekle. Gönderim düğmesinin `disabled` koşuluna kaydolma kolunda kutuyu da dahil et:

```tsx
disabled={busy || (sub === "register" && !accepted)}
```

`submit` içindeki çağrıyı `await register(email, password, name, accepted);` yap.

Mevcut `showPrivacy` mekanizması `#gizlilik` için zaten var; `#kosullar` için aynı deseni izleyen bir durum ekle ve `KosullarView`'ı aynı şekilde göster. İki ayrı boolean yerine tek bir `showDoc: "gizlilik" | "kosullar" | null` durumu tut ve mevcut `showPrivacy` kullanımlarını ona taşı — iki bağımsız boolean, ikisi birden açıkken hangisinin görüneceğini tanımsız bırakır.

- [ ] **Step 8: Kapıyı kur**

`mf-frontend/src/components/views/OnayView.tsx`:

```tsx
"use client";

import { useState } from "react";
import { useAuth } from "@/store/auth";
import { GizlilikView } from "./GizlilikView";
import { KosullarView } from "./KosullarView";

// Kabul etmemis mevcut kullanicilar icin kapi.
//
// Cikis yolu var ve olmak zorunda: tek cikisi kabul etmek olan bir ekran,
// kabul degildir.
export function OnayView() {
  const { acceptTerms, logout } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  return (
    <div className="mx-auto max-w-2xl px-4 sm:px-5 py-6">
      <h1 className="text-lg">Devam etmeden önce</h1>
      <p className="mt-2 text-sm">
        Bu hesap, kullanım koşulları yayımlanmadan önce açılmış. Devam etmek
        için koşulları kabul etmeniz ve aydınlatma metnini okumanız gerekiyor.
      </p>

      <div className="mt-6 space-y-6">
        <KosullarView />
        <GizlilikView />
      </div>

      {error && <div className="notice mt-4" role="status">{error}</div>}

      <div className="flex items-center gap-3 mt-6">
        <button
          className="btn btn-primary"
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            setError("");
            try {
              await acceptTerms();
            } catch {
              setError("Kabul kaydedilemedi. Tekrar deneyin.");
              setBusy(false);
            }
          }}
        >
          Kabul ediyorum
        </button>
        <button className="btn btn-ghost" onClick={() => logout()}>
          Çıkış yap
        </button>
      </div>
    </div>
  );
}
```

`mf-frontend/src/components/AppShell.tsx` — `if (!user) return <AuthView />;` satırının **hemen ardından**:

```tsx
  // Oturum var ama kabul yok: uygulamayi degil kapiyi goster. AuthView ile ayni
  // dallanma sekli, ayni yerde, cunku ikisi de "henuz uygulamaya giremez"in
  // farkli sebepleri.
  if (needsTermsGate(user)) return <OnayView />;
```

`needsTermsGate`'i `@/lib/terms`'ten içe aktar.

- [ ] **Step 9: Hepsini koş**

Run: `cd mf-frontend && npm test && npm run lint && npm run build`
Expected: hepsi temiz.

- [ ] **Step 10: Commit**

```bash
git add mf-frontend/src/lib/ mf-frontend/src/store/auth.tsx \
        mf-frontend/src/components/views/AuthView.tsx \
        mf-frontend/src/components/views/OnayView.tsx \
        mf-frontend/src/components/AppShell.tsx
git commit -m "feat(auth): a checkbox at registration, a gate for everyone before it"
```

---

## Self-review notları

**Spec kapsamı.** Migration ve sürüm alanı Task 1; kayıt reddi ve `accept-terms` Task 2; `#kosullar` sayfası ve metin Task 3; onay kutusu, kapı ve çıkış yolu Task 4.

**Test edilmeyen ne kaldı:** `AcceptTerms`'ün `WHERE terms_accepted_at IS NULL` yüklemi — yani ikinci kabulün ilk tarihi değiştirmediği — yalnızca handler seviyesinde 204 olarak doğrulanıyor. SQL'in kendisi bu repoda test edilmiyor; ilk deploy'da iki kez kabul edip tarihin sabit kaldığını elle görmek gerekiyor.

**Sıra bağımlılığı:** Task 2, Task 1'in `TermsVersion` sabitine bağlı. Task 4, Task 2'nin uç noktasına ve Task 3'ün rotasına bağlı. Task 3 bağımsız ve önce de koşulabilir.
