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
