# Yönetim Paneli — 3. Aşama (Panel) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `GET /admin/stats?window=30d|90d` — panelin bütün serilerini tek yanıtta veren uç — ve `/yonetim` sayfasında dört değişimli kutu, zaman serileri, hesap dağılımı, aktivasyon hunisi, kohort tutunması ve tutarlılık kartı.

**Architecture:** Tüm sayılar Postgres'ten; Prometheus'a bağlı hiçbir şey bu ekrana girmiyor (spec §4). Rollup tablosu yok — SQL seyrek gruplanmış satır döndürür, gün omurgası ve kümülatif toplam **Go tarafında saf fonksiyonlarla** kurulur, böylece veritabanı olmadan test edilebilir. Frontend mevcut `TimeChart` ailesini büyütür; yeni chart bağımlılığı yok. `StatsPanel` mevcut `OverviewPanel`'in **üstüne** eklenir, onun yerine geçmez.

**Tech Stack:** Go 1.26.5 (chi, pgx/v5), Next.js 16 / React 19 / TypeScript, `node --experimental-strip-types --test src/lib/*.test.ts`. Yeni bağımlılık yok.

**Spec:** [`docs/superpowers/specs/2026-08-04-yonetim-paneli-design.md`](../specs/2026-08-04-yonetim-paneli-design.md) §4, §8 madde 3, §9 (`/admin/stats` + chart kova + huni testleri), §10 (backend önce).

## Global Constraints

- **Yeni npm / Go bağımlılığı yok.** Chart kütüphanesi özellikle yasak (spec §11) — `TimeChart` ailesi büyütülür.
- **Tek uç.** Altı chart için altı istek atılmaz; hepsi `GET /admin/stats` yanıtında (spec §4). Yarısı dolu yarısı boş bir panel yalan anlatır.
- **Prometheus yok.** Bu ekranda tek kaynak Postgres. GPU kutusu kapalıyken panel çalışmaya devam eder.
- **`OverviewPanel` silinmez.** Mevcut operasyonel sayılar (p95, adapter build, şema uyumu 24s) yerinde kalır; `StatsPanel` onun üstünde durur.
- **Boş veritabanında sıfır, çökme yok** (spec §9). Payda sıfırken oran `NaN` değil `0` ya da "veri yok".
- **Arayüz metinleri Türkçe.** Atölye dili yasak (GPU, kart, tünel, eğitim koşusu). "AI karar verir" / "en iyi model" / "otomatik" yasak (CLAUDE.md).
- **Dark-only;** renkler `globals.css` custom property'lerinden. Seri paleti `--series-1` / `--series-2` — iki renk, döngüsüz.
- **Frontend testleri** yalnızca `src/lib/*.test.ts` (saf mantık), uzantılı import: `from "./stats.ts"`.
- **Backend testleri** sahte store + `httptest` (`internal/admin/accounts_test.go` deseni); `cd mf-backend && go test ./...`.
- **Zaman dilimi UTC.** Gün kovaları `date_trunc('day', created_at)` ile UTC sınırında; repodaki her damga zaten UTC.
- **Yeni migration yok.** 013 ve 014 spec §5/§6'ya (belgeler, denetim kaydı) ayrılmış — bu aşama numara tüketmez. Yeni indeks de yok: gerekçe spec §4'ün rollup gerekçesiyle aynı, bu ölçekte tarama ucuz.
- **Zaman aşımı:** `/admin` yerel grubu `REQUEST_TIMEOUT` (varsayılan **5s**) altında (`cmd/server/main.go:277`, `internal/admin/routes.go:50`). Go tuzağı: alt context ebeveyninin süresini uzatamaz — handler kendi `context.WithTimeout`'unu **kurmaz**, `r.Context()`'i taşır. `Stats` en fazla 6 sorgu atar, hiçbiri satır başına döngü içermez.
- **Commit mesajları:** WHY ilk satır ≤72 karakter; gövde nedeni açıklar; son satır `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- **Deploy:** Backend önce, sonra frontend (spec §10). Push yarım sürümdür — Render elle tetiklenir.

---

## File Structure

| Dosya | Sorumluluk |
|---|---|
| `mf-backend/internal/admin/stats.go` | **Yeni.** Yanıt modelleri, pencere ayrıştırma, `Stats` handler. |
| `mf-backend/internal/admin/stats_store.go` | **Yeni.** Postgres sorguları + gün omurgası / kümülatif / kohort saf yardımcıları. |
| `mf-backend/internal/admin/stats_test.go` | **Yeni.** Sahte store handler testleri + saf yardımcı testleri. |
| `mf-backend/internal/admin/handler.go` | **Değişir.** `StatsStore` tüketici arayüzü + `Handler.stats` alanı. |
| `mf-backend/internal/admin/routes.go` | **Değişir.** `r.Get("/stats", h.Stats)` yerel gruba. |
| `mf-backend/cmd/server/main.go` | **Değişir.** `admin.New(...)`'a stats store bağlanır. |
| `mf-frontend/src/lib/types.ts` | **Değişir.** `AdminStats` ve alt tipleri. |
| `mf-frontend/src/lib/api.ts` | **Değişir.** `api.admin.stats(window)`. |
| `mf-frontend/src/lib/stats.ts` | **Yeni.** Saf yardımcılar: değişim yüzdesi, seri dönüşümü, huni oranı, kohort olgunluğu, seri kırpma. |
| `mf-frontend/src/lib/stats.test.ts` | **Yeni.** `node --test` ile saf mantık testleri. |
| `mf-frontend/src/components/ui/TimeChart.tsx` | **Değişir.** `formatValue`'ya `percent` birimi. |
| `mf-frontend/src/components/ui/Breakdown.tsx` | **Yeni.** `ShareBar`, `FunnelBars`, `CohortGrid` — SVG/CSS, kütüphanesiz. |
| `mf-frontend/src/components/yonetim/StatsPanel.tsx` | **Yeni.** Kutular + chart'lar + tutarlılık kartı. |
| `mf-frontend/src/app/yonetim/page.tsx` | **Değişir.** `StatsPanel` üstte, `OverviewPanel` altında. |

---

## Sözleşme (bütün görevlerin paylaştığı tipler)

Bu blok Task 1'de yazılır; sonraki görevler **birebir bu adları** kullanır.

```go
// mf-backend/internal/admin/stats.go
type StatBox struct {
	Value     float64  `json:"value"`
	Previous  float64  `json:"previous"`
	ChangePct *float64 `json:"change_pct"` // Previous == 0 iken nil: taban yoksa yüzde uydurmuyoruz
}

type ActiveAdapterBox struct {
	Name          string   `json:"name"`            // "" = aktif adapter yok
	ValidRate     float64  `json:"valid_rate"`      // 0..1, pencere içi şema uyumu
	PreviousRate  float64  `json:"previous_rate"`   // 0..1, önceki pencere
	ChangePoints  *float64 `json:"change_points"`   // yüzde PUANI farkı; iki pencereden biri boşsa nil
}

type StatsBoxes struct {
	TotalUsers     StatBox          `json:"total_users"`      // Value = şimdiki toplam, Previous = pencere başındaki toplam
	TotalReports   StatBox          `json:"total_reports"`    // aynı mantık, assessments
	ReportsLast24h StatBox          `json:"reports_last_24h"` // Value = son 24s, Previous = ondan önceki 24s
	ActiveAdapter  ActiveAdapterBox `json:"active_adapter"`
}

