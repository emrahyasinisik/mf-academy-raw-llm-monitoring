import { test } from "node:test";
import assert from "node:assert/strict";
import { isRedacted, reportTitle } from "./report.ts";

test("redakte rapor, boş başlıkla 'veri yok' gibi görünmez", () => {
  assert.equal(
    reportTitle({ redacted_at: "2026-08-01T12:00:00Z", subject_title: "" }),
    "İçerik silindi",
  );
});

test("redakte edilmemiş rapor kendi başlığını taşır", () => {
  assert.equal(
    reportTitle({ redacted_at: null, subject_title: "Acme tohum turu" }),
    "Acme tohum turu",
  );
});

// Başlıksız ama silinmemiş rapor üçüncü bir durum: silinmiş demek yanlış olur.
test("başlıksız ve silinmemiş rapor, silinmiş sayılmaz", () => {
  assert.equal(reportTitle({ redacted_at: null, subject_title: "" }), "Başlıksız");
  assert.equal(isRedacted({ redacted_at: null }), false);
});
