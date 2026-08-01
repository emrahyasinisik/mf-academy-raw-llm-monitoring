# KVKK aydınlatma metni ve veri silme — uygulama planı

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Barındırdığımız demo'da kullanıcı içeriğini silinebilir kılmak, 30 gün sonra kendiliğinden silmek, ve ne sakladığımızı uygulama içinde bir sayfada anlatmak.

**Architecture:** Silme = redaksiyon. Kişisel sütunlar boşalır, ölçüm satırı `redacted_at` damgasıyla kalır. Kullanıcının bastığı düğme ile 30. günde koşan arka plan işçisi **aynı UPDATE'i** çalıştırır. Frontend `#gizlilik` hash rotasında bir metin sayfası ve rapor listesinde satır içi silme ekler.

**Tech Stack:** Go 1.26.5 (chi, pgx v5), PostgreSQL, Next.js 16 + React, TypeScript.

**Spec:** [`docs/superpowers/specs/2026-08-01-kvkk-ve-veri-silme-design.md`](../specs/2026-08-01-kvkk-ve-veri-silme-design.md)

## Global Constraints

- **Migration numarası 010'dur, 009 değil.** 009 `feat/persona-history` dalında kullanılmış (`a52aaf0`); iki dal birleşince aynı numarada iki dosya olur.
- **Bu repoda veritabanı destekli test yoktur.** Handler'lar tüketici tarafında bildirilen arayüzlerle (`AssessmentStore`, `llm.RunStore`) sahte store'larla test edilir. Testcontainers, sqlmock veya canlı Postgres **eklenmeyecek**. SQL'in kendisi otomatik test edilmez; bu bilinen ve kabul edilen bir sınırdır.
- **Frontend testleri yalnızca `src/lib/*.test.ts`** dosyalarıdır, Node'un yerleşik koşucusuyla (`npm test`). Bileşen testi altyapısı yoktur, eklenmeyecek. Bileşenler `npm run lint` ve `npm run build` ile doğrulanır.
- **API alan adları snake_case**'dir (`subject_title`, `overall_score`), TypeScript tiplerinde de öyle.
- **UI metinleri Türkçe**, atölye kopyası yok: kart, tünel, GPU, model adı kullanıcıya görünmez. Arayüz yalnızca koyu tema.
- **Saklama süresi varsayılanı 30 gün**, `RETENTION_DAYS`. `0` süpürgeyi kapatır ve boot'ta uyarı basar.
- **Başvuru kanalı (e-posta) kapsam dışıdır.** Metin, olmayan bir kanalı varmış gibi göstermeyecek.
- **Mevcut `DELETE /llm/runs/{id}` değiştirilmeyecek.** Satırı gerçekten siliyor ve redaksiyondan daha koruyucu.

---

### Task 1: Şema, modeller ve numara çakışması testi

**Files:**
- Create: `mf-backend/migrations/010_retention.sql`
- Create: `mf-backend/migrations/migrations_test.go`
- Modify: `mf-backend/internal/analysis/models.go`
- Modify: `mf-backend/internal/analysis/store.go` (SELECT sütun listeleri)
- Modify: `mf-backend/internal/llm/models.go`

**Interfaces:**
- Consumes: yok (ilk görev)
- Produces: `assessments.redacted_at` ve `llm_runs.redacted_at` sütunları; Go tarafında `Assessment.RedactedAt *time.Time`, `AssessmentSummary.RedactedAt *time.Time`, `llm.Run.RedactedAt *time.Time`, JSON adı `redacted_at`.

- [ ] **Step 1: Migration numaralarının tekilliğini doğrulayan testi yaz**

`mf-backend/migrations/migrations_test.go`:

```go
package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

// Migrations apply in filename order and every one of them runs on every boot.
// Two files sharing a number is not a sort error — it is two authors believing
// they own the same slot, which is exactly what happens when a feature branch
// adds 009 while another branch does the same. The collision is invisible until
// the merge, and then the ordering between them is alphabetical accident.
func TestMigrationNumbersAreUnique(t *testing.T) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	seen := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		num, _, ok := strings.Cut(name, "_")
		if !ok {
			t.Errorf("migration %q has no NNN_ prefix", name)
			continue
		}
		if prev, dup := seen[num]; dup {
			t.Errorf("migrations %q and %q share the number %s", prev, name, num)
		}
		seen[num] = name
	}
	if len(seen) == 0 {
		t.Fatal("no migrations found — the embed pattern is wrong")
	}
}
```

- [ ] **Step 2: Testi koş, geçtiğini gör**

Run: `cd mf-backend && go test ./migrations/ -run TestMigrationNumbersAreUnique -v`
Expected: PASS (mevcut 001–008 tekil). Bu test 010'u eklemeden önce yeşil olmalı — koruma görevi görüyor, keşif değil.

- [ ] **Step 3: Migration 010'u yaz**

`mf-backend/migrations/010_retention.sql`:

