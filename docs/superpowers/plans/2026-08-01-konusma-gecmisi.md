# Konuşma geçmişi — uygulama planı

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Kullanıcı ne yazdığını ve ne cevap aldığını görebilsin — persona konuşmaları ve üreteç koşumları için — ve bu geçmiş, bugün yayına aldığımız saklama modeline uysun.

**Architecture:** İşin çoğu `feat/persona-history` dalında yazılı ve `git merge-tree` çakışma bildirmiyor. Plan o dalı merge edip bugünkü main'e uygun hale getiriyor: konuşmalar için 30 günlük bir süpürge, üreteç geçmişinde redaksiyon farkındalığı, ve gizlilik metninde iki farklı silme davranışının ayrılması.

**Tech Stack:** Go 1.26.5 (chi, pgx v5), PostgreSQL, Next.js 16 + React, TypeScript.

**Spec:** [`docs/superpowers/specs/2026-08-01-konusma-gecmisi-design.md`](../specs/2026-08-01-konusma-gecmisi-design.md)

## Global Constraints

- **Veritabanı destekli test YOK.** Bu repoda hiç yok ve bu plan bir tane icat etmiyor: ne testcontainers, ne sqlmock, ne canlı Postgres. Store metotları tüketici tarafı arayüzlerle ve sahtelerle test edilir; SQL'in kendisi ilk deploy'da elle doğrulanır. Bu kabul edilmiş bir sınırdır.
- **Frontend testleri yalnızca `src/lib/*.test.ts`**, Node'un yerleşik koşucusuyla (`npm test`). Bileşen testi altyapısı yoktur, eklenmeyecek. Bileşenler `npm run lint` ve `npm run build` ile doğrulanır.
- **API alan adları snake_case** (`redacted_at`, `last_turn_at`, `prompt_preview`).
- **UI metinleri Türkçe.** Donanım, kart, tünel, GPU ya da model adı kullanıcıya görünmez. Yalnızca koyu tema.
- **Konuşmalar redakte edilmez, silinir.** Raporlar redakte edilir. İki davranış bilerek farklıdır ve metin ikisini ayırmak zorundadır.
- **Konuşma saklama ölçütü `last_turn_at`**, `created_at` değil.
- **Migration eklenmiyor.** `009_history.sql` dalda mevcut ve idempotent; 010'dan sonra sırayla koşması güvenli.
- Comments explain WHY.

---

### Task 1: Dalı merge et ve bugünkü main'e karşı doğrula

**Files:**
- Merge: `feat/persona-history` → `main` (12 dosya, 1627 satır)
- Doğrulama sonrası gerekirse: `mf-backend/cmd/server/main.go`, `mf-frontend/src/components/AppShell.tsx`

**Interfaces:**
- Consumes: yok (ilk görev)
- Produces: `decision.Store` (`NewStore(db *pgxpool.Pool) *Store`, metotları `Record`, `List`, `Get`, `Rename`, `Delete`), `decision.ConversationSummary`, `decision.Message`, `HistoryPanel` bileşeni ve `HistoryItem` tipi, ve `GET/PATCH/DELETE /decision/conversations` uçları.

- [ ] **Step 1: Merge'ü çalıştır**

```bash
cd /Users/emrah/dev/mf-capstone
git checkout main
git merge --no-ff feat/persona-history -m "Merge feat/persona-history: conversation history on both screens"
```

Çakışma çıkarsa **dur ve bildir** — `git merge-tree` temiz demişti, çıkan bir çakışma planın varsayımının yanlış olduğu anlamına gelir.

- [ ] **Step 2: Derle ve tüm testleri koş**

Run: `cd mf-backend && go build ./... && go test ./... -count=1`
Expected: PASS.

Bu adım işin kendisi. Dal 3 gün önce yazıldı; o zamandan beri `Assessment` ve `AssessmentSummary` `RedactedAt` aldı, `AssessmentStore` arayüzü `RedactAssessment` kazandı, `main.go` `retentionCleanup` işçisini kurdu. **Çakışmasızlık uyumluluk değildir** — derleme hatası buradan çıkar.

- [ ] **Step 3: Frontend'i derle**