type DayPoint struct {
	T               int64 `json:"t"`                // UTC gün başı, unix saniye
	NewUsers        int   `json:"new_users"`
	CumulativeUsers int   `json:"cumulative_users"` // pencere öncesi taban + koşan toplam
	Assessments     int   `json:"assessments"`
	SchemaValid     int   `json:"schema_valid"`     // o günün geçerli şema sayısı (payı)
}

type SeriesPoint struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

type TargetSeries struct {
	Target string        `json:"target"` // "browser" | "server"
	Points []SeriesPoint `json:"points"`
}

type CategoryCount struct {
	Key   string `json:"key"`   // "individual" | "company"
	Count int    `json:"count"`
}

type Funnel struct {
	Registered int `json:"registered"` // pencerede kaydolan
	Consented  int `json:"consented"`  // onları içinden koşulları kabul etmiş
	Analyzed   int `json:"analyzed"`   // onların içinden en az bir analiz üretmiş
}

type CohortRow struct {
	WeekStart   int64 `json:"week_start"`   // UTC hafta başı, unix saniye
	Size        int   `json:"size"`
	Week2       int   `json:"week_2"`       // 7-14. günlerde analiz üreten
	Week4       int   `json:"week_4"`       // 21-28. günlerde analiz üreten
	MatureWeeks int   `json:"mature_weeks"` // kohortun üstünden geçen tam hafta
}

type ConsistencyCard struct {
	Group             string    `json:"group"`
	Runs              int       `json:"runs"`
	CreatedAt         time.Time `json:"created_at"`
	TotalSpread       float64   `json:"total_spread"` // en yüksek - en düşük ağırlıklı toplam
	MinTotal          float64   `json:"min_total"`
	MaxTotal          float64   `json:"max_total"`
	VolatileCriterion string    `json:"volatile_criterion"`
	VolatileStdDev    float64   `json:"volatile_std_dev"`
}

type StatsResponse struct {
	Window       string           `json:"window"` // "30d" | "90d"
	From         time.Time        `json:"from"`
	To           time.Time        `json:"to"`
	Boxes        StatsBoxes       `json:"boxes"`
	Days         []DayPoint       `json:"days"`      // pencerenin HER günü, boş günler sıfırla
	OrgTypes     []CategoryCount  `json:"org_types"`
	RunsByTarget []TargetSeries   `json:"runs_by_target"`
	Funnel       Funnel           `json:"funnel"`
	Cohorts      []CohortRow      `json:"cohorts"`
	Consistency  *ConsistencyCard `json:"consistency"` // trial grubu yoksa null
}
```

Store arayüzü (tüketici tarafında, `internal/admin/handler.go`):

```go
type StatsStore interface {
	Stats(ctx context.Context, from, to time.Time) (StatsResponse, error)
}
```

`Stats` store metodu `Window` alanını doldurmaz — onu handler yazar.

---

### Task 1: Uç iskeleti — modeller, pencere ayrıştırma, handler

**Files:**
- Create: `mf-backend/internal/admin/stats.go`
- Create: `mf-backend/internal/admin/stats_store.go` (yalnızca boş `Stats` metodu; gövdesi Task 2'de)
- Create: `mf-backend/internal/admin/stats_test.go`
- Modify: `mf-backend/internal/admin/handler.go` (arayüz + `Handler.stats` alanı + `New(...)` imzası)
- Modify: `mf-backend/internal/admin/routes.go` (`localRoutes` içine `r.Get("/stats", h.Stats)`)
- Modify: `mf-backend/cmd/server/main.go` (store'u `admin.New`'a geçir)

**Interfaces:**
- Consumes: `common.JSON`, `common.Error`, `common.ErrBadRequest`, `common.ErrInternal` (`internal/common/response.go`, `errors.go`).
- Produces: yukarıdaki **Sözleşme** bloğunun tamamı; `parseWindow(raw string) (days int, label string, err error)`; `StatsStore` arayüzü; `Handler.Stats(w, r)`.

- [x] **Step 1: Sözleşme tiplerini yaz**

`mf-backend/internal/admin/stats.go` dosyasını aç, yukarıdaki **Sözleşme** bloğundaki Go tiplerini birebir yaz (alan adları, JSON etiketleri ve yorumlar dahil). Dosyanın başına neden tek uç olduğunu anlatan kısa bir yorum koy — spec §4: altı chart için altı istek, biri zaman aşımına uğradığında panelin yarısını yalan yapar.

- [x] **Step 2: `parseWindow` için başarısız test yaz**

`mf-backend/internal/admin/stats_test.go`:

```go
func TestParseWindow(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		days    int
		label   string
		wantErr bool
	}{
		{"", 30, "30d", false},
		{"30d", 30, "30d", false},
		{"90d", 90, "90d", false},
		{"7d", 0, "", true},
		{"30", 0, "", true},
		{"abc", 0, "", true},
	} {
		days, label, err := parseWindow(tc.raw)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%q: err = %v, wantErr %v", tc.raw, err, tc.wantErr)
		}
		if err == nil && (days != tc.days || label != tc.label) {
			t.Fatalf("%q: got (%d, %q), want (%d, %q)", tc.raw, days, label, tc.days, tc.label)
		}
	}
}
```

- [x] **Step 3: Testi çalıştır, düştüğünü gör**

Run: `cd mf-backend && go test ./internal/admin/ -run TestParseWindow`
Expected: derleme hatası — `undefined: parseWindow`.

- [x] **Step 4: `parseWindow`'u yaz**

```go
// Yalnızca iki pencere var, ve serbest bir gün sayısı kabul etmiyoruz: her
// pencere aynı uzunlukta bir "önceki pencere" ile karşılaştırılıyor, ve
// keyfi aralık o karşılaştırmayı kullanıcıya sessizce anlamsızlaştırır.
func parseWindow(raw string) (int, string, error) {
	switch raw {
	case "", "30d":
		return 30, "30d", nil
	case "90d":
		return 90, "90d", nil
	default:
		return 0, "", common.ErrBadRequest("window must be 30d or 90d")
	}
}
```

- [x] **Step 5: Handler testlerini yaz (sahte store)**

`stats_test.go`'ye ekle — `accounts_test.go:20-98` desenini izle (sahte struct + `localRoutes` ile router):

```go
type fakeStatsStore struct {
	from, to time.Time
	res      StatsResponse
	err      error
}

func (f *fakeStatsStore) Stats(_ context.Context, from, to time.Time) (StatsResponse, error) {
	f.from, f.to = from, to
	return f.res, f.err
}

func TestStatsDefaultsToThirtyDayWindow(t *testing.T) {
	store := &fakeStatsStore{}
	h := &Handler{stats: store}
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	statsRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var res StatsResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Window != "30d" {
		t.Fatalf("window = %q, want 30d", res.Window)
	}
	if got := store.to.Sub(store.from); got != 30*24*time.Hour {
		t.Fatalf("store window = %v, want 720h", got)
	}
	// Boş veritabanı sözleşmesi: diziler null değil boş dizi olarak serileşmeli,
	// yoksa frontend .map çağrısında patlar.
	if res.Days == nil || res.OrgTypes == nil || res.RunsByTarget == nil || res.Cohorts == nil {
		t.Fatalf("empty store must produce empty slices, not null: %+v", res)
	}
}