```sql
-- Retention and erasure for the hosted demo.
--
-- Deleting a row is not the tool here. assessments is an audit trail on
-- purpose: criteria_snapshot is copied so a later rubric edit cannot rewrite
-- what an old report meant, and raw_response is kept verbatim because it is the
-- only way to tell a bad model from a bad parser. Dropping rows would move
-- every aggregate they feed — schema_valid, and the consistency figure a trial
-- group exists to produce — and half a trial group is worse than none.
--
-- So the personal columns are blanked and the row stays. What is left is an
-- anonymous measurement. redacted_at records that this happened, and when.
ALTER TABLE assessments ADD COLUMN IF NOT EXISTS redacted_at TIMESTAMPTZ;
ALTER TABLE llm_runs    ADD COLUMN IF NOT EXISTS redacted_at TIMESTAMPTZ;

-- Partial indexes: the sweep only ever looks for rows it has not already done,
-- so a row leaves the index the moment it is redacted. A full index on
-- created_at would grow with the table and make the sweep slower every month;
-- this one is bounded by the 30-day window.
CREATE INDEX IF NOT EXISTS idx_assessments_unredacted
    ON assessments (created_at) WHERE redacted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_llm_runs_unredacted
    ON llm_runs (created_at) WHERE redacted_at IS NULL;
```

- [ ] **Step 4: Testi tekrar koş**

Run: `cd mf-backend && go test ./migrations/ -v`
Expected: PASS. 010 tekil bir numara.

- [ ] **Step 5: Go modellerine alanı ekle**

`mf-backend/internal/analysis/models.go` — `Assessment` ve `AssessmentSummary` struct'larına, `CreatedAt` alanının hemen yanına:

```go
	// Non-nil once the personal columns have been blanked, by the owner's own
	// request or by the retention sweep. The reader needs this to tell "silindi"
	// from "boş geldi": a redacted report has an empty subject for a reason, and
	// showing it as missing data would misdescribe what happened to it.
	RedactedAt *time.Time `json:"redacted_at"`
```

`mf-backend/internal/llm/models.go` — `Run` struct'ına aynı alan, aynı yorumun kısası:

```go
	// Non-nil once the retention sweep has blanked prompt and response.
	RedactedAt *time.Time `json:"redacted_at"`
```

- [ ] **Step 6: SELECT sütun listelerine ekle**

`mf-backend/internal/analysis/store.go` içinde `assessmentColumns` sabitine `a.redacted_at` ekle, ve `scanAssessment` ile `ListAssessments`'in özet taramasına karşılık gelen `&a.RedactedAt` alanını **aynı sırada** ekle. `internal/llm/store.go` içinde `llm_runs` okuyan her `SELECT` ve `RETURNING` listesine `redacted_at`, taramalara `&run.RedactedAt`.

Sıra kritik: pgx sütunları konuma göre tarar, isme göre değil. Bir sütunu listeye ekleyip taramaya eklememek, sonraki alana yanlış değer yazar ve derleme hatası vermez.

- [ ] **Step 7: Derle ve tüm testleri koş**

Run: `cd mf-backend && go build ./... && go test ./...`
Expected: PASS, derleme temiz.

- [ ] **Step 8: Commit**

```bash
git add mf-backend/migrations/010_retention.sql mf-backend/migrations/migrations_test.go \
        mf-backend/internal/analysis/models.go mf-backend/internal/analysis/store.go \
        mf-backend/internal/llm/models.go mf-backend/internal/llm/store.go
git commit -m "feat(kvkk): add redacted_at, and a test that guards the migration number"
```

---

### Task 2: Redaksiyon ve DELETE uç noktası

**Files:**
- Modify: `mf-backend/internal/analysis/store.go`
- Modify: `mf-backend/internal/analysis/handler.go`
- Modify: `mf-backend/internal/analysis/routes.go`
- Test: `mf-backend/internal/analysis/handler_test.go`

**Interfaces:**
- Consumes: Task 1'in `redacted_at` sütunu ve `RedactedAt` alanları.
- Produces: `AssessmentStore.RedactAssessment(ctx context.Context, userID, id string) (bool, error)` — `(true, nil)` redakte etti, `(false, nil)` zaten redakteydi, `(false, ErrNoRows)` bulunamadı/sahibi değil. `Handler.Delete(w, r)` ve `DELETE /analysis/{id}`.

- [ ] **Step 1: Başarısız handler testlerini yaz**

`mf-backend/internal/analysis/handler_test.go` dosyasının sonuna. Dosyada halihazırda sahte store yoksa bu testle birlikte tanımla:

```go
// redactStore is the only part of AssessmentStore these tests touch. The rest
// of the interface is embedded so this stays compiling when the interface
// grows, without pretending to implement methods the handler never calls here.
type redactStore struct {
	AssessmentStore
	changed bool
	err     error
	gotUser string
	gotID   string
}

func (s *redactStore) RedactAssessment(_ context.Context, userID, id string) (bool, error) {
	s.gotUser, s.gotID = userID, id
	return s.changed, s.err
}

func deleteRequest(t *testing.T, h *Handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodDelete, "/analysis/"+id, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	r = r.WithContext(common.ContextWithClaims(
		context.WithValue(r.Context(), chi.RouteCtxKey, rctx),
		common.AuthClaims{UserID: "user-1"}))
	w := httptest.NewRecorder()
	h.Delete(w, r)
	return w
}

func TestDeleteRedactsAndReturns204(t *testing.T) {
	st := &redactStore{changed: true}
	w := deleteRequest(t, NewHandler(st, nil, nil), "rep-1")
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if st.gotUser != "user-1" || st.gotID != "rep-1" {
		t.Errorf("store called with (%q,%q), want (user-1,rep-1)", st.gotUser, st.gotID)
	}
}

// Idempotent on purpose: a second click, a double-submitted form or a retried
// request must not read as a failure. The data is already gone, which is the
// outcome the caller asked for.
func TestDeleteIsIdempotent(t *testing.T) {
	st := &redactStore{changed: false}
	if w := deleteRequest(t, NewHandler(st, nil, nil), "rep-1"); w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 for an already-redacted report", w.Code)
	}
}

// 404 rather than 403 for someone else's report: a 403 confirms the id exists,
// which is a fact the caller is not entitled to. GetAssessment already answers
// this way and the two must not disagree.
func TestDeleteHidesOtherPeoplesReports(t *testing.T) {
	st := &redactStore{err: ErrNoRows}
	if w := deleteRequest(t, NewHandler(st, nil, nil), "rep-1"); w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
```

