import { test } from "node:test";
import assert from "node:assert/strict";
import { needsPasswordGate, isPasswordChangeRequired } from "./passwordGate.ts";

test("password_change_required kodu kapıyı açar", () => {
  assert.equal(
    isPasswordChangeRequired({ status: 403, code: "password_change_required" }),
    true,
  );
});

test("must_change_password bayraklı kullanıcı kapıya düşer", () => {
  assert.equal(needsPasswordGate({ must_change_password: true }), true);
  assert.equal(needsPasswordGate({ must_change_password: false }), false);
  assert.equal(needsPasswordGate(null), false);
});
