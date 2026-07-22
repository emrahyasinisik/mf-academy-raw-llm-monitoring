"use client";

// Master view: Monitoring. Two subviews — Overview aggregates every run into
// fleet-level metrics, Run history is the per-run list with the selected run's
// detail and score.

import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { ListResult, Metrics, Run, RunSummary } from "@/lib/types";
import { ScoreCard } from "../ui/ScoreCard";
import { SubNav } from "../ui/SubNav";

type Sub = "overview" | "runs";

const SUBS = [
  { id: "overview" as const, label: "Overview" },
  { id: "runs" as const, label: "Run history" },
];

const isSub = (s: string): s is Sub => SUBS.some((x) => x.id === s);

export function DashboardView({
  sub,
  onSub,
}: {
  sub: string;
  onSub: (s: string) => void;
}) {
  const active: Sub = isSub(sub) ? sub : "overview";

  const [metrics, setMetrics] = useState<Metrics | null>(null);
  const [runs, setRuns] = useState<RunSummary[]>([]);
  const [selected, setSelected] = useState<Run | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Identifies the most recent detail request so a slow earlier response cannot
  // overwrite the run the user has since clicked on.
  const detailReq = useRef(0);

  // The list only carries summaries, so the detail pane fetches the full run —
  // this is what keeps prompts and responses off every list request.
  const select = useCallback(async (id: string) => {
    const req = ++detailReq.current;
    setDetailLoading(true);
    try {
      const run = await api.getRun(id);
      if (detailReq.current === req) setSelected(run);
    } catch (err) {
      if (detailReq.current === req) {
        setError(
          err instanceof ApiError ? err.message : "Could not load that run.",
        );
      }
    } finally {
      if (detailReq.current === req) setDetailLoading(false);
    }
  }, []);

  // Applying a fetched page is separated from fetching it so the mount effect
  // and the Retry button share one code path with different lifecycles.
  const apply = useCallback(
    (m: Metrics, list: ListResult) => {
      setMetrics(m);
      setRuns(list.runs);
      // Open the newest run on first load so the detail pane isn't empty.
      if (list.runs.length > 0) {
        setSelected((prev) => {
          if (!prev) void select(list.runs[0].id);
          return prev;
        });
      }
    },
    [select],
  );

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [m, list] = await Promise.all([api.metrics(), api.listRuns(50)]);
      apply(m, list);
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : "Could not load the dashboard. Check your connection and retry.",
      );
    } finally {
      setLoading(false);
    }
  }, [apply]);

  // Initial load. Guarded so a response that lands after the user has navigated
  // away cannot write to an unmounted component.
  useEffect(() => {
    let active = true;
    (async () => {
      try {
        const [m, list] = await Promise.all([api.metrics(), api.listRuns(50)]);
        if (active) apply(m, list);
      } catch (err) {
        if (active) {
          setError(
            err instanceof ApiError
              ? err.message
              : "Could not load the dashboard. Check your connection and retry.",
          );
        }
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, [apply]);

  const remove = useCallback(
    async (id: string) => {
      try {
        await api.deleteRun(id);
        if (selected?.id === id) setSelected(null);
        refresh();
      } catch (err) {
        setError(
          err instanceof ApiError ? err.message : "Could not delete that run.",
        );
      }
    },
    [selected, refresh],
  );

  return (
    <div className="max-w-6xl mx-auto p-5 space-y-5">
      <SubNav items={SUBS} active={active} onSelect={onSub} />

      {error && (
        <div
          className="card p-4 flex items-center justify-between gap-3 text-sm"
          style={{
            background: "color-mix(in srgb, var(--bad) 10%, transparent)",
            color: "var(--bad)",
            border: "1px solid color-mix(in srgb, var(--bad) 28%, transparent)",
          }}
        >
          <span>{error}</span>
          <button
            className="btn btn-ghost !py-1 !px-2.5 text-xs"
            onClick={refresh}
          >
            ↻ Retry
          </button>
        </div>
      )}

      {active === "overview" ? (
        <OverviewSub metrics={metrics} onRefresh={refresh} />
      ) : (
        <RunsSub
          runs={runs}
          selected={selected}
          loading={loading}
          detailLoading={detailLoading}
          onSelect={select}
          onRemove={remove}
        />
      )}
    </div>
  );
}

/* ---------------------------------------------------------------- subview 1 */

function OverviewSub({
  metrics,
  onRefresh,
}: {
  metrics: Metrics | null;
  onRefresh: () => void;
}) {
  const models = Object.entries(metrics?.runs_by_model ?? {});

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <Metric label="Total runs" value={metrics?.total_runs ?? "—"} />
        <Metric
          label="Avg score"
          value={metrics ? metrics.avg_score.toFixed(1) : "—"}
          accent
        />
        <Metric
          label="Avg latency"
          value={metrics ? `${Math.round(metrics.avg_latency_ms)}ms` : "—"}
        />
        <Metric
          label="Scored runs"
          value={metrics ? `${metrics.scored_runs}/${metrics.total_runs}` : "—"}
        />
      </div>

      {metrics && Object.keys(metrics.grade_distribution).length > 0 && (
        <div className="card p-4">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold">Grade distribution</h3>
            <button
              className="btn btn-ghost !py-1 !px-2.5 text-xs"
              onClick={onRefresh}
            >
              ↻ Refresh
            </button>
          </div>
          <GradeBars distribution={metrics.grade_distribution} />
        </div>
      )}

      {models.length > 0 && (
        <div className="card p-4">
          <h3 className="text-sm font-semibold mb-3">Runs by model</h3>
          <div className="space-y-2">
            {models
              .sort((a, b) => b[1] - a[1])
              .map(([model, n]) => (
                <div key={model} className="flex items-center gap-3">
                  <span className="text-xs mono flex-1 truncate">{model}</span>
                  <span
                    className="text-xs mono"
                    style={{ color: "var(--text-faint)" }}
                  >
                    {n}
                  </span>
                </div>
              ))}
          </div>
        </div>
      )}

      {metrics && metrics.total_runs === 0 && (
        <p
          className="card p-8 text-center text-sm"
          style={{ color: "var(--text-faint)" }}
        >
          No runs recorded yet. Head to the Playground and run Gemma.
        </p>
      )}
    </div>
  );
}

/* ---------------------------------------------------------------- subview 2 */

function RunsSub({
  runs,
  selected,
  loading,
  detailLoading,
  onSelect,
  onRemove,
}: {
  runs: RunSummary[];
  selected: Run | null;
  loading: boolean;
  detailLoading: boolean;
  onSelect: (id: string) => void;
  onRemove: (id: string) => void;
}) {
  return (
    <div className="grid lg:grid-cols-2 gap-5">
      {/* Run list */}
      <div className="card p-2">
        <div className="px-2 py-2 flex items-center justify-between">
          <h3 className="text-sm font-semibold">Run history</h3>
          <span className="text-xs mono" style={{ color: "var(--text-faint)" }}>
            {runs.length} runs
          </span>
        </div>
        <div className="max-h-[28rem] overflow-y-auto scrollbar-thin space-y-1">
          {loading && runs.length === 0 ? (
            <p
              className="p-3 text-sm animate-pulse-soft"
              style={{ color: "var(--text-dim)" }}
            >
              loading…
            </p>
          ) : runs.length === 0 ? (
            <p className="p-3 text-sm" style={{ color: "var(--text-faint)" }}>
              No runs yet. Head to the Playground and run Gemma.
            </p>
          ) : (
            runs.map((r) => (
              <button
                key={r.id}
                onClick={() => onSelect(r.id)}
                className="w-full text-left px-3 py-2.5 rounded-lg transition-colors"
                style={{
                  background:
                    selected?.id === r.id ? "var(--accent-soft)" : "transparent",
                }}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm truncate flex-1">
                    {r.prompt_preview}
                  </span>
                  {r.score && (
                    <span className={`pill grade-${r.score.grade}`}>
                      {r.score.grade} · {r.score.score.toFixed(0)}
                    </span>
                  )}
                </div>
                <div
                  className="text-xs mono mt-1 flex gap-3"
                  style={{ color: "var(--text-faint)" }}
                >
                  <span>{r.model.split("-").slice(0, 2).join(" ")}</span>
                  <span>{r.latency_ms}ms</span>
                  <span>{new Date(r.created_at).toLocaleTimeString()}</span>
                </div>
              </button>
            ))
          )}
        </div>
      </div>

      {/* Detail */}
      <div className="space-y-4">
        {detailLoading && !selected ? (
          <div
            className="card p-8 text-center text-sm animate-pulse-soft"
            style={{ color: "var(--text-dim)" }}
          >
            loading run…
          </div>
        ) : selected ? (
          <>
            <div className="card p-4">
              <div className="flex items-start justify-between gap-3 mb-3">
                <h3 className="text-sm font-semibold">Run detail</h3>
                <button
                  className="btn btn-ghost !py-1 !px-2.5 text-xs"
                  onClick={() => onRemove(selected.id)}
                >
                  Delete
                </button>
              </div>
              <Field label="Prompt" value={selected.prompt} />
              <Field label="Response" value={selected.response || "(empty)"} />
              {selected.expected_keywords.length > 0 && (
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {selected.expected_keywords.map((k) => (
                    <span
                      key={k}
                      className="pill"
                      style={{ color: "var(--text-dim)" }}
                    >
                      {k}
                    </span>
                  ))}
                </div>
              )}
            </div>
            {selected.score ? (
              <ScoreCard score={selected.score} />
            ) : (
              <div
                className="card p-4 text-sm"
                style={{ color: "var(--text-dim)" }}
              >
                This run has no score yet — clear it from the Decision Scoring
                queue.
              </div>
            )}
          </>
        ) : (
          <div
            className="card p-8 text-center text-sm"
            style={{ color: "var(--text-faint)" }}
          >
            Select a run to inspect its response and score.
          </div>
        )}
      </div>
    </div>
  );
}

function Metric({
  label,
  value,
  accent,
}: {
  label: string;
  value: string | number;
  accent?: boolean;
}) {
  return (
    <div className="card p-4">
      <div
        className="text-2xl font-bold mono"
        style={{ color: accent ? "var(--accent)" : "var(--text)" }}
      >
        {value}
      </div>
      <div className="text-xs mt-1" style={{ color: "var(--text-dim)" }}>
        {label}
      </div>
    </div>
  );
}

function GradeBars({ distribution }: { distribution: Record<string, number> }) {
  const grades = ["A", "B", "C", "D", "F"];
  const max = Math.max(1, ...Object.values(distribution));
  return (
    <div className="flex items-end gap-3 h-24">
      {grades.map((g) => {
        const n = distribution[g] ?? 0;
        return (
          <div key={g} className="flex-1 flex flex-col items-center gap-1">
            <div className="w-full flex-1 flex items-end">
              <div
                className={`w-full rounded-t grade-${g}`}
                style={{
                  height: `${(n / max) * 100}%`,
                  minHeight: n > 0 ? "6px" : "0",
                }}
              />
            </div>
            <span className="text-xs mono" style={{ color: "var(--text-dim)" }}>
              {g}
            </span>
            <span
              className="text-xs mono"
              style={{ color: "var(--text-faint)" }}
            >
              {n}
            </span>
          </div>
        );
      })}
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="mb-2">
      <div className="label">{label}</div>
      <p
        className="text-sm leading-6 whitespace-pre-wrap max-h-40 overflow-y-auto scrollbar-thin"
        style={{ color: "var(--text)" }}
      >
        {value}
      </p>
    </div>
  );
}
