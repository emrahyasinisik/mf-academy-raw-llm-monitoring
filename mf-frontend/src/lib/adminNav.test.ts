import { test } from "node:test";
import assert from "node:assert/strict";
import { PANEL_SECTIONS, sectionFromPath, legacyHashToPath } from "./adminNav.ts";

test("kök yol Genel bölümüne düşer", () => {
  assert.equal(sectionFromPath("/yonetim"), "genel");
});

// Next.js bazı yapılandırmalarda sondaki eğik çizgiyi bırakıyor; iki yol da
// aynı bölüm olmalı, yoksa sidebar'da hiçbir şey seçili görünmez.
test("sondaki eğik çizgi bölümü değiştirmez", () => {
  assert.equal(sectionFromPath("/yonetim/"), "genel");
});

test("alt yollar kendi bölümlerine çözülür", () => {
  assert.equal(sectionFromPath("/yonetim/hesaplar"), "hesaplar");
  assert.equal(sectionFromPath("/yonetim/model"), "model");
  assert.equal(sectionFromPath("/yonetim/mcp"), "mcp");
  assert.equal(sectionFromPath("/yonetim/loglar"), "loglar");
});

// Bilinmeyen bir alt yolun boş bir kabuk yerine Genel'i seçmesi, ürünün
// geri kalanındaki davranışla aynı: AppShell de tanımadığı alt görünümü
// varsayılana düşürüyor.
test("bilinmeyen alt yol Genel'e düşer", () => {
  assert.equal(sectionFromPath("/yonetim/olmayan-bir-sey"), "genel");
});

test("her bölümün yolu kendi bölümüne geri çözülür", () => {
  for (const s of PANEL_SECTIONS) {
    assert.equal(sectionFromPath(s.path), s.id);
  }
});

// Eski hash rotaları ölmüyor: gömülü bağlantısı olan kimse 404 görmemeli.
// Bu eşleme AppShell'deki yönlendirmenin tek doğruluk kaynağı.
test("eski admin hash'leri yeni yollara eşlenir", () => {
  assert.equal(legacyHashToPath("#admin"), "/yonetim");
  assert.equal(legacyHashToPath("#admin/overview"), "/yonetim");
  assert.equal(legacyHashToPath("#admin/model"), "/yonetim/model");
  assert.equal(legacyHashToPath("#admin/mcp"), "/yonetim/mcp");
  assert.equal(legacyHashToPath("#admin/logs"), "/yonetim/loglar");
});

test("bilinmeyen admin alt sekmesi panelin köküne gider", () => {
  assert.equal(legacyHashToPath("#admin/kayip-sekme"), "/yonetim");
});

test("prototip admin alt sekmeleri panelin köküne gider", () => {
  assert.equal(legacyHashToPath("#admin/__proto__"), "/yonetim");
  assert.equal(legacyHashToPath("#admin/constructor"), "/yonetim");
  assert.equal(legacyHashToPath("#admin/toString"), "/yonetim");
});

// Diğer master görünümlerin hash'lerine dokunulmuyor: null, "burada işim yok"
// demek ve AppShell'in yönlendirme yapmamasını sağlıyor.
test("panel dışı hash'ler eşlenmez", () => {
  assert.equal(legacyHashToPath("#analiz"), null);
  assert.equal(legacyHashToPath("#gizlilik"), null);
  assert.equal(legacyHashToPath(""), null);
});