Run: `cd mf-frontend && npm test && npm run lint && npm run build`
Expected: hepsi temiz.

- [ ] **Step 4: `decision.Store` main.go'ya bağlı mı, doğrula**

`cmd/server/main.go` içinde `decision.NewStore(pool)` çağrısı olmalı ve `decision.NewHandler`'a verilmelidir. Dal bunu kendi içinde yapıyor; merge sonrası hâlâ orada olduğunu gözle doğrula ve raporunda satır numarasıyla belirt. Task 2 aynı değişkene ihtiyaç duyacak.

- [ ] **Step 5: `HistoryPanel` yeni düzende görünüyor mu**

`AppShell.tsx`'te `<main>` artık iki satırlı bir flex kolon: kaydırılabilir bir görünüm satırı ve altında `shrink-0` bir alt bilgi. `PersonaView` ve `CodegenView` `h-full` ile o satıra yerleşiyor. Panelin taşmadığını ve kendi kaydırıcısının satırın içinde kaldığını sınıf adlarını okuyarak doğrula; şüphe varsa raporunda söyle.

- [ ] **Step 6: Commit**

Merge commit'i Step 1'de oluştu. Bu adımda düzeltme gerektiyse:

```bash
git add -A
git commit -m "fix(history): reconcile the merged branch with today's main"
```

Düzeltme gerekmediyse commit yok; raporunda "merge temiz, düzeltme gerekmedi" yaz.

---

### Task 2: Konuşma süpürgesi

