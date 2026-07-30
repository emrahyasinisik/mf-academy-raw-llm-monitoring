# Analiz Ekranı Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ürünün rubrik analiz motorunu frontend'de çalıştırılabilir ve okunabilir hale getiren `AnalizView` ekranını yazmak.

**Architecture:** Tek parça master view (`AnalizView.tsx`), AppShell'in kalıcı mount edilen grubunda. Saf aritmetik ve bütçe hesabı ayrı bir `lib/rubric.ts` modülüne çıkarılıyor çünkü tek test edilebilir parça o; ekranın kalanı görsel ve elle doğrulanıyor. Puan aritmetiği `internal/analysis/scoring.go`'dan birebir kopyalanıyor, yeniden türetilmiyor.

**Tech Stack:** Next.js 16 (App Router, SPA), React 19, TypeScript 5, Tailwind 4, Node 25 yerleşik test koşucusu (`node --test`, TypeScript'i doğal çalıştırıyor — yeni bağımlılık yok).

## Global Constraints

- **Dil Türkçe.** Tüm kullanıcıya görünen metin Türkçe. Rubrik etiketleri zaten Türkçe.
- **Tema koyu, tek seçenek.** `globals.css`'teki değişkenler kullanılır; yeni renk tanımlanmaz.
- **Atölye dili yasak.** Kart, tünel, GPU, "6 GB" gibi ifadeler UI'da geçmez. Model kimliği ve süre teknik meta olarak gösterilir, donanım anlatılmaz.
- **`score: null` asla 0 olarak çizilmez.** "Kanıt yok" ayrı bir haldir.
- **`overall_score` asla `coverage` olmadan gösterilmez.**
- **Yeni bağımlılık eklenmez.** `package.json`'ın `dependencies`/`devDependencies` bölümleri değişmez; yalnızca `scripts` bölümüne `test` eklenir.
- **Ölçülen sabitler** (bu plan yazılırken Qwen3-4B-Instruct-2507 tokenizer'ıyla ölçüldü, 30 Temmuz 2026):
  - Türkçe düzyazı: **2.09** karakter/token
  - Yapılandırılmış sistem prompt'u: **3.00** karakter/token
  - Kullanıcı mesajı sarmalayıcısı: **43** token
  - `startup-investability` sistem prompt'u: 2527 karakter / 841 token
  - `digital-marketing` sistem prompt'u: 1962 karakter / 656 token
- **Doğrulama komutları** her task sonunda, `mf-frontend/` içinden:
  - `npm test` (varsa ilgili task için)
  - `npx tsc --noEmit`
  - `npm run lint`

---

### Task 1: Puan aritmetiği — `lib/rubric.ts`

Raporun katkı kolonu buradan besleniyor. Naif yazılırsa kolon toplama eşit çıkmaz ve aritmetiğini gösterip yanlış toplayan bir rapor, hiç göstermeyenden kötüdür. `scoring.go:44-89` birebir taşınıyor.

**Files:**
- Create: `mf-frontend/src/lib/rubric.ts`
- Create: `mf-frontend/src/lib/rubric.test.ts`
- Modify: `mf-frontend/package.json` (yalnızca `scripts`)
- Modify: `mf-frontend/tsconfig.json` (tek derleyici seçeneği)

**Interfaces:**
- Consumes: `Criterion`, `Finding` — `mf-frontend/src/lib/types.ts` içinde zaten tanımlı.
- Produces:
  - `interface Row { criterion: Criterion; finding: Finding | null; scored: boolean; clamped: number | null; points: number | null }`
  - `interface Breakdown { rows: Row[]; overall: number | null; coverage: number; scoredWeight: number; totalWeight: number }`
  - `function breakdown(criteria: Criterion[], findings: Finding[]): Breakdown`

- [ ] **Step 1: `package.json`'a test scripti ekle**

`mf-frontend/package.json` içindeki `scripts` bloğunu şu hale getir (diğer alanlara dokunma):

```json
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "lint": "eslint",
    "test": "node --test src/lib/*.test.ts"
  },
```

- [ ] **Step 2: `tsconfig.json`'a tek seçenek ekle**

Node'un test koşucusu çalışma anındaki bir import olduğu için `.ts` uzantısını
görmek zorunda; TypeScript bunu varsayılan olarak reddediyor
(`TS5097`). `noEmit` zaten açık, ki bu seçeneğin ön koşulu odur.

`mf-frontend/tsconfig.json` içindeki `compilerOptions`'a ekle:

```json
    "allowImportingTsExtensions": true,
```

- [ ] **Step 3: Başarısız testi yaz**

Create `mf-frontend/src/lib/rubric.test.ts`:

```ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { breakdown } from "./rubric.ts";
import type { Criterion, Finding } from "./types.ts";

const c = (key: string, weight: number, scale_max = 5): Criterion => ({
  key,
  label: key,
  description: "",
  weight,
  scale_max,
});

const f = (key: string, score: number | null, found = true): Finding => ({
  key,
  evidence_found: found,
  score,
  evidence: [],
  rationale: "",
});

test("katkilar toplam puana esitlenir", () => {
  const criteria = [c("a", 0.5), c("b", 0.3), c("c", 0.2)];
  const b = breakdown(criteria, [f("a", 5), f("b", 3), f("c", 1)]);

  // `if/throw` ile daraltiliyor: assert.ok'un assertion imzasi burada
  // guvenilir sekilde daraltmiyor ve tsc "possibly null" veriyor.
  const overall = b.overall;
  if (overall === null) throw new Error("puan hesaplanamadi");

  const sum = b.rows.reduce((t, r) => t + (r.points ?? 0), 0);
  assert.ok(Math.abs(sum - overall) < 0.01, `${sum} != ${overall}`);
});

test("toplam scoredWeight'e gore normalize edilir, totalWeight'e gore degil", () => {
  // b kanitsiz: kapsam 0.5 duser ama puan yalnizca a'dan hesaplanir.
  const criteria = [c("a", 0.5), c("b", 0.5)];
  const findings = [f("a", 4), f("b", null, false)];
  const b = breakdown(criteria, findings);

  // 100 * (0.5 * 4/5) / 0.5 = 80, coverage 0.5. totalWeight'e bolunse 40 olurdu.
  assert.equal(b.overall, 80);
  assert.equal(b.coverage, 0.5);
});

test("kanitsiz kriter satirda kalir ama puanlanmaz", () => {
  const criteria = [c("a", 0.5), c("b", 0.5)];
  const b = breakdown(criteria, [f("a", 4), f("b", null, false)]);

  assert.equal(b.rows.length, 2);
  const row = b.rows.find((r) => r.criterion.key === "b")!;
  assert.equal(row.scored, false);
  assert.equal(row.points, null);
  assert.equal(row.clamped, null);
});

test("bulgusu hic gelmeyen kriter de satirda kalir", () => {
  const b = breakdown([c("a", 1)], []);
  assert.equal(b.rows.length, 1);
  assert.equal(b.rows[0].scored, false);
  assert.equal(b.overall, null);
  assert.equal(b.coverage, 0);
});

test("olcek disi puan kirpilir, bulgu atilmaz", () => {
  // Model 0-5 skalasinda 6 dondurdugunde azami sayilir.
  const b = breakdown([c("a", 1)], [f("a", 6)]);
  assert.equal(b.rows[0].clamped, 5);
  assert.equal(b.overall, 100);

  const neg = breakdown([c("a", 1)], [f("a", -2)]);
  assert.equal(neg.rows[0].clamped, 0);
  assert.equal(neg.overall, 0);
});

test("agirligi sifir veya negatif olan kriter tamamen atlanir", () => {
  // Rubrikteki bir yazim hatasi sonucu ters cevirememeli.
  const b = breakdown([c("a", 1), c("bad", -1)], [f("a", 5), f("bad", 5)]);
  assert.equal(b.totalWeight, 1);
  assert.equal(b.overall, 100);
  assert.equal(b.rows.find((r) => r.criterion.key === "bad")!.scored, false);
});

test("scale_max eksikse 5 varsayilir", () => {
  const missing = { ...c("a", 1), scale_max: 0 };
  const b = breakdown([missing], [f("a", 5)]);
  assert.equal(b.overall, 100);
});

test("evidence_found true ama score null ise puanlanmaz", () => {
  const b = breakdown([c("a", 1)], [f("a", null, true)]);
  assert.equal(b.overall, null);
  assert.equal(b.coverage, 0);
});

test("bos rubrik null dondurur, cokmez", () => {
  const b = breakdown([], []);
  assert.equal(b.overall, null);
  assert.equal(b.coverage, 0);
  assert.deepEqual(b.rows, []);
});
```

- [ ] **Step 4: Testin başarısız olduğunu doğrula**

Run: `cd mf-frontend && npm test`
Expected: FAIL — `Cannot find module './rubric.ts'`

- [ ] **Step 5: `lib/rubric.ts`'i yaz**

Create `mf-frontend/src/lib/rubric.ts`:

```ts
// Raporun aritmetiği.
//
// Bu dosya internal/analysis/scoring.go'nun bir kopyası, ve öyle kalmalı.
// Backend puanı hesaplayıp `overall_score` olarak gönderiyor; burada yeniden
// hesaplanmasının tek sebebi, o puanın **nasıl** çıktığını satır satır
// gösterebilmek. İki taraf ayrışırsa rapor kendi toplamıyla çelişir — ve
// aritmetiğini gösterip yanlış toplayan bir rapor, hiç göstermeyenden kötüdür.
// Bir kural burada değişecekse önce scoring.go'da değişir.

import type { Criterion, Finding } from "./types";

/** Bir rubrik satırının rapordaki hali. */
export interface Row {
  criterion: Criterion;
  /** Model o kriter için bir şey döndürmediyse null. */
  finding: Finding | null;
  /** Toplama katılıyor mu. */
  scored: boolean;
  /** [0, scale_max] aralığına kırpılmış puan; puanlanmadıysa null. */
  clamped: number | null;
  /** Bu satırın toplam puana katkısı; puanlanmadıysa null. */
  points: number | null;
}

export interface Breakdown {
  rows: Row[];
  /** Hiçbir şey değerlendirilemediyse null. Sıfır değil. */
  overall: number | null;
  /** Kanıtı olan kriterlerin ağırlık payı. */
  coverage: number;
  scoredWeight: number;
  totalWeight: number;
}

/** scoring.go'daki EffectiveScaleMax: sıfır bölmeyi sessiz NaN'a çevirmesin. */
function effectiveScaleMax(c: Criterion): number {
  return c.scale_max > 0 ? c.scale_max : 5;
}

export function breakdown(criteria: Criterion[], findings: Finding[]): Breakdown {
  const byKey = new Map(findings.map((f) => [f.key, f]));

  const rows: Row[] = [];
  let weightedSum = 0;
  let scoredWeight = 0;
  let totalWeight = 0;

  for (const criterion of criteria) {
    const finding = byKey.get(criterion.key) ?? null;

    // Ağırlığı ≤ 0 olan kriter tamamen atlanır: sıfır zaten katkı vermez,
    // negatif olan bir kriterin bir diğerini götürmesine izin verirdi.
    if (criterion.weight <= 0) {
      rows.push({ criterion, finding, scored: false, clamped: null, points: null });
      continue;
    }
    totalWeight += criterion.weight;

    if (!finding || !finding.evidence_found || finding.score === null) {
      rows.push({ criterion, finding, scored: false, clamped: null, points: null });
      continue;
    }

    // Kırpılır, reddedilmez: model 0-5 skalasında bazen 6 döndürüyor ve bunu
    // azami saymak, bulguyu tamamen atmaktan daha az bilgi kaybettiriyor.
    const max = effectiveScaleMax(criterion);
    const clamped = Math.max(0, Math.min(finding.score, max));

    weightedSum += criterion.weight * (clamped / max);
    scoredWeight += criterion.weight;

    rows.push({ criterion, finding, scored: true, clamped, points: null });
  }

  if (totalWeight <= 0) {
    return { rows, overall: null, coverage: 0, scoredWeight: 0, totalWeight: 0 };
  }
  const coverage = scoredWeight / totalWeight;

  if (scoredWeight <= 0) {
    return { rows, overall: null, coverage, scoredWeight, totalWeight };
  }

  // scoredWeight'e göre yeniden normalize edilir, totalWeight'e göre değil.
  // Tam rubrik ağırlığına bölmek kapsamı puanın içine katlardı: ele alınmamış
  // bir kriter, kötü ele alınmış biri kadar puanı aşağı çekerdi.
  const value = (100 * weightedSum) / scoredWeight;
  if (!Number.isFinite(value)) {
    return { rows, overall: null, coverage, scoredWeight, totalWeight };
  }

  // Satır katkıları aynı paydayı kullanır, yani tam olarak toplama eşitlenir.
  for (const row of rows) {
    if (!row.scored || row.clamped === null) continue;
    const max = effectiveScaleMax(row.criterion);
    row.points = (100 * row.criterion.weight * (row.clamped / max)) / scoredWeight;
  }

  return {
    rows,
    overall: Math.round(value * 100) / 100,
    coverage,
    scoredWeight,
    totalWeight,
  };
}
```

- [ ] **Step 6: Testlerin geçtiğini doğrula**

Run: `cd mf-frontend && npm test`
Expected: PASS — 9 test, 0 fail

- [ ] **Step 7: Tip kontrolü ve lint**

Run: `cd mf-frontend && npx tsc --noEmit && npm run lint`
Expected: ikisi de temiz

- [ ] **Step 8: Commit**

```bash
git add mf-frontend/src/lib/rubric.ts mf-frontend/src/lib/rubric.test.ts mf-frontend/package.json
git commit -m "feat(frontend): the report's arithmetic, copied from scoring.go not re-derived

The contribution column is what makes a rejection defensible, and it only
works if the numbers add up to the total sitting above them. scoring.go
renormalises by scoredWeight rather than totalWeight, so a naive weight x
score column would visibly disagree with the score it explains.

Tests run on Node's built-in runner, which executes TypeScript directly.
No new dependency: this phase does not get a test framework."
```

---

### Task 2: Prompt bütçesi — `lib/rubric.ts`'e ekleme

Vaka alanına yapıştırılan metin motorun penceresine sığmazsa istek ham 400 olarak dönüyor; `analysis` yolunda `decision`'daki gibi bir koruma yok. Ekranın sorumluluğu, göndermeden **önce** söylemek.

Ölçülen gerçek: `startup-investability` sistem prompt'u **841 token**. Vakaya kalan yer pencereye bağlı, ve iki farklı pencere dolaşımda:

| pencere | nereden | yatırılabilirlik | pazarlama |
|---|---|---|---|
| **1200** | `LLM_MAX_PROMPT_TOKENS` varsayılanı — ekranın göreceği sayı | **606 karakter** | 1001 karakter |
| 1366 | motorun gerçekte verdiği tavan | 953 karakter | 1348 karakter |

Gönderilen varsayılanla vakaya kalan yer **606 karakter** — yaklaşık 90 kelime. Bu sayı küçük ve dipnot olamaz; ekranın ana öğesi.

**Files:**
- Modify: `mf-frontend/src/lib/rubric.ts` (dosyanın sonuna eklenir)
- Modify: `mf-frontend/src/lib/rubric.test.ts` (dosyanın sonuna eklenir)

**Interfaces:**
- Produces:
  - `const PROSE_CHARS_PER_TOKEN = 2.09`
  - `const PROMPT_CHARS_PER_TOKEN = 3.0`
  - `const WRAPPER_TOKENS = 43`
  - `function caseBudgetChars(windowTokens: number, systemPromptChars: number): number`
  - `function estimateTokens(text: string): number`

- [ ] **Step 1: Başarısız testleri yaz**

`mf-frontend/src/lib/rubric.test.ts` dosyasının **sonuna** ekle:

```ts
import { caseBudgetChars, estimateTokens, PROSE_CHARS_PER_TOKEN } from "./rubric.ts";

test("gonderilen rubrikler icin butce, olculen degerler", () => {
  // startup-investability: 2527 karakter sistem prompt'u.
  // Backend'in varsayilan penceresi 1200 token (LLM_MAX_PROMPT_TOKENS).
  assert.equal(caseBudgetChars(1200, 2527), 606);
  // digital-marketing: 1962 karakter.
  assert.equal(caseBudgetChars(1200, 1962), 1001);
});

test("operator pencereyi acarsa butce buyur", () => {
  // Motorun gercekte verdigi tavan 1366 token; operator LLM_MAX_PROMPT_TOKENS'i
  // oraya cekerse ekran bunu sunucudan ogrenir ve butce buyur.
  assert.equal(caseBudgetChars(1366, 2527), 953);
});

test("daha kucuk rubrik daha buyuk butce birakir", () => {
  assert.ok(caseBudgetChars(1200, 1962) > caseBudgetChars(1200, 2527));
});

test("butce hicbir zaman negatif donmez", () => {
  assert.equal(caseBudgetChars(100, 9000), 0);
});

test("token tahmini olculen duzyazi oranini kullanir", () => {
  const text = "a".repeat(209);
  assert.equal(estimateTokens(text), Math.ceil(209 / PROSE_CHARS_PER_TOKEN));
});

test("bos metin sifir token", () => {
  assert.equal(estimateTokens(""), 0);
});
```

- [ ] **Step 2: Testin başarısız olduğunu doğrula**

Run: `cd mf-frontend && npm test`
Expected: FAIL — `caseBudgetChars is not a function` (veya export bulunamadı)

- [ ] **Step 3: Bütçe fonksiyonlarını yaz**

`mf-frontend/src/lib/rubric.ts` dosyasının **sonuna** ekle:

```ts
// ---- Prompt bütçesi ----
//
// Vaka metni motorun girdi penceresine sığmak zorunda, ve `analysis` yolunda
// bunu kontrol eden bir koruma yok (`decision`'da var — agent.go:78). Sığmayan
// bir istek mlc'den ham 400 olarak dönüyor, yani kullanıcı sebebini göremiyor.
// Bu yüzden sayaç göndermeden önce uyarıyor.
//
// Aşağıdaki üç oran tahmin değil, ölçüm: Qwen3-4B-Instruct-2507'nin kendi
// tokenizer'ıyla, 30 Temmuz 2026'da alındı. Tekrar ölçülmeden değiştirilmemeli.

/**
 * Türkçe düzyazının ölçülen sıkışması. İngilizce ortalamasından belirgin
 * biçimde kötü: eklemeli yapı ve çoğu İngilizce olan bir kelime dağarcığı
 * birleşince aynı cümle daha çok token ediyor.
 */
export const PROSE_CHARS_PER_TOKEN = 2.09;

/**
 * Sistem prompt'unun sıkışması. Düzyazıdan iyi, çünkü içeriği ASCII anahtarlar,
 * JSON şeması ve tekrar eden kalıplar — hepsi tokenizer'ın iyi bildiği şeyler.
 */
export const PROMPT_CHARS_PER_TOKEN = 3.0;

/** Kullanıcı mesajının sabit sarmalayıcısı (UserPrompt'un <<< >>> bloğu). */
export const WRAPPER_TOKENS = 43;

/** Sohbet şablonunun tur işaretleri ve yuvarlama için pay. */
const MARGIN_TOKENS = 24;

/** Metnin kaç token edeceğinin tahmini. Yukarı yuvarlar: düşük tahmin isteği kaybettirir. */
export function estimateTokens(text: string): number {
  if (!text) return 0;
  return Math.ceil(text.length / PROSE_CHARS_PER_TOKEN);
}

/**
 * Vaka metnine kalan karakter sayısı.
 *
 * @param windowTokens motorun kabul ettiği girdi penceresi (backend'den gelir)
 * @param systemPromptChars seçili rubriğin sistem prompt'unun uzunluğu
 */
export function caseBudgetChars(windowTokens: number, systemPromptChars: number): number {
  const systemTokens = Math.ceil(systemPromptChars / PROMPT_CHARS_PER_TOKEN);
  const left = windowTokens - systemTokens - WRAPPER_TOKENS - MARGIN_TOKENS;
  if (left <= 0) return 0;
  return Math.floor(left * PROSE_CHARS_PER_TOKEN);
}
```

- [ ] **Step 4: Testlerin geçtiğini doğrula**

Run: `cd mf-frontend && npm test`
Expected: PASS — 14 test, 0 fail

- [ ] **Step 5: Tip kontrolü ve lint**

Run: `cd mf-frontend && npx tsc --noEmit && npm run lint`
Expected: temiz

- [ ] **Step 6: Commit**

```bash
git add mf-frontend/src/lib/rubric.ts mf-frontend/src/lib/rubric.test.ts
git commit -m "feat(frontend): measure the case budget instead of guessing at it

The analysis path has no input-length guard, so a case that does not fit
comes back as a raw 400 from mlc with nothing on screen to explain it. The
counter has to say so before the request leaves.

The three ratios are measurements taken with Qwen3-4B's own tokenizer, not
estimates: Turkish prose is 2.09 chars/token, the structured system prompt
3.00, the user wrapper a fixed 43 tokens. The investability rubric's system
prompt is 841 of them, which leaves the case 606 characters at the shipped
window of 1200 tokens."
```

---

### Task 3: Backend pencereyi bildirsin — `/config`

Frontend'in bütçeyi hesaplayabilmesi için pencereyi bilmesi lazım. Sabit yazmak, spec'in kendi gerekçesini ihlal eder: iki yerde iki bağımsız tahmin, kontrol değil çelişki üretir. Dağıtıma göre değişen sayı sunucudan gelir; ölçülen oranlar sabit kalır.

**Files:**
- Modify: `mf-backend/internal/config/handler.go:18-34`
- Modify: `mf-backend/internal/config/config_test.go` (yeni test)

**Interfaces:**
- Produces: `GET /config` yanıtına `"limits": {"max_prompt_tokens": <int>}` eklenir.

- [ ] **Step 1: Başarısız testi yaz**

`mf-backend/internal/config/config_test.go` dosyasının sonuna ekle:

```go
func TestConfigHandlerPublishesPromptWindow(t *testing.T) {
	h := NewHandler(Config{AppName: "mf", AppVersion: "test", Env: "test", LLMMaxPromptTokens: 1200})

	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	rec := httptest.NewRecorder()
	h.Config(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Limits struct {
			MaxPromptTokens int `json:"max_prompt_tokens"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Limits.MaxPromptTokens != 1200 {
		t.Errorf("max_prompt_tokens = %d, want 1200", body.Limits.MaxPromptTokens)
	}
}
```

Dosyanın import bloğunda `encoding/json`, `net/http`, `net/http/httptest` ve `testing` bulunmalı; eksik olanı ekle.

- [ ] **Step 2: Testin başarısız olduğunu doğrula**

Run: `cd mf-backend && go test ./internal/config/ -run TestConfigHandlerPublishesPromptWindow -v`
Expected: FAIL — `max_prompt_tokens = 0, want 1200`

- [ ] **Step 3: Handler'a ekle**

`mf-backend/internal/config/handler.go` içindeki `Config` metodunun `common.JSON` çağrısına, `"scoring"` anahtarından sonra ekle:

```go
		// The prompt window the frontend must size its case field against.
		// Published rather than duplicated on the client: this number is tuned
		// per deployment, and a client-side copy would drift silently until a
		// case that looks acceptable comes back as a raw 400 from the engine.
		//
		// The chars-per-token ratios stay on the client. They are properties of
		// the tokenizer and the language, not of this deployment, and they do
		// not move when an operator changes the window.
		"limits": map[string]any{
			"max_prompt_tokens": h.cfg.LLMMaxPromptTokens,
		},
```

- [ ] **Step 4: Testlerin geçtiğini doğrula**

Run: `cd mf-backend && go test ./internal/config/ -v && go test ./...`
Expected: PASS, tüm paketler yeşil

- [ ] **Step 5: Commit**

```bash
git add mf-backend/internal/config/handler.go mf-backend/internal/config/config_test.go
git commit -m "feat(config): publish the prompt window the case field has to fit

The analysis screen sizes its case field against the engine's input window,
and hardcoding it on the client would be a second independent estimate of a
number the operator tunes — drifting silently until a case that looked fine
comes back as a raw 400.

The chars-per-token ratios deliberately stay on the client: those are
properties of the tokenizer and of Turkish, not of this deployment."
```

---

### Task 4: API istemcisi — rubrik prompt'unu okuyabilmek

Bütçe, seçili rubriğin sistem prompt'unun **gerçek** uzunluğuna bağlı ve iki rubrik arasında 565 karakter fark var. Endpoint zaten var (`GET /analysis/domains/{slug}/prompt`), istemci metodu yok.

**Files:**
- Modify: `mf-frontend/src/lib/types.ts` (analiz bölümünün sonuna)
- Modify: `mf-frontend/src/lib/api.ts:220` civarı (`analysisGet`'ten sonra)

**Interfaces:**
- Consumes: `Criterion` (types.ts'te tanımlı)
- Produces:
  - `interface AnalysisPrompt { domain: string; version: number; system_prompt: string; user_prompt_example: string; criteria: Criterion[]; temperature: number }`
  - `interface AppLimits { max_prompt_tokens: number }`
  - `api.analysisPrompt(slug: string): Promise<AnalysisPrompt>`

- [ ] **Step 1: Tipleri ekle**

`mf-frontend/src/lib/types.ts` içinde `AssessmentList` tanımından **sonra** ekle:

```ts
/**
 * GET /analysis/domains/{slug}/prompt — modele giden prompt'un kendisi.
 *
 * Ekran bunu prompt'u göstermek için değil, **ölçmek** için okuyor: vaka
 * alanına kalan yer, pencereden sistem prompt'u düşüldükten sonra ne kalıyorsa
 * o, ve iki rubriğin sistem prompt'u arasında 565 karakter fark var.
 */
export interface AnalysisPrompt {
  domain: string;
  version: number;
  system_prompt: string;
  user_prompt_example: string;
  criteria: Criterion[];
  temperature: number;
}

/** GET /config'in `limits` bloğu. */
export interface AppLimits {
  max_prompt_tokens: number;
}
```

- [ ] **Step 2: İstemci metodunu ekle**

`mf-frontend/src/lib/api.ts` içinde `analysisGet` satırından **sonra** ekle:

```ts
  // Prompt'un kendisi, gösterilmek için değil ölçülmek için: vaka alanının
  // karakter bütçesi seçili rubriğin sistem prompt'u kadar küçülüyor.
  analysisPrompt: (slug: string) =>
    request<AnalysisPrompt>(`/analysis/domains/${slug}/prompt`),
```

Dosyanın en üstündeki `import type { ... } from "./types"` listesine `AnalysisPrompt` ekle.

- [ ] **Step 3: Tip kontrolü ve lint**

Run: `cd mf-frontend && npx tsc --noEmit && npm run lint`
Expected: temiz

- [ ] **Step 4: Commit**

```bash
git add mf-frontend/src/lib/types.ts mf-frontend/src/lib/api.ts
git commit -m "feat(frontend): read the rubric's prompt, to measure it not to show it

The case field's budget is the window minus the selected rubric's system
prompt, and the two shipped rubrics differ by 565 characters — so the number
has to come from the rubric in hand, not from a constant."
```

---

### Task 5: Boş ekran ve AppShell bağlantısı

Ekran önce **ulaşılabilir** olmalı; formu ve raporu üstüne koyacağız.

**Files:**
- Create: `mf-frontend/src/components/views/AnalizView.tsx`
- Modify: `mf-frontend/src/components/AppShell.tsx:24` (MasterView tipi), `:26-44` (yorum ve NAV), `:212-227` (Pane'ler), ikon eklemesi

**Interfaces:**
- Produces: `export function AnalizView(): React.ReactElement`

- [ ] **Step 1: Görünümün iskeletini yaz**

Create `mf-frontend/src/components/views/AnalizView.tsx`:

```tsx
"use client";

// Rubrik analizi: bir vaka girer, rubrik-puanlı bir rapor çıkar.
//
// Ürünün kendisi bu ekran. Motor uzun süredir çalışıyordu ama yalnızca API ve
// MCP üzerinden ulaşılabiliyordu, yani alıcıya açıp gösterilebilecek bir yeri
// yoktu — satılabilir ama gösterilemez bir durum.
//
// Neden bir sohbet değil de form: rubrik doldurmak çıkarım işidir, konuşma
// değil. Aynı vaka aynı okumayı vermeli, ve ürünün satıldığı şey tam olarak o
// tutarlılık. Sıcaklık da bu yüzden backend'de sabitlenmiş (analysisTemperature
// 0.1), operatörün sohbet için ayarladığı bir kaydırıcıya bırakılmamış.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { AnalysisDomain } from "@/lib/types";

export function AnalizView() {
  const [domains, setDomains] = useState<AnalysisDomain[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);

  const loadDomains = useCallback(() => {
    api
      .analysisDomains()
      .then((d) => {
        setDomains(d.domains);
        setLoadError(null);
      })
      .catch((e: unknown) =>
        setLoadError(e instanceof ApiError ? e.message : "Rubrikler yüklenemedi."),
      );
  }, []);

  useEffect(loadDomains, [loadDomains]);

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-6xl px-4 sm:px-5 py-6">
        <h1 className="font-display text-lg font-semibold">Analiz</h1>
        <p className="text-sm mt-1" style={{ color: "var(--text-dim)" }}>
          Bir vaka girin, rubrik-puanlı bir rapor alın.
        </p>

        {loadError && (
          <div className="notice notice-bad mt-4" role="alert">
            {loadError}
          </div>
        )}

        {!loadError && domains.length === 0 && (
          <p className="mono text-xs mt-4" style={{ color: "var(--text-faint)" }}>
            rubrikler yükleniyor…
          </p>
        )}

        {domains.length > 0 && (
          <p className="mono text-xs mt-4" style={{ color: "var(--text-faint)" }}>
            {domains.length} rubrik yüklendi
          </p>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: AppShell'e bağla**

`mf-frontend/src/components/AppShell.tsx` içinde dört düzenleme:

**(a)** Import ekle, `PersonaView` importunun yanına:

```tsx
import { AnalizView } from "./views/AnalizView";
```

**(b)** `MasterView` tipini genişlet (satır 24):

```tsx
export type MasterView = "analiz" | "codegen" | "persona" | "metrics" | "admin";
```

**(c)** NAV bloğunu ve üstündeki yorumu değiştir (satır 26-44). Eski yorum "üreteç başta, çünkü kutunun servis ettiği şey o" diyor; o gerekçe artık ürünü ikinci sıraya koyuyor:

```tsx
// Analiz başta çünkü ürün o: bir vaka girer, rubrik-puanlı ve denetlenebilir
// bir rapor çıkar. Sıra uzun süre üreteçteydi ve gerekçesi "kutunun servis
// ettiği şey o" idi — makinede ne yüklüyse ona göre bir sıralama. O gerekçe
// ürünün kendisini ikinci sıraya koyuyordu, ve makinede ne yüklü olduğu
// nav sırasının cevaplaması gereken bir soru değil.
//
// Üreteç ve Persona kalıyor: ikisi de çalışan yüzeyler, ve ikisi de makine
// kapalıyken okunabilir biçimde başarısız oluyor. Yönetim ve Metrikler herkese
// listeleniyor, rol gereksinimini görünümün kendisi anlatıyor — kaybolan bir
// nav öğesinden dostane.
const NAV: { id: MasterView; label: string; Icon: () => React.ReactElement }[] = [
  { id: "analiz", label: "Analiz", Icon: IconRubric },
  { id: "codegen", label: "Üreteç", Icon: IconCode },
  { id: "persona", label: "Persona", Icon: IconSpark },
  { id: "metrics", label: "Metrikler", Icon: IconChart },
  { id: "admin", label: "Yönetim", Icon: IconSliders },
];
```

**(d)** `initialRoute`'un varsayılanını değiştir (satır 99):

```tsx
  return parsed ?? { view: "analiz", sub: "" };
```

**(e)** Pane'i ekle. `<main>` içinde, `codegen` Pane'inden **önce**:

```tsx
        {/* Kalıcı mount edilen grupta, Üreteç ve Persona ile birlikte: bir
            analiz tünelin ardındaki makinede onlarca saniye sürüyor, ve view
            söküldüğünde isteği tutan bileşen de gidiyor — iş durmuyor, kayıt
            yine yazılıyor, ama sonucun ineceği yer kalmıyor. */}
        {opened.has("analiz") && (
          <Pane active={view === "analiz"}>
            <AnalizView />
          </Pane>
        )}
```

**(f)** İkonu ekle, dosyanın sonundaki ikon bloğuna. Rubrik: satırları ve işaretlenmiş bir kutusu olan bir liste.

```tsx
function IconRubric() {
  return (
    <svg {...SVG} aria-hidden>
      <path d="M2.5 3.5h11v9h-11z" />
      <path d="M5 6.5h6M5 9.5h4" />
    </svg>
  );
}
```

- [ ] **Step 3: Tarayıcıda doğrula**

Run: `cd mf-frontend && npm run dev`

Tarayıcıda `http://localhost:3000` aç, giriş yap. Beklenen:
- Nav'da beş öğe, "Analiz" başta ve seçili.
- Ekran "N rubrik yüklendi" yazıyor (backend ayaktaysa; `NEXT_PUBLIC_API_URL` doğru olmalı).
- `#codegen` gibi eski derin bağlantılar hâlâ çalışıyor.

- [ ] **Step 4: Tip kontrolü, lint, build**

Run: `cd mf-frontend && npx tsc --noEmit && npm run lint && npm run build`
Expected: üçü de temiz

- [ ] **Step 5: Commit**

```bash
git add mf-frontend/src/components/views/AnalizView.tsx mf-frontend/src/components/AppShell.tsx
git commit -m "feat(frontend): give the product a screen, and put it first

internal/analysis has been live and unreachable from the UI: api.analysisRun()
sat in the client with no component calling it, so the one thing this sells
could not be shown to anyone.

The nav order changes with it. The old reasoning — the generator leads because
it is what the box serves — sorted the product by whatever weights happened to
be loaded, which is not a question nav order should be answering.

Mounted in the kept-alive group, for the reason AppShell already documents: an
analysis takes tens of seconds and unmounting leaves its result nowhere to land."
```

---

### Task 6: Form ve karakter bütçesi

Ekranın ilk yaptığı iş vaka yapıştırmak, ve ilk karşılaşılacak hata metnin pencereye sığmaması. Sayaç ana öğe.

**Files:**
- Modify: `mf-frontend/src/components/views/AnalizView.tsx`

**Interfaces:**
- Consumes: `breakdown` henüz kullanılmıyor; `caseBudgetChars`, `estimateTokens` (Task 2), `api.analysisPrompt` (Task 4), `api.config` (mevcut)
- Produces: bileşen içi durum; dışa yeni arayüz yok

- [ ] **Step 1: Durum ve veri yüklemeyi genişlet**

Tip importunu genişlet:

```tsx
import type { AnalysisDomain, AppLimits } from "@/lib/types";
```

`AnalizView` bileşeninin içindeki durumu şu hale getir (mevcut `domains`/`loadError` korunur):

```tsx
  const [domains, setDomains] = useState<AnalysisDomain[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [slug, setSlug] = useState("");
  const [title, setTitle] = useState("");
  const [subject, setSubject] = useState("");

  // Pencere dağıtıma göre değişiyor, o yüzden sunucudan geliyor. Ölçülen
  // karakter/token oranları lib/rubric.ts'te sabit — onlar dile ve tokenizer'a
  // ait, bu dağıtıma değil.
  const [windowTokens, setWindowTokens] = useState(1200);
  const [systemChars, setSystemChars] = useState<number | null>(null);
```

- [ ] **Step 2: Rubrik yüklendiğinde ilkini seç, pencereyi oku**

`loadDomains`'in `.then` bloğunu şu hale getir, ve altına yeni bir effect ekle:

```tsx
      .then((d) => {
        setDomains(d.domains);
        setLoadError(null);
        // Varsayılan rubrik yatırılabilirlik: beachhead ICP hızlandırma
        // programları ve melek ağları.
        const first =
          d.domains.find((x) => x.slug === "startup-investability") ?? d.domains[0];
        if (first) setSlug((s) => s || first.slug);
      })
```

```tsx
  // Pencere: sunucunun bildirdiği sayı, alınamazsa backend'in kendi varsayılanı.
  useEffect(() => {
    api
      .config()
      .then((c) => {
        const limits = (c as { limits?: Partial<AppLimits> }).limits;
        if (limits?.max_prompt_tokens) setWindowTokens(limits.max_prompt_tokens);
      })
      .catch(() => {
        /* Varsayılan yeterli: bütçe biraz yanılır, gönderim engellenmez. */
      });
  }, []);

  // Seçili rubriğin sistem prompt'u ne kadar yer yiyor. İki rubrik arasında
  // 565 karakter fark var, yani bu rubrik başına okunmak zorunda.
  useEffect(() => {
    if (!slug) return;
    let cancelled = false;
    setSystemChars(null);
    api
      .analysisPrompt(slug)
      .then((p) => {
        if (!cancelled) setSystemChars(p.system_prompt.length);
      })
      .catch(() => {
        if (!cancelled) setSystemChars(null);
      });
    return () => {
      cancelled = true;
    };
  }, [slug]);
```

- [ ] **Step 3: Bütçeyi hesapla ve formu çiz**

Import satırına ekle:

```tsx
import { caseBudgetChars, estimateTokens } from "@/lib/rubric";
```

Bileşenin `return`'ünden önce:

```tsx
  // systemChars henüz gelmediyse bütçe gösterilmiyor: yanlış bir sayı
  // göstermek, sayı göstermemekten kötü.
  const budget = systemChars === null ? null : caseBudgetChars(windowTokens, systemChars);
  const over = budget !== null && subject.length > budget;
  const canRun = slug !== "" && subject.trim() !== "" && !over;
```

`<h1>` bloğundan sonra formu ekle:

```tsx
        <div className="card mt-5 p-4 space-y-4">
          <div>
            <label className="label" htmlFor="analiz-rubrik">
              Rubrik
            </label>
            <select
              id="analiz-rubrik"
              className="input"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
            >
              {domains.map((d) => (
                <option key={d.slug} value={d.slug}>
                  {d.name} · v{d.version} · {d.criteria.length} kriter
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="label" htmlFor="analiz-baslik">
              Vaka başlığı
            </label>
            <input
              id="analiz-baslik"
              className="input"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Şirket veya vaka adı"
            />
          </div>

          <div>
            <div className="flex items-baseline justify-between gap-3">
              <label className="label" htmlFor="analiz-vaka">
                Vaka metni
              </label>
              <span
                className="mono text-xs num"
                style={{ color: over ? "var(--bad)" : "var(--text-faint)" }}
              >
                {subject.length}
                {budget !== null && ` / ${budget}`} karakter
                {budget !== null && ` · ~${estimateTokens(subject)} token`}
              </span>
            </div>
            <textarea
              id="analiz-vaka"
              className="input"
              rows={10}
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
              placeholder="Değerlendirilecek metni buraya yapıştırın."
              aria-invalid={over}
              aria-describedby={over ? "analiz-vaka-uyari" : undefined}
            />
          </div>

          {over && budget !== null && (
            <div className="notice notice-warn" id="analiz-vaka-uyari" role="alert">
              Metin bu rubriğin bıraktığı yerden {subject.length - budget} karakter
              uzun. Rubriğin kendisi de modele gönderiliyor ve pencereden yer
              kaplıyor; bu sınırın üstünde istek değerlendirilmeden reddedilir.
            </div>
          )}

          <div className="flex items-center gap-3">
            <button className="btn btn-primary" disabled={!canRun}>
              Analiz et
            </button>
          </div>
        </div>
```

- [ ] **Step 4: Tarayıcıda doğrula**

Run: `cd mf-frontend && npm run dev`

Beklenen:
- Rubrik seçicide iki rubrik, yatırılabilirlik seçili.
- Sayaç `0 / 606 karakter` gösteriyor; pazarlama rubriğine geçince 1001'e **çıkıyor**.
- Sınırı aşan bir metin yapıştırıldığında sayaç kırmızıya dönüyor, uyarı çıkıyor, buton pasifleşiyor.

- [ ] **Step 5: Tip kontrolü, lint, build**

Run: `cd mf-frontend && npx tsc --noEmit && npm run lint && npm run build`
Expected: temiz

- [ ] **Step 6: Commit**

```bash
git add mf-frontend/src/components/views/AnalizView.tsx
git commit -m "feat(frontend): show the case budget, because it is smaller than anyone expects

Measured with Qwen3-4B's own tokenizer: the investability rubric's system
prompt is 841 tokens of the shipped 1200-token window, which leaves the case
606 characters. That is ninety words, not a deck, and the operator has to see it
before pasting rather than after a 400 with nothing on screen to explain it.

The budget is read per rubric rather than assumed: the two shipped rubrics
differ by 565 characters of system prompt."
```

---

### Task 7: Analizi çalıştır — durumlar ve hatalar

**Files:**
- Modify: `mf-frontend/src/components/views/AnalizView.tsx`

**Interfaces:**
- Consumes: `api.analysisRun`, `useMachine().begin`
- Produces: bileşen içi `assessment` durumu; Task 8 raporu çizerken bunu okuyor

- [ ] **Step 1: Durum ve çalıştırma fonksiyonunu ekle**

Import satırlarına ekle:

```tsx
import { useMachine } from "@/store/machine";
```

Tip importunu genişlet:

```tsx
import type { AnalysisDomain, Assessment } from "@/lib/types";
```

Durumlara ekle:

```tsx
  const { begin } = useMachine();
  const [running, setRunning] = useState(false);
  const [runError, setRunError] = useState<string | null>(null);
  const [assessment, setAssessment] = useState<Assessment | null>(null);
```

`canRun` tanımından sonra ekle:

```tsx
  const run = useCallback(async () => {
    if (running) return;
    setRunning(true);
    setRunError(null);
    // Durum çubuğu geçen süreyi sayabilsin diye. Bitirici `Run` bekliyor,
    // `Assessment` o değil — null geçiliyor, yani çubuk süreyi gösterir ama
    // son koşu özetini doldurmaz. Store'u genişletmenin bedeli bu ekran için
    // faydasından büyük.
    const done = begin("analiz");
    try {
      const result = await api.analysisRun({
        domain: slug,
        subject,
        subject_title: title.trim() || "Adsız vaka",
      });
      setAssessment(result);
    } catch (e: unknown) {
      setRunError(describeRunError(e));
    } finally {
      done(null);
      setRunning(false);
    }
  }, [begin, running, slug, subject, title]);
```

- [ ] **Step 2: Hata çevirmenini yaz**

Dosyanın **sonuna**, bileşenin dışına ekle:

```tsx
// Hataları operatörün okuyabileceği cümlelere çevirir.
//
// Ham gövdeyi ekrana basmak burada işe yaramıyor: bu yolun iki tipik hatası da
// başka bir katmandan geliyor ve o katmanın diliyle konuşuyor. 503 çıkarım
// makinesinin kapalı olması demek — desteklenen bir hal, arıza değil: API
// ayakta kalır ve tarayıcı tarafı etkilenmez. 400 ise neredeyse her zaman
// metnin pencereye sığmaması, ve sayaç bunu zaten önceden söylüyor olmalıydı.
function describeRunError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 503) {
      return "Çıkarım makinesi şu anda ulaşılamıyor. Analiz için gerekli, ama diğer ekranlar çalışmaya devam eder.";
    }
    if (e.status === 400) {
      return "Vaka metni değerlendirilmeden reddedildi — büyük olasılıkla rubriğin bıraktığı yerden uzun. Metni kısaltıp tekrar deneyin.";
    }
    if (e.status === 504 || e.status === 408) {
      return "Analiz zaman aşımına uğradı. Tekrar denemek isteğin sırasını ikiye katlar; önce bir süre bekleyin.";
    }
    return e.message;
  }
  return "Analiz tamamlanamadı.";
}
```

- [ ] **Step 3: Butonu bağla ve durumları çiz**

Butonu şu hale getir:

```tsx
            <button className="btn btn-primary" disabled={!canRun || running} onClick={run}>
              {running ? "Analiz ediliyor…" : "Analiz et"}
            </button>
            {running && (
              <span className="mono text-xs" style={{ color: "var(--text-faint)" }}>
                model tüm rubriği tek seferde dolduruyor; bu bir dakikayı bulabilir
              </span>
            )}
```

Form kartının **sonrasına** ekle:

```tsx
        {runError && (
          <div className="notice notice-bad mt-4" role="alert">
            {runError}
          </div>
        )}
```

- [ ] **Step 4: Tarayıcıda doğrula**

Run: `cd mf-frontend && npm run dev`

Beklenen:
- Kısa bir vaka ile "Analiz et" çalışıyor; buton "Analiz ediliyor…" oluyor, üstteki durum çubuğu saniye sayıyor.
- Çalışırken Üreteç'e geçip Analiz'e dönmek sonucu **kaybettirmiyor**.
- Çıkarım kapalıyken okunabilir bir 503 cümlesi çıkıyor, ekran çökmüyor.

- [ ] **Step 5: Tip kontrolü, lint, build**

Run: `cd mf-frontend && npx tsc --noEmit && npm run lint && npm run build`
Expected: temiz

- [ ] **Step 6: Commit**

```bash
git add mf-frontend/src/components/views/AnalizView.tsx
git commit -m "feat(frontend): run an analysis, and fail in sentences

Both typical failures on this path come from another layer and speak its
language. 503 is the inference host being off — a supported state, not a
fault, and the rest of the app keeps working through it. 400 is almost always
the case not fitting the window, which the counter should already have said.

Retry is deliberately not offered on timeout: the card serves one request at
a time, so a retry doubles the queue rather than the chances."
```

---

### Task 8: Raporu çiz

Ürünün tüm iddiası bu bölümde: rubrik açık, kanıt alıntılı, aritmetik görünür.

**Files:**
- Modify: `mf-frontend/src/components/views/AnalizView.tsx`

**Interfaces:**
- Consumes: `breakdown` (Task 1), `Assessment`
- Produces: `function Rapor({ assessment }: { assessment: Assessment }): React.ReactElement`

- [ ] **Step 1: Rapor bileşenini yaz**

Import satırını genişlet:

```tsx
import { breakdown, caseBudgetChars, estimateTokens } from "@/lib/rubric";
```

Dosyanın sonuna, `describeRunError`'ın yanına ekle:

```tsx
/** Bir oranı yüzdeye çevirir. */
const pct = (x: number) => `%${Math.round(x * 100)}`;

/**
 * Tamamlanmış bir raporun görünümü.
 *
 * İki kural pazarlık konusu değil ve ikisi de types.ts'te yazılı:
 * `score: null` sıfır değildir, ve `overall_score` kapsamsız gösterilmez.
 * Birincisi sessizliği başarısızlık diye puanlar, ikincisi 0.9 kapsamdaki 68
 * ile 0.3 kapsamdaki 68'i aynı şey gibi gösterir.
 */
function Rapor({ assessment }: { assessment: Assessment }) {
  const b = breakdown(assessment.criteria_snapshot, assessment.findings);

  return (
    <article className="card mt-5 p-4 rapor">
      <header className="flex flex-wrap items-start justify-between gap-4 pb-4"
        style={{ borderBottom: "1px solid var(--line)" }}>
        <div className="min-w-0">
          <h2 className="font-display text-base font-semibold truncate">
            {assessment.subject_title}
          </h2>
          <p className="mono text-xs mt-1" style={{ color: "var(--text-faint)" }}>
            {assessment.domain_name} · v{assessment.domain_version} ·{" "}
            {new Date(assessment.created_at).toLocaleString("tr-TR")}
          </p>
        </div>

        <div className="flex items-baseline gap-4 shrink-0">
          <div>
            <div className="eyebrow">Toplam</div>
            <div className="num font-display text-2xl">
              {b.overall === null ? "—" : b.overall.toFixed(1)}
            </div>
          </div>
          {/* Kapsam puanın yanından ayrılmaz: aynı sayı farklı kapsamlarda
              farklı bulgudur. */}
          <div>
            <div className="eyebrow">Kapsam</div>
            <div className="num font-display text-2xl">{pct(b.coverage)}</div>
          </div>
        </div>
      </header>

      {b.overall === null && (
        <div className="notice notice-warn mt-4">
          Bu vakada değerlendirilebilecek hiçbir kriter bulunamadı. Bu, sıfır puan
          değildir — metin rubriğin sorduğu şeylere dair kanıt taşımıyor.
        </div>
      )}

      <div className="mt-4 space-y-2">
        {b.rows.map((row) => (
          <details key={row.criterion.key} className="well p-3">
            <summary className="flex flex-wrap items-center gap-x-3 gap-y-1 cursor-pointer">
              <span className="font-medium text-sm flex-1 min-w-0">
                {row.criterion.label}
              </span>

              <span className="mono text-xs num" style={{ color: "var(--text-faint)" }}>
                ağırlık {row.criterion.weight.toFixed(2)}
              </span>

              {row.scored ? (
                <>
                  <span className="pill pill-brand num">
                    {row.clamped} / {row.criterion.scale_max > 0 ? row.criterion.scale_max : 5}
                  </span>
                  {/* Katkı kolonu: bu sayıların toplamı yukarıdaki toplam
                      puana eşittir. Raporun savunulabilir olmasının sebebi. */}
                  <span className="mono text-xs num" style={{ color: "var(--text-dim)" }}>
                    +{row.points!.toFixed(1)}
                  </span>
                </>
              ) : (
                <span className="pill pill-warn">kanıt yok</span>
              )}
            </summary>

            <div className="mt-3 space-y-2">
              {row.criterion.description && (
                <p className="text-xs" style={{ color: "var(--text-faint)" }}>
                  {row.criterion.description}
                </p>
              )}

              {row.finding?.rationale && (
                <p className="text-sm" style={{ color: "var(--text-dim)" }}>
                  {row.finding.rationale}
                </p>
              )}

              {row.finding && row.finding.evidence.length > 0 ? (
                <ul className="space-y-1.5">
                  {row.finding.evidence.map((q, i) => (
                    // Birebir alıntı, parafraz değil: parafraz kaynağa karşı
                    // doğrulanamaz, ve doğrulanamayan bir atıf sağlam göründüğü
                    // için hiç atıf olmamasından kötüdür.
                    <li
                      key={i}
                      className="text-sm pl-3"
                      style={{ borderLeft: "2px solid var(--brand-line)", color: "var(--text)" }}
                    >
                      “{q}”
                    </li>
                  ))}
                </ul>
              ) : (
                !row.scored && (
                  <p className="text-xs" style={{ color: "var(--text-faint)" }}>
                    Metinde bu kritere dair bir ifade bulunamadı. Kanıtsız kriterler
                    toplamdan düşer ve kapsam sayısında raporlanır — düşük puan
                    olarak sayılmaz.
                  </p>
                )
              )}
            </div>
          </details>
        ))}
      </div>

      <footer className="mt-4 pt-3 flex flex-wrap gap-x-4 gap-y-1 mono text-xs"
        style={{ borderTop: "1px solid var(--line)", color: "var(--text-faint)" }}>
        <span>model {assessment.model}</span>
        <span className="num">{(assessment.latency_ms / 1000).toFixed(1)} sn</span>
        <span className="num">
          {assessment.prompt_tokens} + {assessment.completion_tokens} token
        </span>
        {/* Gizlenmiyor: şemayı tutturamamış bir cevap operatörün bilmesi
            gereken bir şey, ve rapor yine de okunabilir kalıyor. */}
        {!assessment.schema_valid && <span style={{ color: "var(--warn)" }}>şema onarıldı</span>}
      </footer>
    </article>
  );
}
```

- [ ] **Step 2: Ekrana bağla**

`runError` bloğundan sonra ekle:

```tsx
        {assessment && <Rapor assessment={assessment} />}
```

- [ ] **Step 3: Aritmetiği gerçek bir raporla doğrula**

Bir analiz çalıştır ve tarayıcı konsolunda kontrol et: satırlardaki `+X.X`
değerlerinin toplamı, başlıktaki toplam puana yuvarlama farkı içinde eşit
olmalı. Eşit değilse Task 1'e dön — kolon toplamıyla çelişen bir rapor
yayınlanamaz.

- [ ] **Step 4: Tip kontrolü, lint, build**

Run: `cd mf-frontend && npm test && npx tsc --noEmit && npm run lint && npm run build`
Expected: temiz

- [ ] **Step 5: Commit**

```bash
git add mf-frontend/src/components/views/AnalizView.tsx
git commit -m "feat(frontend): render the report, arithmetic included

A report that says 68 is an opinion nobody can contest. A report that says 68
because traction rated 2 of 5 on these two quoted lines, at weight 0.20, is a
finding an applicant can appeal and an operator can defend — so the
contribution column ships, and its numbers sum to the total above them.

Two invariants the types have documented since before there was a screen: a
null score renders as no evidence, never as zero, and the score never appears
without its coverage beside it."
```

---

### Task 9: Geçmiş listesi

**Files:**
- Modify: `mf-frontend/src/components/views/AnalizView.tsx`

**Interfaces:**
- Consumes: `api.analysisList`, `api.analysisGet`
- Produces: bileşen içi `refreshHistory` (Task 7 bunu çağırıyor)

- [ ] **Step 1: Durum ve yükleyiciyi ekle**

Tip importunu genişlet:

```tsx
import type { AnalysisDomain, Assessment, AssessmentSummary } from "@/lib/types";
```

Durumlara ekle:

```tsx
  const [history, setHistory] = useState<AssessmentSummary[]>([]);
```

`loadDomains`'in yanına ekle:

```tsx
  const refreshHistory = useCallback(
    () =>
      api
        .analysisList(20)
        .then((r) => setHistory(r.assessments))
        .catch(() => {
          /* Geçmiş ikincil: alınamaması ekranın geri kalanını durdurmaz. */
        }),
    [],
  );

  useEffect(() => {
    void refreshHistory();
  }, [refreshHistory]);
```

**Not:** Bu tanım `run`'dan **önce** gelmeli — bir sonraki adım `run`'ın içinden
çağırıyor.

- [ ] **Step 2: Yeni raporun listeye düşmesini sağla**

Task 7'de yazılan `run` fonksiyonunda, `setAssessment(result);` satırından
hemen sonra ekle:

```tsx
      // Yeni rapor listenin başına geçsin.
      void refreshHistory();
```

ve bağımlılık listesini genişlet:

```tsx
  }, [begin, running, slug, subject, title, refreshHistory]);
```

- [ ] **Step 3: Listeyi çiz**

Raporun **sonrasına** ekle:

```tsx
        {history.length > 0 && (
          <section className="mt-8 gecmis">
            <h2 className="eyebrow">Önceki raporlar</h2>
            <div className="mt-2 space-y-1">
              {history.map((h) => (
                <button
                  key={h.id}
                  className="card card-action w-full text-left p-3 flex flex-wrap items-center gap-x-4 gap-y-1"
                  onClick={() =>
                    api
                      .analysisGet(h.id)
                      .then(setAssessment)
                      .catch(() => setRunError("Rapor açılamadı."))
                  }
                >
                  <span className="text-sm flex-1 min-w-0 truncate">{h.subject_title}</span>
                  <span className="mono text-xs" style={{ color: "var(--text-faint)" }}>
                    {h.domain_name}
                  </span>
                  <span className="mono text-xs num" style={{ color: "var(--text-dim)" }}>
                    {h.overall_score === null ? "—" : h.overall_score.toFixed(1)} ·{" "}
                    {pct(h.coverage)}
                  </span>
                  <span className="mono text-xs" style={{ color: "var(--text-faint)" }}>
                    {new Date(h.created_at).toLocaleDateString("tr-TR")}
                  </span>
                </button>
              ))}
            </div>
          </section>
        )}
```

- [ ] **Step 4: Tarayıcıda doğrula**

Beklenen: liste dolu; bir satıra tıklamak o raporu yukarıda açıyor; yeni bir analiz listenin başına ekleniyor. Listede puanın yanında **her zaman** kapsam var.

- [ ] **Step 5: Tip kontrolü, lint, build**

Run: `cd mf-frontend && npx tsc --noEmit && npm run lint && npm run build`
Expected: temiz

- [ ] **Step 6: Commit**

```bash
git add mf-frontend/src/components/views/AnalizView.tsx
git commit -m "feat(frontend): list past reports, coverage always beside the score

The list projection deliberately carries coverage, and it is rendered here for
the same reason the report renders it: the same number means different things
at different coverage, and a list is exactly where that gets forgotten."
```

---

### Task 10: Yazdırma

Dışarı çıkan satış materyali bu kuralın çıktısı olacak.

**Files:**
- Modify: `mf-frontend/src/app/globals.css` (dosyanın sonuna)

- [ ] **Step 1: Yazdırma kurallarını ekle**

`mf-frontend/src/app/globals.css` dosyasının **sonuna** ekle:

```css
/* Yazdırma ------------------------------------------------------------
   Örnek rapor buradan çıkıyor: ekran görüntüsü yerine yazdırma seçildi çünkü
   kural bir kez yazılıyor ve çıktısı seçilebilir, aranabilir metin oluyor.

   Koyu tema kağıda gitmez — mürekkep bir yana, kanıt alıntıları okunmaz. Bu
   yüzden yazdırmada renk değişkenleri açık bir palete çevriliyor; ekranın
   koyu-tek-seçenek olması bir tasarım kararı, kağıdın beyaz olması bir
   kısıt. */
@media print {
  :root {
    --bg: #ffffff;
    --bg-sunk: #ffffff;
    --panel: #ffffff;
    --panel-2: #ffffff;
    --panel-3: #ffffff;
    --line: #d0d7de;
    --line-strong: #a8b3bd;
    --text: #10161c;
    --text-dim: #3d4b58;
    --text-faint: #5c6b78;
    --brand: #a8460f;
    --brand-line: #a8460f;
    --bevel: none;
    --shadow-1: none;
    --shadow-2: none;
    --shadow-3: none;
  }

  /* Ekranın kendisi olan şeyler kağıda gitmez. */
  header,
  nav,
  .skip-link,
  button,
  .gecmis,
  .card:not(.rapor) {
    display: none !important;
  }

  /* Rapor kartı kağıtta bir çerçeve değil, sayfanın kendisi. */
  .rapor {
    border: none !important;
    box-shadow: none !important;
    padding: 0 !important;
    margin: 0 !important;
  }

  /* Kanıtın tamamı açılır: katlanmış bir alıntı, denetlenemeyen bir alıntıdır
     ve bu raporun tek satış argümanı denetlenebilir olması. */
  details {
    break-inside: avoid;
  }
  details > div {
    display: block !important;
  }

  body {
    background: #ffffff;
  }
}
```

- [ ] **Step 2: `<details>`'in yazdırmada açık gelmesini sağla**

CSS tek başına `<details>`'i açmıyor. `Rapor` bileşenindeki `<details>` etiketine `open` ekle — kanıt zaten varsayılan olarak görünür olmalı, ve katlanma yalnızca uzun raporda okumayı kolaylaştıran bir seçenek:

```tsx
          <details key={row.criterion.key} className="well p-3" open>
```

- [ ] **Step 3: Doğrula**

Bir raporu ekranda aç, `Cmd/Ctrl+P` bas. Beklenen:
- Beyaz zemin, siyah metin, okunabilir alıntılar.
- Nav, başlık şeridi, butonlar ve geçmiş listesi yok.
- Tüm kriterler ve kanıtları açık.
- Bir kriterin kutusu iki sayfaya bölünmüyor.

PDF'e kaydet ve metnin **seçilebilir** olduğunu doğrula.

- [ ] **Step 4: Tip kontrolü, lint, build**

Run: `cd mf-frontend && npx tsc --noEmit && npm run lint && npm run build`
Expected: temiz

- [ ] **Step 5: Commit**

```bash
git add mf-frontend/src/app/globals.css mf-frontend/src/components/views/AnalizView.tsx
git commit -m "feat(frontend): make the report printable, so the sales asset is text

What leaves the building is a PDF of the real screen rather than a screenshot:
the rule is written once, and the output is selectable, searchable text rather
than a picture somebody could have made in Photoshop.

The dark theme is a design choice for the screen and a constraint on paper —
evidence quotes are unreadable in it — so print flips the tokens to a light
palette instead of asking the reader to."
```

---

### Task 11: Uçtan uca doğrulama

**Files:** yok — yalnızca doğrulama

- [ ] **Step 1: Tüm kapılar**

Run:
```bash
cd mf-frontend && npm test && npx tsc --noEmit && npm run lint && npm run build
cd ../mf-backend && go test ./...
```
Expected: hepsi yeşil

- [ ] **Step 2: Bitmiş sayılma koşullarını tek tek geçir**

Spec'in listesi (`docs/superpowers/specs/2026-07-30-analiz-ekrani-design.md`):

- [ ] Analiz nav'da, tıklanınca açılıyor, rubrikleri yüklüyor
- [ ] Vaka yapıştırılıp çalıştırılabiliyor; sınır aşılırsa gönderimden önce uyarıyor
- [ ] Çalışırken başka bir view'a geçip dönmek sonucu kaybettirmiyor
- [ ] Rapor, `criteria_snapshot`'taki her kriter için ya alıntılı puan ya "kanıt yok" gösteriyor; hiçbir null 0 olarak çizilmiyor
- [ ] Toplam puan kapsamla birlikte; katkı kolonunun toplamı toplam puana eşit
- [ ] Geçmişten eski rapor açılabiliyor
- [ ] `Cmd/Ctrl+P` okunabilir, metni seçilebilir PDF üretiyor

- [ ] **Step 3: Bulunan farkları düzelt, sonra commit**

```bash
git add -A
git commit -m "fix(frontend): close the gaps found running the screen end to end"
```

---

## Bu planın kapsamadıkları

Spec'te yazılı ve bilerek dışarıda:

1. **Sentetik örnek vaka.** 606 karakter bütçesine sığacak şekilde tasarlanmalı, ve sentetik olduğu hem dosyada hem raporda etiketli olmalı.
2. **Kutuya sade taban Qwen3-4B** ve backend'deki dört `defaultModel` sabitinin düzeltilmesi (`analysis`, `wiki`, `decision`, `admin/mcp`) — hepsi şu an Flutter üretecini işaret ediyor.
3. **Örnek raporun üretilip PDF'e alınması.** §7 madde 1 burada kapanır.
4. **Pencere kararı.** 606 karakter bir deck almaz — 1366'ya çekilse bile 953. Kriter başına ayrı çağrı, sistem prompt'unu kırpmak, ya da vakayı özet olarak konumlandırmak — üçü de ayrı kararlar ve örnek raporun ne olacağını belirliyorlar.
5. **`analysis` yoluna backend tarafında girdi uzunluğu koruması.** `decision`'da var, analizde yok; ekrandaki uyarı hatayı okunabilir kılmaya yeter ama korumanın yerini tutmaz.
