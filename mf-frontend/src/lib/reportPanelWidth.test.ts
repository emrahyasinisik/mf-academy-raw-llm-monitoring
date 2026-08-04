import assert from "node:assert/strict";
import { afterEach, beforeEach, test } from "node:test";
import {
  REPORT_PANEL_WIDTH_KEY,
  clampReportPanelWidth,
  loadReportPanelWidth,
  saveReportPanelWidth,
} from "./reportPanelWidth.ts";

const store = new Map<string, string>();
const stubStorage = {
  getItem: (key: string) => store.get(key) ?? null,
  setItem: (key: string, value: string) => {
    store.set(key, value);
  },
  removeItem: (key: string) => {
    store.delete(key);
  },
};

beforeEach(() => {
  store.clear();
  (globalThis as { localStorage?: typeof stubStorage }).localStorage = stubStorage;
});

afterEach(() => {
  delete (globalThis as { localStorage?: typeof stubStorage }).localStorage;
});

test("clampReportPanelWidth respects min and max", () => {
  assert.equal(clampReportPanelWidth(100, 1200), 280);
  assert.equal(clampReportPanelWidth(900, 1200), 660);
  assert.equal(clampReportPanelWidth(400, 1200), 400);
});

test("loadReportPanelWidth defaults to 38% when unset", () => {
  assert.equal(loadReportPanelWidth(1000), 380);
});

test("save and load round-trip through localStorage", () => {
  saveReportPanelWidth(450);
  assert.equal(store.get(REPORT_PANEL_WIDTH_KEY), "450");
  assert.equal(loadReportPanelWidth(1000), 450);
});

test("loadReportPanelWidth clamps stored value to viewport", () => {
  store.set(REPORT_PANEL_WIDTH_KEY, "9999");
  assert.equal(loadReportPanelWidth(800), 440);
});
