import { test } from "node:test";
import assert from "node:assert/strict";
import { seatFull } from "./orgTeam.ts";

test("seatFull when count reaches limit", () => {
  assert.equal(seatFull(5, 5), true);
  assert.equal(seatFull(6, 5), true);
});

test("seatFull false while seats remain", () => {
  assert.equal(seatFull(0, 5), false);
  assert.equal(seatFull(4, 5), false);
});

test("seatFull with zero limit is full", () => {
  assert.equal(seatFull(0, 0), true);
});