**Files:**
- Modify: `mf-backend/internal/decision/store.go`
- Modify: `mf-backend/internal/retention/retention.go`
- Modify: `mf-backend/internal/retention/retention_test.go`
- Modify: `mf-backend/cmd/server/main.go`
- Modify: `mf-frontend/src/components/views/GizlilikView.tsx` (yalnızca süre cümlesi — tam metin Task 4'te)

**Interfaces:**
- Consumes: Task 1'in `decision.Store`'u.
- Produces: `(*decision.Store).SweepConversations(ctx context.Context, olderThan time.Time) (int64, error)`, `retention.ConversationSweeper` arayüzü, ve `retention.Sweep(ctx, a AssessmentSweeper, r RunSweeper, c ConversationSweeper, age time.Duration, now time.Time) (Result, error)` — parametre sırası bu, `Result` üçüncü alanı `Conversations int64`.

- [ ] **Step 1: Başarısız testleri yaz**

`mf-backend/internal/retention/retention_test.go` — mevcut `fakeSweeper`'a üçüncü metodu ekle ve üç testi üçlüye genişlet:

```go
func (f *fakeSweeper) SweepConversations(_ context.Context, olderThan time.Time) (int64, error) {
	f.calls++
	f.cutoff = olderThan
	return f.n, f.err
}
```

Mevcut üç testi şu hale getir (imza değişikliği zaten hepsini kırar):

```go
func TestSweepPassesTheSameCutoffToAllThree(t *testing.T) {
	a, r, c := &fakeSweeper{n: 3}, &fakeSweeper{n: 5}, &fakeSweeper{n: 7}
	got, err := Sweep(context.Background(), a, r, c, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	want := now.Add(-30 * 24 * time.Hour)
	if !a.cutoff.Equal(want) || !r.cutoff.Equal(want) || !c.cutoff.Equal(want) {
		t.Errorf("cutoffs %v / %v / %v, want all %v", a.cutoff, r.cutoff, c.cutoff, want)
	}
	if got.Assessments != 3 || got.Runs != 5 || got.Conversations != 7 {
		t.Errorf("Result = %+v, want {3 5 7}", got)
	}
}

// Bir tablonun düşmesi diğer ikisini bağışlamamalı: tablolar bağımsız
// başarısız oluyor, ve sağlam olanı atlamak kişisel veriyi yerinde bırakır.
func TestSweepRunsAllThreeEvenWhenTheFirstFails(t *testing.T) {
	boom := errors.New("connection reset")
	a, r, c := &fakeSweeper{err: boom}, &fakeSweeper{n: 5}, &fakeSweeper{n: 7}
	got, err := Sweep(context.Background(), a, r, c, time.Hour, now)
	if r.calls != 1 || c.calls != 1 {
		t.Errorf("runs called %d, conversations called %d; want 1 and 1", r.calls, c.calls)
	}
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to wrap %v", err, boom)
	}
	if got.Runs != 5 || got.Conversations != 7 {
		t.Errorf("Result = %+v, want the successful halves reported anyway", got)
	}
}

func TestSweepJoinsAllFailures(t *testing.T) {
	first, second, third := errors.New("a down"), errors.New("r down"), errors.New("c down")
	a, r, c := &fakeSweeper{err: first}, &fakeSweeper{err: second}, &fakeSweeper{err: third}
	_, err := Sweep(context.Background(), a, r, c, time.Hour, now)
	for _, want := range []error{first, second, third} {
		if !errors.Is(err, want) {
			t.Errorf("err = %v, want it to wrap %v", err, want)
		}
	}
}
```

- [ ] **Step 2: Testleri koş, düştüğünü gör**

Run: `cd mf-backend && go test ./internal/retention/ -v`
Expected: FAIL — `Sweep` üç parametre almıyor, `Result.Conversations` yok.

- [ ] **Step 3: `retention` paketini genişlet**

`mf-backend/internal/retention/retention.go`:

```go
// ConversationSweeper is the decision store. Unlike the other two this one
// deletes rather than blanks: a conversation feeds no aggregate, so there is no
// measurement left behind worth keeping, and a row emptied of its messages
// would be a tombstone nobody reads.
type ConversationSweeper interface {
	SweepConversations(ctx context.Context, olderThan time.Time) (int64, error)
}
```

`Result`'a `Conversations int64` ekle, `Sweep`'e üçüncü parametreyi ekle ve gövdeye üçüncü çağrıyı **aynı desende** koy — erken dönüş yok, hata `errs`'e eklenir, sayı koşulsuz atanır:

```go
	k, err := c.SweepConversations(ctx, cutoff)
	if err != nil {
		errs = append(errs, err)
	}
	res.Conversations = k
```

- [ ] **Step 4: Store metodunu yaz**

`mf-backend/internal/decision/store.go`, `Delete`'in hemen altına:

```go
// SweepConversations removes every thread untouched for longer than olderThan.
//
// last_turn_at, not created_at: a thread someone is still using should not
// vanish from under them on its thirtieth day, and "untouched for a month" is
// what the retention period actually promises. The column is already indexed by
// idx_conversations_user_active.
//
// DELETE rather than the blanking the reports get. A report row carries
// measurements that aggregates depend on; a conversation carries none, so
// there is nothing to preserve and an emptied row would be litter.
// conversation_messages goes with it through ON DELETE CASCADE.
func (s *Store) SweepConversations(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM conversations WHERE last_turn_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

- [ ] **Step 5: main.go'yu güncelle**

`retentionCleanup`'ın imzasına üçüncü süpürgeyi ekle ve `retention.Sweep` çağrısına geçir. Log satırına üçüncü sayacı ekle ve koşulu genişlet:

```go
		if res.Assessments > 0 || res.Runs > 0 || res.Conversations > 0 {
			slog.Info("retention sweep",
				"assessments", res.Assessments, "runs", res.Runs,
				"conversations", res.Conversations)
		}
```

Çağrı yerinde `decisionStore` değişkenini kullan — Task 1'in raporunda satır numarası var. Yoksa `analysisStore` için yapılanın aynısını yap: inline yapılan `decision.NewStore(pool)` çağrısını kendi değişkenine çıkar, aynı örneği hem handler'a hem işçiye ver, **ikinci bir store yaratma.**

- [ ] **Step 6: Testleri koş**

Run: `cd mf-backend && go build ./... && go test ./... -count=1`
Expected: PASS, retention'da üç test.

- [ ] **Step 7: Commit**

```bash
git add mf-backend/internal/decision/store.go mf-backend/internal/retention/ mf-backend/cmd/server/main.go
git commit -m "feat(history): sweep conversations by last touch, not by birth"
```

---

### Task 3: Üreteç geçmişi redaksiyonu tanısın

**Files:**
- Modify: `mf-backend/internal/llm/models.go`
- Modify: `mf-backend/internal/llm/store.go` (`ListRuns`)
- Modify: `mf-frontend/src/lib/types.ts`
- Modify: `mf-frontend/src/lib/report.ts`
- Test: `mf-frontend/src/lib/report.test.ts`
- Modify: `mf-frontend/src/components/views/CodegenView.tsx`

**Interfaces:**
- Consumes: Task 1'in `HistoryItem` tipi (`{id, title, badge?, timestamp, detail?}`) ve `CodegenView`'ın `api.listRuns` çağrısı.
- Produces: `llm.RunSummary.RedactedAt *time.Time` (json `redacted_at`), ve `runTitle(x: { redacted_at: string | null; prompt_preview: string }): string`.

**Neden şimdi:** KVKK işinin 1. görevi `RunSummary`'ye bu alanı bilerek **eklememişti**, gerekçesi "alan ve sütun, onu okuyan kodla birlikte gelir" idi. Okuyan kod bu görevle geliyor. O zamana kadar `ListRuns` sorgusu sütunu hiç çekmiyordu.

- [ ] **Step 1: Başarısız frontend testini yaz**

`mf-frontend/src/lib/report.test.ts` sonuna:

```ts
import { runTitle } from "./report.ts";

test("redakte koşum, boş istemle 'kullanıcı bir şey yazmamış' gibi görünmez", () => {
  assert.equal(
    runTitle({ redacted_at: "2026-08-01T12:00:00Z", prompt_preview: "" }),
    "İçerik silindi",
  );
});

test("redakte edilmemiş koşum kendi önizlemesini taşır", () => {
  assert.equal(
    runTitle({ redacted_at: null, prompt_preview: "bir liste bileşeni yaz" }),
    "bir liste bileşeni yaz",
  );
});

// Boş önizleme ama silinmemiş: üçüncü durum, ve silinmiş demek yanlış olur.
test("boş önizlemeli ama silinmemiş koşum, silinmiş sayılmaz", () => {
  assert.equal(runTitle({ redacted_at: null, prompt_preview: "" }), "Başlıksız");
});
```

Mevcut import satırını `import { isRedacted, reportTitle, runTitle } from "./report.ts";` olacak şekilde birleştir — iki ayrı import satırı lint hatası verir.

- [ ] **Step 2: Testi koş, düştüğünü gör**

Run: `cd mf-frontend && npm test`
Expected: FAIL — `runTitle` dışa aktarılmamış.

- [ ] **Step 3: Yardımcıyı yaz**

`mf-frontend/src/lib/report.ts` sonuna:

```ts
// Koşumlar için aynı üç durum, farklı alan adıyla. reportTitle ile
// birleştirilmedi: ikisi farklı API tiplerinden besleniyor ve tek bir jenerik
// imza, çağıran tarafta hangi alanın okunduğunu görünmez yapardı.
export function runTitle(
  x: { redacted_at: string | null; prompt_preview: string },
): string {
  if (isRedacted(x)) return "İçerik silindi";
  return x.prompt_preview || "Başlıksız";
}
```

- [ ] **Step 4: Testi koş**

Run: `cd mf-frontend && npm test`
Expected: PASS.

- [ ] **Step 5: Backend'e alanı ve sütunu ekle**

`mf-backend/internal/llm/models.go`, `RunSummary` struct'ına `CreatedAt`'ten sonra:

```go
	// The codegen history list reads this to tell a swept run from one whose
	// prompt was always empty. It was deliberately left off until now: the
	// column and the field belong with the code that reads them, and until the
	// history panel existed nothing did.
	RedactedAt *time.Time `json:"redacted_at"`
```

`mf-backend/internal/llm/store.go`, `ListRuns` sorgusunda `r.created_at`'ten sonra `r.redacted_at` ekle, ve `rows.Scan` argümanlarında `&run.CreatedAt`'ten sonra `&run.RedactedAt` ekle. **pgx konuma göre tarar** — sütunu ekleyip taramayı unutmak, sonraki alanlara yanlış değer yazar ve derleme hatası vermez. Bu sorguda `redacted_at`'ten sonra skor sütunları geliyor, yani hata sessizce skoru bozar.

- [ ] **Step 6: Frontend tipini ve eşlemeyi güncelle**

`mf-frontend/src/lib/types.ts`, `RunSummary` arayüzüne:

```ts
  redacted_at: string | null;
```

`mf-frontend/src/components/views/CodegenView.tsx` içinde `api.listRuns` sonucunu `HistoryItem`'a çeviren eşlemeyi bul ve başlığı `runTitle(r)` ile üret. `runTitle`'ı `@/lib/report`'tan içe aktar.

- [ ] **Step 7: Hepsini koş**

Run: `cd mf-backend && go build ./... && go test ./... -count=1`
Run: `cd mf-frontend && npm test && npm run lint && npm run build`
Expected: hepsi temiz.

- [ ] **Step 8: Commit**

```bash
git add mf-backend/internal/llm/ mf-frontend/src/lib/ mf-frontend/src/components/views/CodegenView.tsx
git commit -m "feat(history): a swept run says it was deleted, not that it was empty"
```

---

### Task 4: Gizlilik metni iki davranışı ayırsın

**Files:**
- Modify: `mf-frontend/src/components/views/GizlilikView.tsx`

**Interfaces:**
- Consumes: Task 2'nin silme davranışı, Task 3'ün redaksiyon davranışı.
- Produces: yok (metin).

- [ ] **Step 1: "Ne saklanıyor" listesine konuşmaları ekle**

Mevcut dört maddeli listeye, üreteç maddesinden sonra:

```tsx
        <li>
          Persona ekranındaki konuşmalarınız: yazdığınız mesajlar, aldığınız
          yanıtlar, ve o yanıtı üretirken toplanan araştırma sonuçları.
        </li>
```

- [ ] **Step 2: "Ne kadar süreyle" bölümünü iki davranışa böl**

Mevcut 30 gün paragrafını **değiştirme**, altına ekle:

```tsx
      <p className="mt-2 text-sm">
        Persona konuşmaları için silme daha basit: otuz gün dokunulmayan bir
        konuşma tamamen siliniyor, mesajlarıyla birlikte, geriye kayıt
        kalmıyor. Raporlarda içeriksiz bir ölçüm satırı kalmasının sebebi o
        satırın ürünün kendi ölçümlerini beslemesi; bir konuşma hiçbir şey
        beslemiyor, o yüzden saklanacak bir şeyi de yok.
      </p>
      <p className="mt-2 text-sm">
        Süre, konuşmanın açıldığı tarihten değil <strong>son
        mesajdan</strong> sayılıyor — sürdürdüğünüz bir konuşma otuzuncu
        gününde ortasından silinmiyor.
      </p>
```

- [ ] **Step 3: Doğrula**

Run: `cd mf-frontend && npm run lint && npm run build`
Expected: temiz.

Metni okuyup şunu kontrol et: sayfa artık üç farklı davranış anlatıyor (rapor redakte edilir, konuşma silinir, hesap bilgisi kalır) ve üçü de kodda karşılığı olan cümlelerdir. Karşılığı olmayan bir cümle varsa **bildir, ekleme**.

- [ ] **Step 4: Commit**

```bash
git add mf-frontend/src/components/views/GizlilikView.tsx
git commit -m "docs(kvkk): the page now describes two kinds of deletion, because there are two"
```

---

## Self-review notları

**Spec kapsamı.** Dört maddenin de karşılığı var: merge ve doğrulama Task 1; konuşma süpürgesi Task 2; redaksiyon farkındalığı Task 3; gizlilik metni Task 4.

**Test edilmeyen ne kaldı:** `SweepConversations`'ın SQL'i ve cascade davranışı. Bu repoda DB testi yok ve plan bir tane icat etmiyor; ilk deploy'da bir konuşma silip `conversation_messages`'ın da gittiğini elle görmek gerekiyor.

**Sıra bağımlılığı gerçek:** Task 2 ve 3 Task 1'in merge'üne bağlı, çünkü ikisinin dokunduğu dosyaların bir kısmı henüz main'de yok. Task 4 metin olduğu için bağımsız ama en sona konuldu; anlattığı davranışlar 2 ve 3'te yazılıyor ve önce yazılırsa sayfa olmayan bir şeyi anlatır.