Gerekli import'lar: `context`, `net/http`, `net/http/httptest`, `testing`, `github.com/go-chi/chi/v5`, ve `common` paketi. `common.ContextWithClaims`'in gerçek adı farklıysa `internal/common` içinden doğru yardımcıyı kullan — `ClaimsFromContext`'in karşılığı olan yazma yolu.

- [ ] **Step 2: Testleri koş, derlenmediğini gör**

Run: `cd mf-backend && go test ./internal/analysis/ -run TestDelete -v`
Expected: FAIL — `RedactAssessment` ve `h.Delete` tanımlı değil.

- [ ] **Step 3: Store metodunu yaz**

`mf-backend/internal/analysis/store.go`:

```go
// RedactAssessment blanks the personal columns of one report the caller owns.
//
// Two round trips in the miss case, and deliberately so. The UPDATE cannot tell
// "this row is not yours" from "you already did this": both change zero rows.
// Only the second is a success, and collapsing them would either 404 a
// legitimate repeat click or 204 a probe for someone else's report id.
func (s *Store) RedactAssessment(ctx context.Context, userID, id string) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE assessments
		    SET subject = '', subject_title = '', findings = '[]'::jsonb,
		        raw_response = '', redacted_at = now()
		  WHERE id = $1 AND user_id = $2 AND redacted_at IS NULL`,
		id, userID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}

	var one int
	err = s.db.QueryRow(ctx,
		`SELECT 1 FROM assessments WHERE id = $1 AND user_id = $2`, id, userID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNoRows
	}
	if err != nil {
		return false, err
	}
	return false, nil
}
```

- [ ] **Step 4: Arayüze ve handler'a ekle**

`mf-backend/internal/analysis/handler.go` — `AssessmentStore` arayüzüne:

```go
	RedactAssessment(ctx context.Context, userID, id string) (bool, error)
```

Ve `Get` handler'ının hemen altına:

```go
// Delete redacts one report. DELETE /analysis/{id}
//
// "Delete" in the URL and redaction in the database: the personal columns go,
// the measurement row stays. The name follows what the caller is asking for
// rather than what the storage does, because the caller's content really is
// gone and a route called /redact would describe our schema to someone who has
// no reason to know it.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	_, err := h.store.RedactAssessment(r.Context(), claims.UserID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("report not found"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not delete the report"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Rotayı bağla**

`mf-backend/internal/analysis/routes.go` — `Get("/{id}", h.Get)` satırının hemen altına, aynı grup içinde:

```go
		// Same wildcard caveat as the GET above: registered after the literal
		// paths, or "/trials/{group}" would be swallowed by "/{id}".
		pr.With(common.Timeout(defaultTimeout)).Delete("/{id}", h.Delete)
```

- [ ] **Step 6: Testleri koş**

Run: `cd mf-backend && go test ./internal/analysis/ -v`
Expected: PASS, üç yeni test dahil.

- [ ] **Step 7: Commit**

```bash
git add mf-backend/internal/analysis/
git commit -m "feat(kvkk): DELETE /analysis/{id} redacts the report it owns"
```

---

### Task 3: Süpürge SQL'i

**Files:**
- Modify: `mf-backend/internal/analysis/store.go`
- Modify: `mf-backend/internal/llm/store.go`

**Interfaces:**
- Consumes: Task 1'in sütunları.
- Produces: `(*analysis.Store).SweepAssessments(ctx context.Context, olderThan time.Time) (int64, error)` ve `(*llm.Store).SweepRuns(ctx context.Context, olderThan time.Time) (int64, error)`. İkisi de redakte edilen satır sayısını döner.

`llm` tarafında **`RedactRun` yazılmıyor**: spec onu listeliyordu ama çağıranı yok — `DELETE /llm/runs/{id}` satırı gerçekten siliyor ve değişmiyor. Çağrılmayan bir metot, test edilmemiş bir metottur.

- [ ] **Step 1: Süpürge metotlarını yaz**

`mf-backend/internal/analysis/store.go`:

```go
// SweepAssessments redacts every report older than olderThan.
//
// The same UPDATE the owner's own button runs, with an age predicate instead of
// an id. One statement, one meaning of "deleted": whatever the retention period
// does to a report is exactly what the user could have done sooner.
func (s *Store) SweepAssessments(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE assessments
		    SET subject = '', subject_title = '', findings = '[]'::jsonb,
		        raw_response = '', redacted_at = now()
		  WHERE created_at < $1 AND redacted_at IS NULL`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

`mf-backend/internal/llm/store.go`:

