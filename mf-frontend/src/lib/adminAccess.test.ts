import { test } from "node:test";
import assert from "node:assert/strict";
import { panelGate } from "./adminAccess.ts";

// Oturum henüz çözülmemişken karar verilemez. "Kullanıcı yok" ile "kullanıcı
// daha yüklenmedi" karıştırılırsa, sayfayı yenileyen yönetici bir anlığına
// giriş ekranını görür ve sonra panele atlar.
test("oturum çözülmeden karar verilmez", () => {
  assert.equal(panelGate({ loading: true, user: null }), "booting");
  assert.equal(panelGate({ loading: true, user: { role: "admin" } }), "booting");
});

test("oturum yoksa panelin kendi giriş ekranı", () => {
  assert.equal(panelGate({ loading: false, user: null }), "login");
});

// Yönetici olmayan biri panelin varlığını öğrenmiş olabilir; kapı burada
// kapanmaz, backend'de kapanır. Bu yalnızca çalışmayacak bir ekranı
// göstermemek için.
test("yönetici olmayan ürüne yönlendirilir", () => {
  assert.equal(panelGate({ loading: false, user: { role: "user" } }), "redirect");
});

test("yönetici panele girer", () => {
  assert.equal(panelGate({ loading: false, user: { role: "admin" } }), "allow");
});
