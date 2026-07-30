import { test } from "node:test";
import assert from "node:assert/strict";
import {
  breakdown,
  caseBudgetChars,
  estimateTokens,
  PROSE_CHARS_PER_TOKEN,
} from "./rubric.ts";
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

// ---- Prompt butcesi ----

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