```go
// SweepRuns redacts every monitoring record older than olderThan.
//
// system_prompt goes with prompt and response: CreateRunRequest takes it from
// the frontend, so a user can put anything in it. Treating it as our own
// template would be an assumption about someone else's data.
func (s *Store) SweepRuns(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE llm_runs
		    SET prompt = '', response = '', system_prompt = '',
		        expected_keywords = '{}', redacted_at = now()
		  WHERE created_at < $1 AND redacted_at IS NULL`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

- [ ] **Step 2: Derle ve mevcut testleri koş**

Run: `cd mf-backend && go build ./... && go test ./...`
Expected: PASS. Bu adımda yeni test yok — SQL'in kendisi bu repoda test edilmiyor (Global Constraints). Davranış Task 4'te sahte süpürgelerle test ediliyor.

- [ ] **Step 3: Commit**

```bash
git add mf-backend/internal/analysis/store.go mf-backend/internal/llm/store.go
git commit -m "feat(kvkk): sweep statements, the same UPDATE with an age predicate"
```

---

### Task 4: `internal/retention` paketi

**Files:**
- Create: `mf-backend/internal/retention/retention.go`
- Test: `mf-backend/internal/retention/retention_test.go`

**Interfaces:**
- Consumes: Task 3'ün `SweepAssessments` / `SweepRuns` imzaları.
- Produces: `retention.Sweep(ctx context.Context, a AssessmentSweeper, r RunSweeper, age time.Duration, now time.Time) (Result, error)`, `retention.Result{Assessments, Runs int64}`, ve `AssessmentSweeper` / `RunSweeper` arayüzleri.

- [ ] **Step 1: Başarısız testleri yaz**

`mf-backend/internal/retention/retention_test.go`:

```go
package retention

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSweeper struct {
	n      int64
	err    error
	cutoff time.Time
	calls  int
}

func (f *fakeSweeper) SweepAssessments(_ context.Context, olderThan time.Time) (int64, error) {
	f.calls++
	f.cutoff = olderThan
	return f.n, f.err
}

func (f *fakeSweeper) SweepRuns(_ context.Context, olderThan time.Time) (int64, error) {
	f.calls++
	f.cutoff = olderThan
	return f.n, f.err
}

var now = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

func TestSweepPassesTheSameCutoffToBoth(t *testing.T) {
	a, r := &fakeSweeper{n: 3}, &fakeSweeper{n: 5}
	got, err := Sweep(context.Background(), a, r, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	want := now.Add(-30 * 24 * time.Hour)
	if !a.cutoff.Equal(want) || !r.cutoff.Equal(want) {
		t.Errorf("cutoffs %v / %v, want both %v", a.cutoff, r.cutoff, want)
	}
	if got.Assessments != 3 || got.Runs != 5 {
		t.Errorf("Result = %+v, want {3 5}", got)
	}
}

// One table failing must not spare the other. A sweep that gives up on the
// first error leaves personal data behind in a table that was perfectly
// healthy, and the next run an hour later hits the same broken one first.
func TestSweepRunsBothEvenWhenTheFirstFails(t *testing.T) {
	boom := errors.New("connection reset")
	a, r := &fakeSweeper{err: boom}, &fakeSweeper{n: 5}
	got, err := Sweep(context.Background(), a, r, time.Hour, now)
	if r.calls != 1 {
		t.Errorf("runs sweeper called %d times, want 1", r.calls)
	}
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to wrap %v", err, boom)
	}
	if got.Runs != 5 {
		t.Errorf("Result.Runs = %d, want the successful half reported anyway", got.Runs)
	}
}
```

- [ ] **Step 2: Testleri koş, derlenmediğini gör**

Run: `cd mf-backend && go test ./internal/retention/ -v`
Expected: FAIL — paket yok.

- [ ] **Step 3: Paketi yaz**

`mf-backend/internal/retention/retention.go`:

```go
// Package retention enforces the demo's storage limit: content older than the
// configured age is redacted, not deleted, so the measurement row survives its
// contents. See docs/superpowers/specs/2026-08-01-kvkk-ve-veri-silme-design.md.
package retention

import (
	"context"
	"errors"
	"time"
)

// AssessmentSweeper is the analysis store, declared here so this package does
// not import it — the dependency runs the other way at wiring time.
type AssessmentSweeper interface {
	SweepAssessments(ctx context.Context, olderThan time.Time) (int64, error)
}

// RunSweeper is the llm store.
type RunSweeper interface {
	SweepRuns(ctx context.Context, olderThan time.Time) (int64, error)
}

// Result is what one pass redacted, per table.
type Result struct {
	Assessments int64
	Runs        int64
}

// Sweep redacts everything older than age in both tables.
//
// now is a parameter rather than a call to time.Now, because the cutoff is the
// only interesting thing this function computes and a test that cannot pin it
// is testing nothing.
//
// Both sweepers always run. Errors are joined rather than returned early: the
// tables fail independently, and abandoning the second one would leave personal
// data in a table that had nothing wrong with it.
func Sweep(
	ctx context.Context,
	a AssessmentSweeper,
	r RunSweeper,
	age time.Duration,
	now time.Time,
) (Result, error) {
	cutoff := now.Add(-age)

	var res Result
	var errs []error

	n, err := a.SweepAssessments(ctx, cutoff)
	if err != nil {
		errs = append(errs, err)
	}
	res.Assessments = n

	m, err := r.SweepRuns(ctx, cutoff)
	if err != nil {
		errs = append(errs, err)
	}
	res.Runs = m

	return res, errors.Join(errs...)
}
```

