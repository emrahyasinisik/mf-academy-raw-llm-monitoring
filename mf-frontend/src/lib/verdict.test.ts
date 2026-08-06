import assert from "node:assert/strict";
import { test } from "node:test";

import { parseVerdict, stripVerdictLines } from "./verdict.ts";

// The backend reads the same two lines out of the same reply
// (mf-backend/internal/decision/verdict.go). These tests exist because the two
// readers disagreeing is invisible until a badge says 0/100 on a thread the
// server recorded with no score at all.

test("reads the documented format", () => {
  const v = parseVerdict(
    "Pazar büyüyor [2].\n\nKARAR: Yatırılabilir\nSKOR: 72\nGEREKÇE: Çekiş güçlü [4].",
    2,
  );
  assert.deepEqual(v, { label: "Yatırılabilir", score: 72 });
});

test("a clarifying question is not a verdict", () => {
  assert.equal(parseVerdict("Hangi aşamadasın — tohum mu, Seri A mı?", 0), null);
});

test("case-insensitive, as the server's reader is", () => {
  assert.deepEqual(parseVerdict("karar: Temkinli\nskor: 55", 1), {
    label: "Temkinli",
    score: 55,
  });
});

// No sources → no badge (and no report CTA). Greeting turns inventing
// "Karar: şarkı…" must not look like an investability verdict.
test("drops the verdict when the turn gathered no sources", () => {
  assert.equal(
    parseVerdict("KARAR: Yatırılamaz\nSKOR: 0\nGEREKÇE: Kanıt eksik.", 0),
    null,
  );
});

test("ignores free-form karar labels", () => {
  assert.equal(
    parseVerdict("KARAR: Şarkıya dair içerik önerilebilir\nSKOR: 10", 2),
    null,
  );
});

test("keeps a 0 that real sources backed", () => {
  const v = parseVerdict("Pazar doygun [1].\n\nKARAR: Yatırılamaz\nSKOR: 0", 2);
  assert.equal(v?.score, 0);
});

test("strips the protocol lines out of the prose", () => {
  const body = stripVerdictLines("Gerekçe burada.\nKARAR: Temkinli\nSKOR: 55");
  assert.equal(body, "Gerekçe burada.");
});
