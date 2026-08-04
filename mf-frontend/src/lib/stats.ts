import type {
  CategoryCount,
  MetricSeries,
  StatsDay,
  StatsFunnel,
  StatsTargetSeries,
} from "./types";

// Kova, oran ve kohort olgunluğu bileşenden ayrı duruyor: panelin en kolay
// yalan söyleyen aritmetiği node --test ile tarayıcı açmadan sınanabiliyor.
export function changeLabel(pct: number | null): string {
  if (pct === null) return "—";
  const value = Math.abs(pct).toLocaleString("tr-TR", {
    maximumFractionDigits: 1,
  });
  if (pct > 0) return `+%${value}`;
  if (pct < 0) return `-%${value}`;
  return `%${value}`;
}

export function changeTone(pct: number | null): string {
  if (pct === null) return "var(--text-faint)";
  if (pct > 0) return "var(--ok)";
  if (pct < 0) return "var(--bad)";
  return "var(--text-dim)";
}

export function daySeries(
  days: StatsDay[],
  key: "new_users" | "cumulative_users" | "assessments",
  label: string,
): MetricSeries {
  return {
    label,
    points: days.map((day) => ({ t: day.t, v: day[key] })),
  };
}

export function validitySeries(days: StatsDay[]): MetricSeries {
  return {
    label: "Şema geçerliliği",
    points: days
      .filter((day) => day.assessments !== 0)
      .map((day) => ({ t: day.t, v: day.schema_valid / day.assessments })),
  };
}

export function targetSeries(list: StatsTargetSeries[]): MetricSeries[] {
  return [...list]
    .sort((a, b) => totalVolume(b) - totalVolume(a))
    .slice(0, 2)
    .map((series) => ({
      label: targetLabel(series.target),
      points: series.points,
    }));
}

export function funnelRates(f: StatsFunnel): { consented: number; analyzed: number } {
  if (f.registered === 0) {
    return { consented: 0, analyzed: 0 };
  }
  return {
    consented: f.consented / f.registered,
    analyzed: f.analyzed / f.registered,
  };
}

export function cohortRate(
  count: number,
  size: number,
  matureWeeks: number,
  needWeeks: number,
): number | null {
  if (matureWeeks < needWeeks) return null;
  if (size === 0) return 0;
  return count / size;
}

export function shareRows(
  list: CategoryCount[],
): { key: string; label: string; count: number; share: number }[] {
  const total = list.reduce((sum, row) => sum + row.count, 0);
  if (total === 0) return [];
  return list.map((row) => ({
    key: row.key,
    label: orgTypeLabel(row.key),
    count: row.count,
    share: row.count / total,
  }));
}

function totalVolume(series: StatsTargetSeries): number {
  return series.points.reduce((sum, point) => sum + point.v, 0);
}

function targetLabel(target: string): string {
  if (target === "browser") return "Tarayıcı";
  if (target === "server") return "Sunucu";
  return target;
}

function orgTypeLabel(key: string): string {
  if (key === "individual") return "Bireysel";
  if (key === "company") return "Şirket";
  return key;
}