- [ ] **Step 4: Testleri koş**

Run: `cd mf-backend && go test ./internal/retention/ -v`
Expected: PASS, iki test.

- [ ] **Step 5: Commit**

```bash
git add mf-backend/internal/retention/
git commit -m "feat(kvkk): retention sweep, with a cutoff a test can pin"
```

---

### Task 5: Yapılandırma ve arka plan işçisi

**Files:**
- Modify: `mf-backend/internal/config/config.go`
- Modify: `mf-backend/cmd/server/main.go`
- Modify: `render.yaml`
- Test: `mf-backend/internal/config/config_test.go` (yoksa oluştur)

**Interfaces:**
- Consumes: Task 4'ün `retention.Sweep`, Task 3'ün store metotları.
- Produces: `Config.RetentionDays int`, `Config.RetentionSweepInterval time.Duration`, `Config.RetentionEnabled() bool`.

- [ ] **Step 1: Yapılandırma testini yaz**

`mf-backend/internal/config/config_test.go` (dosya varsa sonuna ekle):

```go
// Env must be "production": Warnings() returns nil outside it by design, so a
// zero-value Config would pass this test while saying nothing at all.
func TestRetentionDisabledIsWarnedAbout(t *testing.T) {
	c := Config{Env: "production", RetentionDays: 0}
	if c.RetentionEnabled() {
		t.Error("RetentionEnabled() = true for 0 days")
	}
	var found bool
	for _, w := range c.Warnings() {
		if strings.Contains(w, "RETENTION_DAYS") {
			found = true
		}
	}
	if !found {
		t.Error("Warnings() says nothing about retention being off")
	}
}

func TestRetentionEnabledByDefaultValue(t *testing.T) {
	if !(Config{RetentionDays: 30}).RetentionEnabled() {
		t.Error("RetentionEnabled() = false for 30 days")
	}
}
```

- [ ] **Step 2: Testi koş, düştüğünü gör**

Run: `cd mf-backend && go test ./internal/config/ -run TestRetention -v`
Expected: FAIL — `RetentionDays` ve `RetentionEnabled` yok.

- [ ] **Step 3: Yapılandırmayı ekle**

`mf-backend/internal/config/config.go` — `Config` struct'ına:

```go
	RetentionDays          int
	RetentionSweepInterval time.Duration
```

`Load()` içine:

```go
		RetentionDays:          getInt("RETENTION_DAYS", 30),
		RetentionSweepInterval: getDuration("RETENTION_SWEEP_INTERVAL", 6*time.Hour),
```

Ve metotlar:

```go
// RetentionEnabled reports whether old content is swept at all.
//
// Zero is a legitimate setting for an operator running this on their own
// hardware, where the storage limit is their policy and not ours. On the demo
// it is a mistake, which is why it is a warning rather than a silent default.
func (c Config) RetentionEnabled() bool { return c.RetentionDays > 0 }
```

`Warnings()` içine, `return w` satırından önce (biriktirdiği dilimin adı `w`,
`Validate()`'in `problems`'ı değil):

```go
	if !c.RetentionEnabled() {
		w = append(w, "RETENTION_DAYS is 0: pasted case text, evidence quotes and "+
			"prompts are kept forever, and the privacy page promises they are not")
	}
```

`Warnings()` üretim dışında hiçbir şey döndürmüyor (`if !c.IsProduction() { return nil }`),
yani bu uyarı yalnızca gerçekten önemli olduğu yerde çıkıyor.

- [ ] **Step 4: Testi koş**

Run: `cd mf-backend && go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: İşçiyi main.go'ya bağla**

`mf-backend/cmd/server/main.go` — `go sessionCleanup(workerCtx, authStore)` satırının hemen altına:

```go
	// ---- background: redact content past the retention period ----
	if cfg.RetentionEnabled() {
		go retentionCleanup(workerCtx, analysisStore, llmStore,
			time.Duration(cfg.RetentionDays)*24*time.Hour, cfg.RetentionSweepInterval)
	}
```

Ve `sessionCleanup`'ın yanına, aynı biçimde:

```go
// retentionCleanup redacts content past the retention period, on the same
// shape as sessionCleanup: once at boot, then on a ticker, stopping with the
// worker context.
//
// Once at boot matters more here than for sessions. A deploy that has been down
// over the weekend comes back with rows already past their limit, and waiting a
// full interval to act on them would mean the stated period is only true when
// the process happens to have been running.
func retentionCleanup(
	ctx context.Context,
	a retention.AssessmentSweeper,
	r retention.RunSweeper,
	age, interval time.Duration,
) {
	sweep := func() {
		res, err := retention.Sweep(ctx, a, r, age, time.Now())
		if err != nil {
			slog.Error("retention sweep", "error", err)
		}
		if res.Assessments > 0 || res.Runs > 0 {
			slog.Info("retention sweep",
				"assessments", res.Assessments, "runs", res.Runs)
		}
	}

	sweep()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}
