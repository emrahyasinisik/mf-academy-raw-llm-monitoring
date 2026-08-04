import { test } from "node:test";
import assert from "node:assert/strict";
import { orgGate, canAccessOrgPanel } from "./orgAccess.ts";

test("company owner/admin allow", () => {
  assert.equal(orgGate({ loading: false, user: { org_id: "o", org_role: "owner", org_type: "company" } }), "allow");
  assert.equal(orgGate({ loading: false, user: { org_id: "o", org_role: "admin", org_type: "company" } }), "allow");
});

test("member and individual redirect", () => {
  assert.equal(orgGate({ loading: false, user: { org_id: "o", org_role: "member", org_type: "company" } }), "redirect");
  assert.equal(orgGate({ loading: false, user: { org_id: "o", org_role: "owner", org_type: "individual" } }), "redirect");
});

test("loading then login", () => {
  assert.equal(orgGate({ loading: true, user: null }), "booting");
  assert.equal(orgGate({ loading: false, user: null }), "login");
});

test("platform admin alone cannot open sirket", () => {
  assert.equal(canAccessOrgPanel({ role: "admin", org_id: null, org_role: "owner", org_type: "individual" }), false);
});