func TestStatsRejectsUnknownWindow(t *testing.T) {
	h := &Handler{stats: &fakeStatsStore{}}
	req := httptest.NewRequest(http.MethodGet, "/stats?window=7d", nil)
	w := httptest.NewRecorder()

	statsRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestStatsPassesNinetyDayWindowToStore(t *testing.T) {
	store := &fakeStatsStore{}
	h := &Handler{stats: store}
	req := httptest.NewRequest(http.MethodGet, "/stats?window=90d", nil)
	w := httptest.NewRecorder()

	statsRouter(h).ServeHTTP(w, req)

	if got := store.to.Sub(store.from); got != 90*24*time.Hour {
		t.Fatalf("store window = %v, want 2160h", got)
	}
}
```

`statsRouter` test yardımcısını `accounts_test.go:94-98`'deki `accountsRouter` gibi yaz: `chi.NewRouter()` + `r.Get("/stats", h.Stats)`.

- [x] **Step 6: Handler'ı ve bağlantıları yaz**

`stats.go`'ye:

```go
// Stats panelin tamamını tek yanıtta verir. GET /admin/stats?window=30d|90d
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	days, label, err := parseWindow(r.URL.Query().Get("window"))
	if err != nil {
		common.Error(w, err)
		return
	}
	// Gün sınırına hizala: kova sınırı isteğin saatine göre kayarsa aynı gün
	// iki ardışık istekte farklı sayı gösterir.
	to := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	from := to.Add(-time.Duration(days) * 24 * time.Hour)

	// Kendi context'ini KURMA: alt context ebeveyninin süresini uzatamaz, ve
	// /admin yerel grubu zaten REQUEST_TIMEOUT altında.
	res, err := h.stats.Stats(r.Context(), from, to)
	if err != nil {
		common.Error(w, common.ErrInternal("could not read stats"))
		return
	}
	res.Window, res.From, res.To = label, from, to
	// Boş dilim null olarak serileşir ve frontend'in .map çağrısı patlar. Bunu
	// store'a bırakmıyoruz: sözleşme uçta, her store yolunda geçerli olmalı.
	normalizeStats(&res)
	common.JSON(w, http.StatusOK, res)
}

// normalizeStats nil dilimleri boş dilime çevirir.
func normalizeStats(res *StatsResponse) {
	if res.Days == nil {
		res.Days = []DayPoint{}
	}
	if res.OrgTypes == nil {
		res.OrgTypes = []CategoryCount{}
	}
	if res.RunsByTarget == nil {
		res.RunsByTarget = []TargetSeries{}
	}
	if res.Cohorts == nil {
		res.Cohorts = []CohortRow{}
	}
}
```

`handler.go`: `StatsStore` arayüzünü diğer tüketici arayüzlerinin (`AccountStore`, `handler.go:50-60`) yanına ekle; `Handler` struct'ına `stats StatsStore` alanı; `New(...)` imzasına parametre. `routes.go`: `localRoutes` içine `r.Get("/stats", h.Stats)` — `/overview` satırının hemen altına, **yerel (timeout'lu) gruba**, `/metrics` grubuna değil. `cmd/server/main.go`: mevcut `*admin.Store`'u yeni parametreye geçir. Bu görevde `Store`'a `stats_store.go`'da yalnızca imzası doğru, boş bir `Stats` metodu eklenir (`return StatsResponse{}, nil`) — Task 2 aynı metodun gövdesini gerçek sorgularla doldurur. `nil` store geçme: derleme geçer, ilk istek panic eder.

- [x] **Step 7: Testler ve derleme**

```bash
cd mf-backend && go build ./... && go test ./internal/admin/
```
Expected: PASS.

- [x] **Step 8: Commit**

```bash
git add mf-backend/internal/admin/stats.go mf-backend/internal/admin/stats_store.go \
        mf-backend/internal/admin/stats_test.go mf-backend/internal/admin/handler.go \
        mf-backend/internal/admin/routes.go mf-backend/cmd/server/main.go
git commit -m "$(cat <<'EOF'
Serve the whole panel from one endpoint so it cannot half-load

Six charts behind six requests means one timeout renders a dashboard that
is half full and half empty — which reads as a fact, not as a failure.

Window is 30d or 90d only: every box compares against a prior window of
equal length, and an arbitrary range makes that comparison quietly
meaningless.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Store — kutular, günlük seriler, hesap dağılımı, hedef kırılımı