```

`analysisStore` ve `llmStore` değişkenlerinin main.go'daki gerçek adlarını kullan; ikisi de zaten handler'lara veriliyor.

- [ ] **Step 6: render.yaml'a ortam değişkenlerini ekle**

`render.yaml`, `CORS_ORIGINS` bloğunun altına:

```yaml
      # Hosted demo keeps pasted case text for 30 days, then blanks the personal
      # columns and leaves the measurement row. 0 disables the sweep entirely,
      # which is a legitimate setting on an operator's own hardware and a
      # mistake here — the server warns about it at boot.
      - key: RETENTION_DAYS
        value: "30"
      - key: RETENTION_SWEEP_INTERVAL
        value: 6h
```

- [ ] **Step 7: Derle ve tüm testleri koş**

Run: `cd mf-backend && go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add mf-backend/internal/config/ mf-backend/cmd/server/main.go render.yaml
git commit -m "feat(kvkk): run the sweep at boot and on a ticker"
```

---

### Task 6: Frontend — silme akışı ve redakte görünüm

**Files:**
- Create: `mf-frontend/src/lib/report.ts`
- Create: `mf-frontend/src/lib/report.test.ts`
- Modify: `mf-frontend/src/lib/types.ts`
- Modify: `mf-frontend/src/lib/api.ts`
- Modify: `mf-frontend/src/components/views/AnalizView.tsx`

**Interfaces:**
- Consumes: Task 2'nin `DELETE /analysis/{id}`, Task 1'in `redacted_at` JSON alanı.
- Produces: `isRedacted(x: { redacted_at: string | null }): boolean`, `reportTitle(x: { redacted_at: string | null; subject_title: string }): string`, `api.analysisDelete(id: string): Promise<void>`.

- [ ] **Step 1: Saf yardımcıların testini yaz**

`mf-frontend/src/lib/report.test.ts`:

```ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { isRedacted, reportTitle } from "./report.ts";

test("redakte rapor, boş başlıkla 'veri yok' gibi görünmez", () => {
  assert.equal(
    reportTitle({ redacted_at: "2026-08-01T12:00:00Z", subject_title: "" }),
    "İçerik silindi",
  );
});

test("redakte edilmemiş rapor kendi başlığını taşır", () => {
  assert.equal(
    reportTitle({ redacted_at: null, subject_title: "Acme tohum turu" }),
    "Acme tohum turu",
  );
});

// Başlıksız ama silinmemiş rapor üçüncü bir durum: silinmiş demek yanlış olur.
test("başlıksız ve silinmemiş rapor, silinmiş sayılmaz", () => {
  assert.equal(reportTitle({ redacted_at: null, subject_title: "" }), "Başlıksız");
  assert.equal(isRedacted({ redacted_at: null }), false);
});
```

- [ ] **Step 2: Testi koş, düştüğünü gör**

Run: `cd mf-frontend && npm test`
Expected: FAIL — `./report.ts` yok.

- [ ] **Step 3: Yardımcıları yaz**

`mf-frontend/src/lib/report.ts`:

```ts
/** Silinmiş bir raporun, boş gelmiş bir rapordan ayrılması. */

export function isRedacted(x: { redacted_at: string | null }): boolean {
  return x.redacted_at !== null;
}

// Üç durum var ve ikisi aynı boş dizeyle geliyor: içeriği silinmiş rapor,
// hiç başlık üretilememiş rapor. Ayıran tek şey redacted_at, ve ikisini
// birleştirmek kullanıcıya "verin kayıp" ile "verinizi sildik" arasındaki
// farkı kaybettirir.
export function reportTitle(
  x: { redacted_at: string | null; subject_title: string },
): string {
  if (isRedacted(x)) return "İçerik silindi";
  return x.subject_title || "Başlıksız";
}
```

- [ ] **Step 4: Testi koş**

Run: `cd mf-frontend && npm test`
Expected: PASS.

- [ ] **Step 5: Tipleri ve API istemcisini genişlet**

`mf-frontend/src/lib/types.ts` — `AssessmentSummary` ve `Assessment` arayüzlerine:

```ts
  redacted_at: string | null;
```

`mf-frontend/src/lib/api.ts` — `analysisGet` satırının altına:

```ts
  // 204 döner; request() zaten gövdesiz yanıtı undefined'a çeviriyor.
  analysisDelete: (id: string) =>
    request<void>(`/analysis/${id}`, { method: "DELETE" }),
```

- [ ] **Step 6: Liste satırını yeniden yapılandır ve silmeyi bağla**

`mf-frontend/src/components/views/AnalizView.tsx`, "Önceki raporlar" bölümü.

Satır şu anda bir `<button>`; içine ikinci bir düğme koymak geçersiz HTML ve klavye ile gezinmeyi bozar. Dış öğe `<div>` olur, açma eylemi kendi düğmesine iner:

```tsx
{history.map((h) => (
  <div
    key={h.id}
    className="card w-full p-3 flex flex-wrap items-center gap-x-4 gap-y-1"
  >
    <button
      className="card-action text-left flex-1 min-w-0 truncate text-sm"
      onClick={() =>
        api
          .analysisGet(h.id)
          .then(setAssessment)
          .catch(() => setRunError("Rapor açılamadı."))
      }
    >
      {reportTitle(h)}
    </button>

    {/* mevcut domain_name / kapsam / puan span'ları burada, değişmeden */}

    {!isRedacted(h) && (
      pendingDelete === h.id ? (
        <span className="flex items-center gap-2">
          <button className="btn btn-danger btn-sm" onClick={() => remove(h.id)}>
            Silinsin
          </button>
          <button className="btn btn-ghost btn-sm" onClick={() => setPendingDelete(null)}>
            Vazgeç
          </button>
        </span>
      ) : (
        <button
          className="btn btn-ghost btn-sm"
          onClick={() => setPendingDelete(h.id)}
        >
          Sil
        </button>
      )
    )}
  </div>
))}
```

Bileşenin üstüne durum ve eylem:

```tsx
const [pendingDelete, setPendingDelete] = useState<string | null>(null);

