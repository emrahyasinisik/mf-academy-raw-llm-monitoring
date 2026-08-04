export const REPORT_PANEL_WIDTH_KEY = "persona.reportPanelWidth";

const MIN = 280;
const DEFAULT_RATIO = 0.38;
const MAX_RATIO = 0.55;

export function clampReportPanelWidth(px: number, viewportWidth: number): number {
  const max = Math.floor(viewportWidth * MAX_RATIO);
  return Math.max(MIN, Math.min(max, Math.round(px)));
}

function readStoredWidth(): number | null {
  if (typeof globalThis.localStorage === "undefined") return null;
  const raw = globalThis.localStorage.getItem(REPORT_PANEL_WIDTH_KEY);
  if (raw === null) return null;
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) ? n : null;
}

export function loadReportPanelWidth(viewportWidth: number): number {
  const stored = readStoredWidth();
  const px = stored ?? Math.floor(viewportWidth * DEFAULT_RATIO);
  return clampReportPanelWidth(px, viewportWidth);
}

export function saveReportPanelWidth(px: number): void {
  if (typeof globalThis.localStorage === "undefined") return;
  globalThis.localStorage.setItem(REPORT_PANEL_WIDTH_KEY, String(px));
}
