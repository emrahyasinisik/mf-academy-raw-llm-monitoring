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