// İki adımlı onay, tarayıcının confirm() kutusu değil: bu arayüz koyu tema ve
// kendi diliyle konuşuyor, ve confirm() ikisini de terk ediyor.
//
// Silinen satır listeden çıkarılmıyor, redakte işaretleniyor. Neyin silindiğini
// göstermek, satırı yok etmekten dürüst — ve zaten sunucuda da olan bu.
const remove = useCallback(async (id: string) => {
  setPendingDelete(null);
  try {
    await api.analysisDelete(id);
  } catch {
    setRunError("Rapor silinemedi.");
    return;
  }
  const stamp = new Date().toISOString();
  setHistory((prev) =>
    prev.map((h) =>
      h.id === id ? { ...h, redacted_at: stamp, subject_title: "" } : h,
    ),
  );
  setAssessment((cur) =>
    cur && cur.id === id
      ? { ...cur, redacted_at: stamp, subject_title: "", subject: "", findings: [] }
      : cur,
  );
}, []);
```

`reportTitle` ve `isRedacted`'i `@/lib/report`'tan içe aktar.

- [ ] **Step 7: Açık raporun redakte hâlini göster**

`Rapor` bileşeninin en başına, bulgular render edilmeden önce:

```tsx
if (isRedacted(assessment)) {
  return (
    <article className="card mt-5 p-4">
      <h2 className="eyebrow">Rapor</h2>
      <p className="mt-2 text-sm">
        Bu raporun içeriği silindi. Puan ve kapsam kaydı duruyor, vaka metni ve
        kanıt alıntıları kaldırıldı.
      </p>
    </article>
  );
}
```

- [ ] **Step 8: Saklama süresi cümlesini listeye ekle**

`<h2 className="eyebrow">Önceki raporlar</h2>` satırının hemen altına:

```tsx
<p className="text-xs mt-1" style={{ color: "var(--text-faint)" }}>
  Rapor içerikleri 30 gün sonra otomatik silinir. Puan kaydı kalır.
</p>
```

- [ ] **Step 9: Lint, build ve testleri koş**

Run: `cd mf-frontend && npm test && npm run lint && npm run build`
Expected: hepsi temiz.

- [ ] **Step 10: Commit**

```bash
git add mf-frontend/src/lib/report.ts mf-frontend/src/lib/report.test.ts \
        mf-frontend/src/lib/types.ts mf-frontend/src/lib/api.ts \
        mf-frontend/src/components/views/AnalizView.tsx
git commit -m "feat(kvkk): delete a report, and show that it was deleted"
```

---

### Task 7: Gizlilik sayfası ve rotası

**Files:**
- Create: `mf-frontend/src/components/views/GizlilikView.tsx`
- Modify: `mf-frontend/src/components/AppShell.tsx`
- Modify: `mf-frontend/src/components/views/AuthView.tsx`

**Interfaces:**
- Consumes: yok.
- Produces: `#gizlilik` rotası ve `GizlilikView` bileşeni.

- [ ] **Step 1: Rotayı tanınır kıl**

`mf-frontend/src/components/AppShell.tsx`:

```tsx
export type MasterView =
  | "analiz" | "codegen" | "persona" | "metrics" | "admin" | "gizlilik";

// Nav'da olmayan ama adreslenebilen rotalar. isMaster bugüne kadar NAV
// üyeliğine bakıyordu; gizlilik bir çalışma aracı değil, bir belge — nav'a
// girmesi orayı sulandırır, ama derin bağlantının çalışması gerekiyor.
const OFF_NAV: MasterView[] = ["gizlilik"];

const isMaster = (v: string): v is MasterView =>
  NAV.some((n) => n.id === v) || (OFF_NAV as string[]).includes(v);
```

Görünüm anahtarlamasına, `{view === "admin" && ...}` satırının yanına:

```tsx
{view === "gizlilik" && <GizlilikView />}
```

Ve ana içeriğin altına bir alt bilgi (şu an hiç yok):

```tsx
<footer className="mt-10 pt-4 text-xs" style={{ color: "var(--text-faint)" }}>
  <a href="#gizlilik">Verileriniz ve gizlilik</a>
</footer>
```

- [ ] **Step 2: Sayfayı yaz**

`mf-frontend/src/components/views/GizlilikView.tsx`. Metin bilerek kısa ve somut; ürünün gerçekten yaptığını anlatıyor, yapmadığını vaat etmiyor:

