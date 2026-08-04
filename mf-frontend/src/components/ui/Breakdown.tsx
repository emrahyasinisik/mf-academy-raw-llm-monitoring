type ShareRow = {
  key: string;
  label: string;
  count: number;
  share: number;
};

type FunnelStage = {
  label: string;
  count: number;
  rate: number;
};

type CohortDisplayRow = {
  label: string;
  size: number;
  week2: number | null;
  week4: number | null;
};

const EMPTY = "Bu aralıkta veri yok";

export function ShareBar({ rows }: { rows: ShareRow[] }) {
  if (rows.length === 0) return <EmptyState />;

  return (
    <div className="space-y-3">
      <div
        className="flex h-3 overflow-hidden rounded-full"
        style={{ background: "var(--line)" }}
        aria-hidden="true"
      >
        {rows.map((row, i) => (
          <div
            key={row.key}
            style={{
              width: `${clampPercent(row.share)}%`,
              background: segmentColor(i),
            }}
          />
        ))}
      </div>
      <ul className="grid gap-2 text-xs sm:grid-cols-2">
        {rows.map((row, i) => (
          <li key={row.key} className="flex items-center justify-between gap-3">
            <span className="flex items-center gap-2 min-w-0">
              <span
                className="h-2.5 w-2.5 shrink-0 rounded-full"
                style={{ background: segmentColor(i) }}
                aria-hidden="true"
              />
              <span className="truncate" style={{ color: "var(--text-dim)" }}>
                {row.label}
              </span>
            </span>
            <span className="tabular-nums" style={{ color: "var(--text)" }}>
              {formatCount(row.count)} · {formatPercent(row.share)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function FunnelBars({ stages }: { stages: FunnelStage[] }) {
  if (stages.length === 0 || stages.every((stage) => stage.count === 0)) {
    return <EmptyState />;
  }

  return (
    <div className="space-y-3">
      {stages.map((stage, i) => (
        <div key={stage.label} className="space-y-1.5">
          <div className="flex items-baseline justify-between gap-3 text-xs">
            <span style={{ color: "var(--text-dim)" }}>{stage.label}</span>
            <span className="tabular-nums" style={{ color: "var(--text)" }}>
              {formatCount(stage.count)} · {formatPercent(stage.rate)}
            </span>
          </div>
          <div
            className="h-2.5 overflow-hidden rounded-full"
            style={{ background: "var(--line)" }}
          >
            <div
              className="h-full rounded-full"
              style={{
                width: `${clampPercent(stage.rate)}%`,
                background: segmentColor(i),
              }}
            />
          </div>
        </div>
      ))}
    </div>
  );
}

export function CohortGrid({ rows }: { rows: CohortDisplayRow[] }) {
  if (rows.length === 0) return <EmptyState />;

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs tabular-nums">
        <thead>
          <tr style={{ color: "var(--text-faint)" }}>
            <th scope="col" className="py-1 pr-3 text-left font-normal">
              Kohort haftası
            </th>
            <th scope="col" className="py-1 px-3 text-right font-normal">
              Üye
            </th>
            <th scope="col" className="py-1 px-3 text-right font-normal">
              2. hafta
            </th>
            <th scope="col" className="py-1 pl-3 text-right font-normal">
              4. hafta
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.label} style={{ color: "var(--text-dim)" }}>
              <th scope="row" className="py-1.5 pr-3 text-left font-normal">
                {row.label}
              </th>
              <td className="py-1.5 px-3 text-right">{formatCount(row.size)}</td>
              <td className="py-1.5 px-3 text-right">{formatCohortRate(row.week2)}</td>
              <td className="py-1.5 pl-3 text-right">{formatCohortRate(row.week4)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EmptyState() {
  return (
    <div
      className="flex min-h-[140px] items-center justify-center text-xs"
      style={{ color: "var(--text-faint)" }}
    >
      {EMPTY}
    </div>
  );
}

function formatCohortRate(value: number | null) {
  if (value === null) {
    return (
      <span title="Kohort henüz bu yaşta değil" aria-label="Kohort henüz bu yaşta değil">
        —
      </span>
    );
  }
  return formatPercent(value);
}

function formatPercent(value: number): string {
  return `%${Math.round(value * 100)}`;
}

function formatCount(value: number): string {
  return value.toLocaleString("tr-TR");
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.min(Math.max(value * 100, 0), 100);
}

function segmentColor(index: number): string {
  if (index === 0) return "var(--series-1)";
  if (index === 1) return "var(--series-2)";
  return "var(--text-dim)";
}