**Files:**
- Modify: `mf-backend/internal/admin/stats_store.go` (Task 1'in boş `Stats` metodunun gövdesi)
- Modify: `mf-backend/internal/admin/stats_test.go` (saf yardımcı testleri)

**Interfaces:**
- Consumes: Task 1'in `StatsResponse`, `DayPoint`, `StatBox`, `CategoryCount`, `TargetSeries`, `SeriesPoint`, `ActiveAdapterBox` tipleri; `Store` (`internal/admin/store.go:21-26`, `db *pgxpool.Pool`, pgx/v5).
- Produces: `func (s *Store) Stats(ctx context.Context, from, to time.Time) (StatsResponse, error)` (Task 3 aynı metodu huni + kohort ile tamamlar); saf yardımcılar `daySpine`, `assembleDays`, `changePct`, `assembleTargets`.

**Neden Go'da birleştiriyoruz:** SQL seyrek gruplanmış satır döndürür, gün omurgası ve kümülatif toplam Go'da kurulur. Böylece kova mantığı canlı Postgres olmadan test edilir — repoda veritabanı gerektiren test yok.

- [x] **Step 1: Saf yardımcılar için başarısız testleri yaz**

`stats_test.go`'ye ekle:

```go
func TestDaySpineCoversEveryDayOfWindow(t *testing.T) {
	to := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -30)
	spine := daySpine(from, to)
	if len(spine) != 30 {
		t.Fatalf("len = %d, want 30", len(spine))
	}
	if spine[0] != from.Unix() {
		t.Fatalf("first = %d, want %d", spine[0], from.Unix())
	}
	if spine[29] != to.AddDate(0, 0, -1).Unix() {
		t.Fatalf("last = %d, want %d", spine[29], to.AddDate(0, 0, -1).Unix())
	}
}

func TestAssembleDaysZeroFillsAndAccumulates(t *testing.T) {
	to := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -3)
	spine := daySpine(from, to)
	newUsers := map[int64]int{spine[0]: 2, spine[2]: 1} // ortadaki gün boş
	days := assembleDays(spine, 10, newUsers, map[int64]int{spine[2]: 5}, map[int64]int{spine[2]: 4})

	if len(days) != 3 {
		t.Fatalf("len = %d, want 3", len(days))
	}
	if days[1].NewUsers != 0 || days[1].Assessments != 0 {
		t.Fatalf("gap day must be zero, got %+v", days[1])
	}
	want := []int{12, 12, 13}
	for i, w := range want {
		if days[i].CumulativeUsers != w {
			t.Fatalf("day %d cumulative = %d, want %d", i, days[i].CumulativeUsers, w)
		}
	}
	if days[2].SchemaValid != 4 || days[2].Assessments != 5 {
		t.Fatalf("day 2 = %+v", days[2])
	}
}

func TestChangePctIsNilWithoutABaseline(t *testing.T) {
	if got := changePct(5, 0); got != nil {
		t.Fatalf("no baseline must give nil, got %v", *got)
	}
	got := changePct(150, 100)
	if got == nil || *got != 50 {
		t.Fatalf("want +50, got %v", got)
	}
	down := changePct(50, 100)
	if down == nil || *down != -50 {
		t.Fatalf("want -50, got %v", down)
	}
}

func TestAssembleTargetsZeroFillsEachTarget(t *testing.T) {
	to := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	spine := daySpine(to.AddDate(0, 0, -2), to)
	series := assembleTargets(spine, map[string]map[int64]int{
		"server":  {spine[1]: 3},
		"browser": {spine[0]: 1},
	})
	if len(series) != 2 || series[0].Target != "browser" {
		t.Fatalf("targets must be sorted for a stable colour order: %+v", series)
	}
	for _, s := range series {
		if len(s.Points) != 2 {
			t.Fatalf("%s: %d points, want 2", s.Target, len(s.Points))
		}
	}
	if series[1].Points[1].V != 3 {
		t.Fatalf("server day 2 = %v, want 3", series[1].Points[1].V)
	}
}
```

- [x] **Step 2: Testleri çalıştır, düştüklerini gör**

Run: `cd mf-backend && go test ./internal/admin/ -run 'TestDaySpine|TestAssemble|TestChangePct'`
Expected: derleme hatası — `undefined: daySpine` vb.

- [x] **Step 3: Saf yardımcıları yaz**

`stats_store.go` içinde:

- `daySpine(from, to time.Time) []int64` — `from`'dan başlayıp `to`'ya kadar (dahil değil) 24 saatlik adımlarla UTC unix saniyeleri.
- `assembleDays(spine []int64, baseUsers int, newUsers, assessments, schemaValid map[int64]int) []DayPoint` — omurganın her günü için satır üretir, eksik anahtarlar sıfır olur, `CumulativeUsers` `baseUsers` üstüne koşan toplamla ilerler.
- `changePct(current, previous float64) *float64` — `previous == 0` iken `nil`; aksi hâlde `(current-previous)/previous*100`, iki ondalığa yuvarlanmış.
- `assembleTargets(spine []int64, byTarget map[string]map[int64]int) []TargetSeries` — hedef adına göre **sıralı** döner (renk sırası istekten isteğe değişmesin), her seri omurganın tamamını taşır.

`assembleDays` boş omurgada `[]DayPoint{}` döndürmeli, `nil` değil — Task 1'in "null değil boş dizi" sözleşmesi.

- [x] **Step 4: Testleri çalıştır, geçtiklerini gör**

Run: `cd mf-backend && go test ./internal/admin/ -run 'TestDaySpine|TestAssemble|TestChangePct' -v`
Expected: PASS.

- [x] **Step 5: SQL'i yaz**

`(s *Store) Stats(ctx, from, to)` — sorgu sayısı bütçesi 5s'lik istek zaman aşımı altında; **satır başına sorgu yok**, her şey gruplanmış toplama.

Önceki pencere: `priorFrom := from.Add(-to.Sub(from))`.

1. **Kutular — tek `QueryRow`**, `Overview` (`store.go:246-270`) desenindeki alt-select yığını:

```sql
SELECT
    (SELECT count(*) FROM users),
    (SELECT count(*) FROM users WHERE created_at < $1),
    (SELECT count(*) FROM assessments),
    (SELECT count(*) FROM assessments WHERE created_at < $1),
    (SELECT count(*) FROM assessments WHERE created_at > now() - interval '24 hours'),
    (SELECT count(*) FROM assessments
       WHERE created_at > now() - interval '48 hours'
         AND created_at <= now() - interval '24 hours'),
    coalesce((SELECT a.name FROM llm_settings s
                LEFT JOIN llm_adapters a ON a.id = s.active_adapter_id
               WHERE s.id = 1), ''),
    (SELECT count(*) FROM assessments WHERE created_at >= $1 AND created_at < $2),
    (SELECT coalesce(avg(schema_valid::int), 0) FROM assessments
       WHERE created_at >= $1 AND created_at < $2),
    (SELECT count(*) FROM assessments WHERE created_at >= $3 AND created_at < $1),
    (SELECT coalesce(avg(schema_valid::int), 0) FROM assessments
       WHERE created_at >= $3 AND created_at < $1)
```

`$1 = from`, `$2 = to`, `$3 = priorFrom`. Adapter adı `coalesce` **dışarıda**: `llm_settings` satırı hiç yoksa alt-select NULL döner ve `string`'e tarama patlar.

`ActiveAdapter.ChangePoints` yalnızca **iki pencerede de en az bir analiz varsa** dolar; biri boşken oran karşılaştırması uydurma bir sinyal üretir.

`TotalUsers`/`TotalReports` kutuları kümülatif: `Value` = şimdiki toplam, `Previous` = pencere başındaki toplam, yani yüzde pencere içindeki büyüme.

2. **Günlük yeni kayıt:**

```sql
SELECT date_trunc('day', created_at AT TIME ZONE 'UTC'), count(*)
  FROM users WHERE created_at >= $1 AND created_at < $2 GROUP BY 1
```

3. **Günlük analiz + şema geçerliliği:**

```sql
SELECT date_trunc('day', created_at AT TIME ZONE 'UTC'),
       count(*), count(*) FILTER (WHERE schema_valid)
  FROM assessments WHERE created_at >= $1 AND created_at < $2 GROUP BY 1
```

4. **Hesap dağılımı** — pencereden bağımsız anlık dağılım, çünkü bu bir hacim değil bileşim ölçüsü:

```sql
SELECT type, count(*) FROM organizations GROUP BY 1 ORDER BY 1
```

5. **Hedef kırılımlı çalışma hacmi:**

```sql
SELECT target, date_trunc('day', created_at AT TIME ZONE 'UTC'), count(*)
  FROM llm_runs WHERE created_at >= $1 AND created_at < $2 GROUP BY 1, 2
```

Her `Query` sonrası `rows.Err()` kontrol edilir (pgx'te `Next` false döndüğünde hata sessizce yutulur). Sonuçlar `map[int64]int`'lere toplanıp Step 3'ün yardımcılarına verilir. `OrgTypes` ve `RunsByTarget` boşken `nil` değil boş dilim döner.

- [x] **Step 6: Derle ve bütün paketi test et**

```bash
cd mf-backend && go build ./... && go test ./internal/admin/
```
Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add mf-backend/internal/admin/stats_store.go mf-backend/internal/admin/stats_test.go
git commit -m "$(cat <<'EOF'
Build the day buckets in Go so they can be tested without a database

Postgres returns only the days that have rows. A chart drawn straight from
that is a lie of omission: quiet days vanish and the line reads as if
nothing happened between them.

Filling the spine and accumulating in Go keeps the bucket logic under the
test suite this repo can actually run, and leaves SQL doing the one thing
it is better at.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Store — aktivasyon hunisi ve kohort tutunması

**Files:**
- Modify: `mf-backend/internal/admin/stats_store.go`
- Modify: `mf-backend/internal/admin/stats_test.go`

**Interfaces:**
- Consumes: Task 1'in `Funnel`, `CohortRow` tipleri; Task 2'nin `Stats` metodu ve `daySpine`.
- Produces: `Stats` yanıtının `Funnel` ve `Cohorts` alanları; saf yardımcı `matureWeeks(weekStart int64, now time.Time) int`.

**Neden bu ikisi:** spec §4 — üye sayısı büyürken aktivasyon düşüyorsa büyüme sahtedir. Üye grafiği bu ikisi olmadan yanıltıcıdır.

- [x] **Step 1: `matureWeeks` için başarısız test yaz**

```go
func TestMatureWeeksCountsOnlyElapsedWeeks(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	week := func(d int) int64 { return now.AddDate(0, 0, -d).Truncate(24 * time.Hour).Unix() }

	if got := matureWeeks(week(3), now); got != 0 {
		t.Fatalf("3 days old = %d weeks, want 0", got)
	}
	if got := matureWeeks(week(15), now); got != 2 {
		t.Fatalf("15 days old = %d weeks, want 2", got)
	}
	// Gelecekteki bir hafta başı negatif dönmemeli.
	if got := matureWeeks(now.AddDate(0, 0, 7).Unix(), now); got != 0 {
		t.Fatalf("future cohort = %d, want 0", got)
	}
}
```

- [x] **Step 2: Testi çalıştır, düştüğünü gör**

Run: `cd mf-backend && go test ./internal/admin/ -run TestMatureWeeks`
Expected: `undefined: matureWeeks`.

- [x] **Step 3: `matureWeeks`'i yaz**

```go
// Kaç tam hafta geçtiği, kohortun 2. ve 4. hafta hücresinin okunabilir olup
// olmadığını belirler. Üç günlük bir kohortun 4. hafta tutunması %0 DEĞİLDİR,
// henüz yoktur — ve %0 basmak, ürünü tutunmuyor gibi gösteren bir yalandır.
func matureWeeks(weekStart int64, now time.Time) int {
	elapsed := now.Unix() - weekStart
	if elapsed <= 0 {
		return 0
	}
	return int(elapsed / (7 * 24 * 3600))
}
```

- [x] **Step 4: Huni ve kohort SQL'ini `Stats`'a ekle**

6. **Huni** — pencerede kaydolanlar üzerinden, tek satır:

```sql
SELECT count(*),
       count(*) FILTER (WHERE u.terms_accepted_at IS NOT NULL),
       count(*) FILTER (WHERE EXISTS (
           SELECT 1 FROM assessments a WHERE a.user_id = u.id))
  FROM users u WHERE u.created_at >= $1 AND u.created_at < $2
```

7. **Kohort** — haftalık kohort, tutunma **kullanıcının kendi kayıt anına** göre ölçülür (hafta başına göre değil; aynı kohortun pazartesi ve cuma üyeleri aksi hâlde farklı uzunlukta pencere alır):

```sql
SELECT date_trunc('week', u.created_at AT TIME ZONE 'UTC') AS w,
       count(*),
       count(*) FILTER (WHERE EXISTS (
           SELECT 1 FROM assessments a WHERE a.user_id = u.id
             AND a.created_at >= u.created_at + interval '7 days'
             AND a.created_at <  u.created_at + interval '14 days')),
       count(*) FILTER (WHERE EXISTS (
           SELECT 1 FROM assessments a WHERE a.user_id = u.id
             AND a.created_at >= u.created_at + interval '21 days'
             AND a.created_at <  u.created_at + interval '28 days'))
  FROM users u WHERE u.created_at >= $1 AND u.created_at < $2
 GROUP BY 1 ORDER BY 1
```

Her satırın `MatureWeeks` alanı `matureWeeks(weekStart, time.Now().UTC())` ile doldurulur. `Cohorts` boşken boş dilim.

- [x] **Step 5: Testler ve derleme**

```bash
cd mf-backend && go build ./... && go test ./internal/admin/
```
Expected: PASS. Task 1'in sahte store testleri hâlâ geçmeli — `Funnel` sıfır değerleriyle serileşir, `Cohorts` boş dizi.

- [x] **Step 6: Commit**

```bash
git add mf-backend/internal/admin/stats_store.go mf-backend/internal/admin/stats_test.go
git commit -m "$(cat <<'EOF'
Measure activation and retention, or the growth line means nothing

A rising member count with falling activation is not growth, and the
membership chart alone cannot tell the difference.

Retention is measured from each user's own signup instant rather than
their cohort's Monday: otherwise the Friday half of a cohort is judged on
a shorter window than the Monday half.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Frontend sözleşmesi — tipler, API istemcisi, saf yardımcılar

**Files:**
- Modify: `mf-frontend/src/lib/types.ts`
- Modify: `mf-frontend/src/lib/api.ts`
- Create: `mf-frontend/src/lib/stats.ts`
- Create: `mf-frontend/src/lib/stats.test.ts`

**Interfaces:**
- Consumes: Task 1'in JSON sözleşmesi (alan adları birebir); `MetricSeries` / `MetricPoint` (`types.ts:355-387`); `request<T>` deseni (`api.ts:336-347`).
- Produces:
  - `types.ts`: `StatsWindow = "30d" | "90d"`, `StatBox`, `ActiveAdapterBox`, `StatsBoxes`, `StatsDay`, `StatsTargetSeries`, `CategoryCount`, `StatsFunnel`, `CohortRow`, `ConsistencyCard`, `AdminStats`.
  - `api.ts`: `api.admin.stats(window: StatsWindow)` → `Promise<AdminStats>`.
  - `stats.ts`: `changeLabel`, `changeTone`, `daySeries`, `validitySeries`, `targetSeries`, `funnelRates`, `cohortRate`, `shareRows`.

- [x] **Step 1: Tipleri yaz**

`types.ts`'ye, `AdminOverview` (satır 339-352) bloğunun altına, backend JSON etiketleriyle **birebir** aynı alan adlarıyla: `change_pct: number | null`, `days: StatsDay[]`, `runs_by_target: StatsTargetSeries[]`, `consistency: ConsistencyCard | null` vb. `AdminStats` alanları: `window`, `from`, `to`, `boxes`, `days`, `org_types`, `runs_by_target`, `funnel`, `cohorts`, `consistency`.

- [x] **Step 2: API istemcisini yaz**

`api.ts`'de `metrics` (satır 340-341) hemen altına:

```ts
stats: (window: StatsWindow) =>
  request<AdminStats>(`/admin/stats?window=${encodeURIComponent(window)}`),
```

- [x] **Step 3: Saf yardımcı testlerini yaz**

`mf-frontend/src/lib/stats.test.ts` — uzantılı import (`from "./stats.ts"`), `node:test` + `node:assert/strict` (`adminNav.test.ts` deseni):

```ts
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  changeLabel,
  daySeries,
  validitySeries,
  funnelRates,
  cohortRate,
  shareRows,
  targetSeries,
} from "./stats.ts";

test("değişim yüzdesi yokken tire basar, uydurmaz", () => {
  assert.equal(changeLabel(null), "—");
  assert.equal(changeLabel(0), "%0");
  assert.equal(changeLabel(12.5), "+%12,5");
  assert.equal(changeLabel(-8), "-%8");
});

test("günlük seri her günü taşır, boş günler sıfır", () => {
  const days = [
    { t: 100, new_users: 2, cumulative_users: 12, assessments: 0, schema_valid: 0 },
    { t: 200, new_users: 0, cumulative_users: 12, assessments: 3, schema_valid: 2 },
  ];
  const s = daySeries(days, "new_users", "Yeni kayıt");
  assert.equal(s.points.length, 2);
  assert.deepEqual(s.points[1], { t: 200, v: 0 });
  const cum = daySeries(days, "cumulative_users", "Toplam üye");
  assert.ok(cum.points[1].v >= cum.points[0].v, "kümülatif seri azalamaz");
});

test("şema geçerliliği analiz üretilmeyen günü çizmez", () => {
  const s = validitySeries([
    { t: 100, new_users: 0, cumulative_users: 1, assessments: 0, schema_valid: 0 },
    { t: 200, new_users: 0, cumulative_users: 1, assessments: 4, schema_valid: 3 },
  ]);
  assert.equal(s.points.length, 1, "payda sıfır olan gün noktaya dönüşmemeli");
  assert.equal(s.points[0].v, 0.75);
});

test("huni oranı paydası sıfırken NaN değil sıfır", () => {
  assert.deepEqual(funnelRates({ registered: 0, consented: 0, analyzed: 0 }), {
    consented: 0,
    analyzed: 0,
  });
  assert.deepEqual(funnelRates({ registered: 4, consented: 3, analyzed: 1 }), {
    consented: 0.75,
    analyzed: 0.25,
  });
});

test("olgunlaşmamış kohort hücresi null döner", () => {
  assert.equal(cohortRate(0, 10, 1, 4), null, "1 haftalık kohortun 4. haftası yok");
  assert.equal(cohortRate(3, 10, 4, 4), 0.3);
  assert.equal(cohortRate(0, 0, 8, 2), 0, "boş kohort NaN değil sıfır");
});

test("dağılım payları toplamı bire yakın, boş girdide boş liste", () => {
  assert.deepEqual(shareRows([]), []);
  const rows = shareRows([
    { key: "individual", count: 3 },
    { key: "company", count: 1 },
  ]);
  assert.equal(rows[0].share, 0.75);
  assert.equal(rows[1].share, 0.25);
});

test("hedef serileri en yoğun ikiyle sınırlanır ve sıralı gelir", () => {
  const out = targetSeries([
    { target: "server", points: [{ t: 1, v: 5 }] },
    { target: "browser", points: [{ t: 1, v: 9 }] },
  ]);
  assert.equal(out.length, 2);
  assert.equal(out[0].label, "Tarayıcı", "en yoğun hedef ilk renge düşer");
});
```

- [x] **Step 4: Testleri çalıştır, düştüklerini gör**

Run: `cd mf-frontend && npm test`
Expected: FAIL — `Cannot find module './stats.ts'`.

- [x] **Step 5: `stats.ts`'i yaz**

Dosya başına neden saf olduğunu anlatan kısa yorum: kova, oran ve olgunluk mantığı bileşenden ayrı durduğu için `node --test` ile koşabiliyor (spec §9).

- `changeLabel(pct: number | null): string` — `null` → `"—"`; pozitifte `+` öneki; Türkçe ondalık virgül (`toLocaleString("tr-TR", { maximumFractionDigits: 1 })`), yüzde işareti **başta**.
- `changeTone(pct: number | null): string` — `null` → `var(--text-faint)`, `> 0` → `var(--ok)`, `< 0` → `var(--bad)`, `0` → `var(--text-dim)`.
- `daySeries(days, key, label): MetricSeries` — `key` `"new_users" | "cumulative_users" | "assessments"`.
- `validitySeries(days): MetricSeries` — `assessments === 0` olan günü **atlar** (boşluk, sıfır değil).
- `targetSeries(list: StatsTargetSeries[]): MetricSeries[]` — toplam hacme göre azalan sırala, **en fazla 2** seri döndür (palet iki renk taşıyor ve döngüye girmiyor — `TimeChart.tsx:16-26`), etiketleri Türkçeleştir (`browser` → "Tarayıcı", `server` → "Sunucu", bilinmeyen anahtar olduğu gibi).
- `funnelRates(f): { consented: number; analyzed: number }` — payda sıfırsa sıfır.
- `cohortRate(count, size, matureWeeks, needWeeks): number | null` — `matureWeeks < needWeeks` → `null`; `size === 0` → `0`.
- `shareRows(list: CategoryCount[]): { key: string; label: string; count: number; share: number }[]` — toplam sıfırsa boş liste; etiketler `individual` → "Bireysel", `company` → "Şirket".

- [x] **Step 6: Testler + lint + derleme**

```bash
cd mf-frontend && npm test && npm run lint && npm run build
```
Expected: hepsi PASS.

- [x] **Step 7: Commit**

```bash
git add mf-frontend/src/lib/stats.ts mf-frontend/src/lib/stats.test.ts \
        mf-frontend/src/lib/types.ts mf-frontend/src/lib/api.ts
git commit -m "$(cat <<'EOF'
Keep the panel's arithmetic out of the components so it can be tested

Bucket filling, funnel ratios and cohort maturity are where a dashboard
lies quietly: a zero denominator becomes NaN, and an immature cohort
becomes a confident 0% that reads as churn.

These live in src/lib because that is the only place this frontend runs
tests.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: `StatsPanel` — dört kutu ve zaman serileri

**Files:**
- Create: `mf-frontend/src/components/yonetim/StatsPanel.tsx`
- Modify: `mf-frontend/src/components/ui/TimeChart.tsx` (`formatValue`'ya `percent`)

**Interfaces:**
- Consumes: `api.admin.stats`, `stats.ts` yardımcıları, `Stat` (`components/yonetim/Stat.tsx`), `TimeChart` + `ChartUnit`, `Segmented` (`components/ui/Segmented.tsx`), `AccountsPanel` yükleme/hata desenleri.
- Produces: `export function StatsPanel()`; `TimeChart`'ta `unit="percent"` desteği (0..1 değeri `%42` olarak yazar).

- [x] **Step 1: `percent` birimini ekle**

`TimeChart.tsx`'de `formatValue` (satır 37-50) başına:

```ts
if (unit === "percent") {
  // Oranlar 0..1 aralığında geliyor; eksende "0.75" değil "%75" okunur.
  return `%${Math.round(v * 100)}`;
}
```

`ChartUnit` birleşimine `"percent"` ekle. Mevcut `"rps" | "seconds" | "count"` davranışı değişmez — `MetricsView` aynı çıktıyı vermeye devam etmeli.

- [x] **Step 2: Pencere seçici ve veri yükleme**

`StatsPanel.tsx` — `"use client"`. `useState<StatsWindow>("30d")`, `useEffect` içinde `api.admin.stats(window)`; hata `notice notice-bad`, yükleme `skeleton` kartları (`OverviewPanel.tsx:19-31` deseni). Pencere seçici mevcut `Segmented` bileşeniyle (30 gün / 90 gün) — yeni bir seçici yazılmaz.

- [x] **Step 3: Dört kutu**

`Stat` bileşenini kullan. Değişim satırı `hint` içine yazılır; `Stat`'ın imzası **değişmemeli** ya da yalnızca opsiyonel alanla genişlemeli — `OverviewPanel`'in altı çağrısı bozulmadan derlenmeli.

| Kutu | Değer | Alt satır |
|---|---|---|
| Toplam üye | `boxes.total_users.value` | `changeLabel(...change_pct)` + "önceki pencereye göre" |
| Toplam rapor | `boxes.total_reports.value` | aynı |
| Son 24 saat | `boxes.reports_last_24h.value` | "önceki 24 saate göre" |
| Aktif adapter | `boxes.active_adapter.name \|\| "—"` | şema uyumu `%..`, `change_points` puan farkı |

Aktif adapter kutusunun alt satırı **yüzde puanı** taşır ("+3 puan"), yüzde değişimi değil — iki oranın farkını yüzde diye sunmak okuyana yanlış büyüklük verir.

- [x] **Step 4: Zaman serisi chart'ları**

Dört `TimeChart`, her biri `card` içinde başlık + bir cümlelik açıklamayla:

1. **Yeni kayıt (gün)** — `daySeries(days, "new_users", "Yeni kayıt")`, `unit="count"`.
2. **Toplam üye** — `daySeries(days, "cumulative_users", "Toplam üye")`, `unit="count"`. **Ayrı chart**, birincisiyle aynı eksende değil: kümülatif seri günlük seriyi büyüklük mertebesiyle ezer, günlük çizgi tabanda düzleşir. Tek eksenli bir bileşene iki ölçek sığmaz.
3. **Analiz hacmi** — `daySeries(days, "assessments", "Analiz")`, `unit="count"`.
4. **Şema geçerliliği** — `validitySeries(days)`, `unit="percent"`. Açıklama: adapter aktive edildikten sonra düşerse geri alma sinyali (spec §4).

Tablo görünümü için panelin başında tek bir aç/kapa dursun ve hepsine geçsin (`MetricsView.tsx:142` deseni).

- [x] **Step 5: Doğrula**

```bash
cd mf-frontend && npm test && npm run lint && npm run build
```
Expected: PASS (`npm test` regresyon içindir — `formatValue` değişti).

- [x] **Step 6: Commit**

```bash
git add mf-frontend/src/components/yonetim/StatsPanel.tsx mf-frontend/src/components/ui/TimeChart.tsx
git commit -m "$(cat <<'EOF'
Split daily signups from the cumulative line instead of sharing an axis

One axis cannot carry both: the cumulative total flattens the daily line
into the baseline, and the reader loses the series they came for.

Rates get a percent unit rather than a raw 0.75 on the axis, and a day
with no analyses is drawn as a gap — zero would read as a day where
nothing the model produced was valid.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Dağılım, huni, kohort görselleri ve sayfaya bağlama

**Files:**
- Create: `mf-frontend/src/components/ui/Breakdown.tsx`
- Modify: `mf-frontend/src/components/yonetim/StatsPanel.tsx`
- Modify: `mf-frontend/src/app/yonetim/page.tsx`

**Interfaces:**
- Consumes: `stats.ts`'ten `shareRows`, `funnelRates`, `cohortRate`, `targetSeries`; `TimeChart`.
- Produces: `ShareBar`, `FunnelBars`, `CohortGrid` — üçü de tek dosyada, çünkü aynı gerekçeyle (kütüphanesiz, iki renkli palet, dark-only) birlikte değişiyorlar.

**Neden yeni bağımlılık yok:** spec §11 — `TimeChart` zaten elle çiziliyor, üç üretim bağımlılığı olan bir frontend'e dördüncüsü chart için gelmiyor. Bu üç görselin hepsi `div` + CSS genişliği; SVG bile gerekmiyor.

- [x] **Step 1: `Breakdown.tsx`'i yaz**

Üç bileşen, hepsi `"use client"` gerektirmeyen saf sunum (state yok):

```tsx
export function ShareBar({ rows }: { rows: { key: string; label: string; count: number; share: number }[] })
export function FunnelBars({ stages }: { stages: { label: string; count: number; rate: number }[] })
export function CohortGrid({ rows }: { rows: { label: string; size: number; week2: number | null; week4: number | null }[] })
```

- `ShareBar`: tek yatay yığılmış çubuk (`--series-1` / `--series-2`) + altında etiket/sayı/yüzde listesi. Renk tek başına bilgi taşımaz — her segmentin karşılığı listede yazılı (renk körlüğü ve ekran okuyucu).
- `FunnelBars`: aşama başına bir satır; çubuk genişliği `rate * 100%`, sağda mutlak sayı ve yüzde. İlk aşama daima %100.
- `CohortGrid`: `table` elemanı (grid değil — bu bir tablo ve ekran okuyucu öyle okumalı). Sütunlar: kohort haftası, üye, 2. hafta, 4. hafta. `null` hücre `—` basar ve `title` ile "kohort henüz bu yaşta değil" der.
- Boş girdide her üçü de "Bu aralıkta veri yok" basar (`TimeChart.tsx:140-148` ile aynı metin).

Renkler `var(--series-1)` / `var(--series-2)`, `var(--line)`, `var(--text-dim)` üzerinden; sabit hex yazılmaz (bu bileşenler tooltip swatch'ı okumadığı için `TimeChart`'ın literal gerekçesi burada geçerli değil).

- [x] **Step 2: `StatsPanel`'e bağla**

`StatsPanel`'e üç kart daha:

- **Hesap dağılımı** — `ShareBar rows={shareRows(data.org_types)}`.
- **Aktivasyon hunisi** — `FunnelBars`, aşamalar: "Kayıt" (%100), "Koşulları kabul", "İlk analiz"; oranlar `funnelRates(data.funnel)`. Kart açıklaması: üye sayısı büyürken bu düşüyorsa büyüme sahtedir (spec §4).
- **Kohort tutunması** — `CohortGrid`, satır etiketi hafta başı tarihi (`tr-TR`, gün/ay), hücreler `cohortRate(row.week_2, row.size, row.mature_weeks, 2)` ve `cohortRate(row.week_4, row.size, row.mature_weeks, 4)`.

Ayrıca **çalışma hacmi** chart'ı: `TimeChart series={targetSeries(data.runs_by_target)} unit="count"`.

- [x] **Step 3: Sayfaya bağla**

`mf-frontend/src/app/yonetim/page.tsx`:

```tsx
import { StatsPanel } from "@/components/yonetim/StatsPanel";
import { OverviewPanel } from "@/components/yonetim/OverviewPanel";

export default function Page() {
  return (
    <div className="space-y-8">
      <StatsPanel />
      <OverviewPanel />
    </div>
  );
}
```

`OverviewPanel` **silinmez ve içi boşaltılmaz** — p95, adapter build ve 24 saatlik şema uyumu operasyonel sayılar ve `StatsPanel`'in pencereli görünümünde karşılıkları yok. Gerekirse üstüne "Anlık durum" başlığı eklenir.

- [x] **Step 4: Doğrula**

```bash
cd mf-frontend && npm test && npm run lint && npm run build
```
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add mf-frontend/src/components/ui/Breakdown.tsx \
        mf-frontend/src/components/yonetim/StatsPanel.tsx \
        mf-frontend/src/app/yonetim/page.tsx
git commit -m "$(cat <<'EOF'
Draw the funnel and cohort table by hand rather than add a chart library

Three coloured bars and a table do not justify a fourth production
dependency on a frontend that has three, and a library would arrive with
its own theme system to fight the one this app already has.

The cohort grid is a table element, and an immature cell prints a dash:
a cohort that is one week old has no fourth-week retention to report.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Tutarlılık kartı (kapsam kesilirse ilk bu düşer)

**Files:**
- Modify: `mf-backend/internal/admin/stats.go`, `stats_store.go`, `stats_test.go`
- Modify: `mf-frontend/src/components/yonetim/StatsPanel.tsx`

**Interfaces:**
- Consumes: `analysis.PerCriterionStdDev(criteria []Criterion, runs [][]Finding) map[string]float64` (`internal/analysis/scoring.go:125-165`); trial'lar ayrı tabloda değil, `assessments.trial_group` sütununda (`migrations/005_analysis.sql:93-96`, indeks `:118-119`).
- Produces: `StatsResponse.Consistency *ConsistencyCard` (Task 1'de tanımlı) dolu döner.

**Neden:** `POST /analysis/trial` ve `PerCriterionStdDev` zaten yazılmış; eksik olan **yayınlanmış bir sayı** (`docs/urun-ve-pazarlama.md` §7, 3. öncelik). Kart panelde durunca her adapter değişiminden sonra yeniden ölçmek doğal hale gelir.

**Kapsam notu:** bu görev düşerse panel eksik olmaz — `consistency` alanı `null` döner ve frontend kartı basmaz. Diğer yedi görev bunun üstüne bir şey inşa etmiyor.

- [x] **Step 1: Sahte store testini yaz**

`stats_test.go`'ye: `fakeStatsStore` `Consistency: nil` döndürdüğünde yanıtın `"consistency": null` taşıdığını ve frontend sözleşmesinin bozulmadığını; dolu döndüğünde alanların JSON'a birebir geçtiğini doğrulayan bir test.

- [x] **Step 2: Store sorgusunu yaz**

En son trial grubu:

```sql
SELECT trial_group, count(*), min(created_at)
  FROM assessments WHERE trial_group IS NOT NULL
 GROUP BY trial_group ORDER BY max(created_at) DESC LIMIT 1
```

Sonra o grubun satırlarını (`total_score` ve bulguları) çekip:
- `TotalSpread = max(total) - min(total)`, `MinTotal`, `MaxTotal`.
- `VolatileCriterion` / `VolatileStdDev`: `analysis.PerCriterionStdDev` çıktısının en büyük değeri; eşitlikte kriter anahtarına göre **deterministik** seçim (map yineleme sırası rastgele — sıralamadan seçmek panelin sayısını istekten isteğe oynatır).

Grup yoksa `nil` döner, hata değil: henüz trial koşulmamış olması bir arıza değil.

`internal/admin`'in `internal/analysis`'e bağımlılığı yeni bir yön açıyorsa (import döngüsü riski) skorlama tarafını çağırmak yerine standart sapmayı SQL'de `stddev_samp` ile hesapla ve bunu görev raporunda gerekçesiyle belirt.

- [x] **Step 3: Backend'i doğrula**

```bash
cd mf-backend && go build ./... && go test ./...
```
Expected: PASS.

- [x] **Step 4: Kartı ekle**

`StatsPanel`'in **sonuna** (spec §4: "panelde bir kart", diğer her şeyden sonra) `data.consistency` doluysa bir kart: spread değeri, kaç koşum, en oynak kriter ve standart sapması, grubun tarihi. `null` ise hiçbir şey basılmaz — boş bir kart, olmayan bir ölçümü varmış gibi gösterir (spec §4 sonu).

Kart metni ürünün konumlandırma diline uymalı: satılan eksen **ilk elemede tutarlılık**; "en iyi model" ya da "AI karar verir" yazılmaz.

- [x] **Step 5: Frontend'i doğrula**

```bash
cd mf-frontend && npm test && npm run lint && npm run build
```
Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add mf-backend/internal/admin/ mf-frontend/src/components/yonetim/StatsPanel.tsx
git commit -m "$(cat <<'EOF'
Put the trial spread on the panel so the sales number cannot go stale

The consistency machinery has existed since the trial endpoint landed;
what was missing was a published figure. A card on the panel makes
re-measuring after every adapter change the path of least resistance.

No trial group yet is not an error — the field is null and the card is
absent, because an empty card presents a measurement that was never taken.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Aşama kapanışı — bütün doğrulama ve belge notu

**Files:**
- Modify: `docs/superpowers/plans/2026-08-04-yonetim-paneli-panel.md` (kutucukları işaretle)
- Modify: `docs/nasil-calisiyor.md` (panel bölümüne `/admin/stats` satırı — dosyada panel/admin bölümü yoksa ekleme yapma, raporunda belirt)

**Interfaces:**
- Consumes: 1-7. görevlerin tamamı.
- Produces: yeşil bir dal ve kapanış kaydı.

- [x] **Step 1: Bütün paketleri koştur**

```bash
cd mf-backend && go build ./... && go test ./...
cd ../mf-frontend && npm test && npm run lint && npm run build
```
Expected: hepsi PASS. Çıktıları rapor dosyasına yaz.

- [x] **Step 2: Sözleşme denetimi**

`mf-backend/internal/admin/stats.go`'daki JSON etiketlerini `mf-frontend/src/lib/types.ts`'teki alan adlarıyla tek tek karşılaştır. Bir isim uyuşmazlığı `undefined` olarak sessizce geçer ve chart boş çizer — derleme bunu yakalamaz.

- [x] **Step 3: Boş veritabanı sözleşmesi**

`grep` ile doğrula: `Days`, `OrgTypes`, `RunsByTarget`, `Cohorts` hiçbir yolda `nil` dönmüyor (spec §9: boş veritabanında sıfırlarla döner, çökmez).

- [x] **Step 4: Plan kutucuklarını işaretle ve commit**

```bash
git add docs/
git commit -m "$(cat <<'EOF'
Close out phase three with the panel reading only from Postgres

Every series on this screen comes from the database, so the panel keeps
working when the inference machine is switched off — a dashboard that
empties with the GPU box would read as a dead product.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

## Deploy notu (spec §10)

Backend önce: frontend `GET /admin/stats`'ı okuyor, uç yokken panel boş açılır. Ters yönde pencere yok — yeni uç eski frontend'i bozmuyor.

`render.yaml` `autoDeploy: true` diyor ama GitHub webhook'u pratikte ateşlemiyor. Deploy elle tetiklenip doğrulanacak; doğrulanmadan hiçbir şey "çıktı" diye raporlanmayacak.

## Bu planda bilinçli olarak olmayanlar

- **Yeni migration / indeks.** 013 ve 014 spec §5-§6'ya ayrılmış. İndeks gerekçesi rollup gerekçesiyle aynı (spec §4): bu ölçekte tarama ucuz. Tetikleyici: `/admin/stats` p95'i 1s'yi geçerse indeks ayrı bir iş olarak eklenir.
- **Prometheus kaynaklı hiçbir seri.** `Metrikler` ekranı ürün tarafında kalıyor.
- **MRR / churn / LTV.** Faturalama yok; boş kutu olmayan ölçümü varmış gibi gösterir (spec §11).
- **Rollup tabloları, dual-axis chart, üçüncü seri rengi.** Palet iki renk taşıyor ve döngüye girmiyor; üçüncü hedef çıkarsa `targetSeries` en yoğun ikisini gösterir.
- **Hesap silme, belgeler, denetim kaydı.** 4. ve 5. aşama.