```tsx
// Bu sayfa barındırdığımız demo'yu anlatır. Operatör kendi donanımına kurduğunda
// veri sorumlusu o olur ve bu metin onun için geçerli değildir.
export function GizlilikView() {
  return (
    <section className="max-w-2xl">
      <h1 className="text-lg">Verileriniz ve gizlilik</h1>

      <h2 className="eyebrow mt-6">Kim işliyor</h2>
      <p className="mt-2 text-sm">
        Bu demo MasterFabric tarafından işletiliyor. Girdiğiniz veriler bizim
        sunucumuzda saklanıyor.
      </p>

      <h2 className="eyebrow mt-6">Ne saklanıyor</h2>
      <ul className="mt-2 text-sm list-disc pl-5 space-y-1">
        <li>Analiz için yapıştırdığınız vaka metninin tamamı.</li>
        <li>Üretilen raporun bulguları ve kanıt alıntıları.</li>
        <li>Üreteç ekranında gönderdiğiniz istemler ve alınan yanıtlar.</li>
        <li>E-posta adresiniz ve oturum kayıtlarınız.</li>
      </ul>

      <h2 className="eyebrow mt-6">Ne için</h2>
      <p className="mt-2 text-sm">
        Raporu üretmek ve ürünün kendisini ölçmek için. Üçüncü taraflara
        aktarılmıyor, reklam için kullanılmıyor.
      </p>

      <h2 className="eyebrow mt-6">Ne kadar süreyle</h2>
      <p className="mt-2 text-sm">
        30 gün. Sonrasında vaka metni, kanıt alıntıları ve istemler
        kendiliğinden siliniyor; geriye puan, kapsam ve tarih gibi içeriği
        olmayan ölçüm kayıtları kalıyor. Bunun bir sonucu var ve saklamıyoruz:
        30 günden eski bir raporun puanını görebilirsiniz ama o puanın neye
        dayandığını artık gösteremiyoruz.
      </p>

      <h2 className="eyebrow mt-6">Daha erken silmek</h2>
      <p className="mt-2 text-sm">
        Analiz ekranındaki rapor listesinde her raporun yanında bir silme
        eylemi var. Bastığınızda 30. günde olacak şeyin aynısı hemen oluyor:
        içerik gidiyor, ölçüm kaydı kalıyor. Geri alınamıyor.
      </p>
      <p className="mt-2 text-sm">
        Bunun dışındaki talepler için henüz bir başvuru kanalımız yok. Demo
        dışında, gerçek verilerle kullanım için hazır olduğumuzu söylemiyoruz.
      </p>
    </section>
  );
}
```

- [ ] **Step 3: Giriş ekranına bağlantı koy**

`mf-frontend/src/components/views/AuthView.tsx` — formun altına, kaydolmadan önce görülebilsin diye:

```tsx
<p className="mt-4 text-xs" style={{ color: "var(--text-faint)" }}>
  <a href="#gizlilik">Verilerinizi nasıl sakladığımızı okuyun.</a>
</p>
```

`AuthView` oturum açılmadan render edildiği için `#gizlilik` bağlantısı `AppShell`'in rota anahtarlamasına ulaşmaz — `AuthView` içinde de aynı bağlantıya basıldığında `GizlilikView`'ı gösterecek küçük bir yerel durum gerekir:

```tsx
const [showPrivacy, setShowPrivacy] = useState(
  typeof window !== "undefined" && window.location.hash === "#gizlilik",
);
if (showPrivacy) {
  return (
    <div className="p-6">
      <button className="btn btn-ghost btn-sm" onClick={() => setShowPrivacy(false)}>
        ← Geri
      </button>
      <GizlilikView />
    </div>
  );
}
```

Ve bağlantının `onClick`'i `setShowPrivacy(true)` çağırsın.

- [ ] **Step 4: Lint, build ve testleri koş**

Run: `cd mf-frontend && npm test && npm run lint && npm run build`
Expected: hepsi temiz.

- [ ] **Step 5: Derin bağlantıyı elle doğrula**

Run: `cd mf-frontend && npm run dev`, tarayıcıda `http://localhost:3000/#gizlilik`
Expected: sayfa doğrudan açılıyor, giriş yapılmışsa alt bilgi bağlantısı da aynı yere gidiyor.

- [ ] **Step 6: Commit**

```bash
git add mf-frontend/src/components/views/GizlilikView.tsx \
        mf-frontend/src/components/AppShell.tsx \
        mf-frontend/src/components/views/AuthView.tsx
git commit -m "feat(kvkk): say what we keep, where it is, and how to remove it"
```

---

## Self-review notları

**Spec kapsamı.** Spec'in her bölümünün karşılığı var: migration ve sütunlar Task 1; redaksiyon ve uç nokta Task 2; süpürge SQL'i Task 3; işçi Task 4–5; frontend silme ve redakte görünüm Task 6; sayfa, rota ve metin Task 7.

**Spec'ten bilerek sapılan tek yer:** spec `llm.Store.RedactRun` istiyordu; yazılmıyor, çünkü çağıranı yok — `DELETE /llm/runs/{id}` satırı gerçekten siliyor ve bu iş kapsamında değişmiyor. Çağrılmayan metot test edilmeyen metottur.

**Test edilmeyen ne kaldı:** SQL ifadelerinin kendisi. Bu repoda veritabanı destekli test yok ve bu plan bir tane icat etmiyor. Yani "UPDATE doğru sütunları boşaltıyor mu" sorusunun otomatik cevabı yok; ilk deploy'da elle bir rapor silip sütunlara bakmak gerekiyor. Bunu bir eksiklik olarak biliyoruz, gizlemiyoruz.
