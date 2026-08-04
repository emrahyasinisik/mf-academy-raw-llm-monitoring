import { test } from "node:test";
import assert from "node:assert/strict";
import { ORG_SECTIONS, sectionFromPath } from "./orgNav.ts";

test("kök yol Özet bölümüne düşer", () => {
  assert.equal(sectionFromPath("/sirket"), "ozet");
});

// Next.js bazı yapılandırmalarda sondaki eğik çizgiyi bırakıyor; iki yol da
// aynı bölüm olmalı, yoksa sidebar'da hiçbir şey seçili görünmez.
test("sondaki eğik çizgi bölümü değiştirmez", () => {
  assert.equal(sectionFromPath("/sirket/"), "ozet");
});

test("alt yollar kendi bölümlerine çözülür", () => {
  assert.equal(sectionFromPath("/sirket/ekip"), "ekip");
  assert.equal(sectionFromPath("/sirket/kullanim"), "kullanim");
  assert.equal(sectionFromPath("/sirket/aktivite"), "aktivite");
});

// Bilinmeyen bir alt yolun boş bir kabuk yerine Özet'i seçmesi, ürünün
// geri kalanındaki davranışla aynı: tanınmayan alt görünüm varsayılana düşer.
test("bilinmeyen alt yol Özet'e düşer", () => {
  assert.equal(sectionFromPath("/sirket/olmayan-bir-sey"), "ozet");
});

test("her bölümün yolu kendi bölümüne geri çözülür", () => {
  for (const s of ORG_SECTIONS) {
    assert.equal(sectionFromPath(s.path), s.id);
  }
});
