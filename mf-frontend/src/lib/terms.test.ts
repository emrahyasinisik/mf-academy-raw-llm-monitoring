import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { needsTermsGate } from "./terms.ts";

describe("needsTermsGate", () => {
  it("closes when never accepted", () => {
    assert.equal(
      needsTermsGate({ terms_accepted_at: null, terms_version: "" }, "2026-08-01"),
      true,
    );
  });

  it("opens when version matches", () => {
    assert.equal(
      needsTermsGate(
        { terms_accepted_at: "2026-08-01T10:00:00Z", terms_version: "2026-08-01" },
        "2026-08-01",
      ),
      false,
    );
  });

  it("closes when version lags a reconsent publish", () => {
    assert.equal(
      needsTermsGate(
        { terms_accepted_at: "2026-08-01T10:00:00Z", terms_version: "2026-08-01" },
        "2026-08-04",
      ),
      true,
    );
  });

  it("ignores logged-out callers", () => {
    assert.equal(needsTermsGate(null, "2026-08-01"), false);
  });

  it("does not lock everyone when nothing is published yet", () => {
    assert.equal(
      needsTermsGate(
        { terms_accepted_at: "2026-08-01T10:00:00Z", terms_version: "2026-08-01" },
        null,
      ),
      false,
    );
  });
});
