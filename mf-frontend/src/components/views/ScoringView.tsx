"use client";

// Master view: Decision Scoring. Monitoring answers "what did the model do?";
// this view answers "how good was it?". It owns the three things the scorer is
// for — clearing the unscored backlog, reading the rubric across every scored
// run, and comparing two runs dimension by dimension.

import { useCallback, useEffect, useMemo, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { Breakdown, Run, RunSummary } from "@/lib/types";
import { ScoreCard } from "../ui/ScoreCard";
import { SubNav } from "../ui/SubNav";

type Sub = "queue" | "rubric" | "compare";

const SUBS = [
  { id: "queue" as const, label: "Scoring queue" },
  { id: "rubric" as const, label: "Rubric analysis" },
  { id: "compare" as const, label: "Compare runs" },
];

const isSub = (s: string): s is Sub => SUBS.some((x) => x.id === s);

// Scoring one run is independent of scoring the next, so a small worker pool
// drains a backlog far faster than a sequential loop — while still bounding the
// concurrent load we put on the API.
const SCORE_CONCURRENCY = 3;

const DIMENSIONS: readonly (keyof Breakdown)[] = [
  "completion",
  "latency",
  "efficiency",
  "keywords",
  "length",
];

const DIM_LABELS: Record<keyof Breakdown, string> = {
  completion: "Completion",
  latency: "Latency",
  efficiency: "Efficiency",
  keywords: "Keywords",
  length: "Length",
};

export function ScoringView({
  sub,
  onSub,
}: {
  sub: string;
  onSub: (s: string) => void;
}) {
  const active: Sub = isSub(sub) ? sub : "queue";

  const [runs, setRuns] = useState<RunSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [batch, setBatch] = useState<{ done: number; total: number } | null>(
    null,
  );

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const list = await api.listRuns(100);
      setRuns(list.runs);
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : "Could not load runs. Check your connection and retry.",
      );
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load. Guarded so a response that lands after the user has navigated
  // away cannot write to an unmounted component.
  useEffect(() => {
    let active = true;
    (async () => {
      try {
        const list = await api.listRuns(100);
        if (active) setRuns(list.runs);
      } catch (err) {
        if (active) {
          setError(
            err instanceof ApiError
              ? err.message
              : "Could not load runs. Check your connection and retry.",
          );
        }
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  const unscored = useMemo(() => runs.filter((r) => !r.score), [runs]);

  async function scoreOne(id: string) {
    try {
      await api.scoreRun(id);
      await refresh();
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : "Could not score that run.",
      );
    }
  }

  // Drains the whole backlog through a bounded pool. Individual failures are
  // swallowed on purpose: one bad run must not abort the batch, and anything
  // that failed simply stays in the queue after the refresh.
  async function scoreAll() {
    const pending = unscored.map((r) => r.id);
    if (pending.length === 0) return;

    setError(null);
    setBatch({ done: 0, total: pending.length });

    let next = 0;
    let done = 0;
    const worker = async () => {
      for (;;) {
        const i = next++;
        if (i >= pending.length) return;
        try {
          await api.scoreRun(pending[i]);
        } catch {
          /* leave it in the queue; the refresh below will show it again */
        }
        done++;
        setBatch({ done, total: pending.length });
      }
    };

    await Promise.all(
      Array.from({ length: Math.min(SCORE_CONCURRENCY, pending.length) }, worker),
    );
    setBatch(null);
    await refresh();
  }

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

      {active === "queue" && (
        <QueueSub
          unscored={unscored}
          total={runs.length}
          loading={loading}
          batch={batch}
          onScoreOne={scoreOne}
          onScoreAll={scoreAll}
          onRefresh={refresh}
        />
      )}
      {active === "rubric" && <RubricSub runs={runs} loading={loading} />}
      {active === "compare" && <CompareSub runs={runs} onError={setError} />}
    </div>
  );
}

/* ---------------------------------------------------------------- subview 1 */

function QueueSub({
  unscored,
  total,
  loading,
  batch,
  onScoreOne,
  onScoreAll,
  onRefresh,
}: {
  unscored: RunSummary[];
  total: number;
  loading: boolean;
  batch: { done: number; total: number } | null;
  onScoreOne: (id: string) => void;
  onScoreAll: () => void;
  onRefresh: () => void;
}) {
  return (
    <div className="card p-4">
      <div className="flex items-center justify-between gap-3 mb-4">
        <div>
          <h3 className="text-sm font-semibold">Unscored runs</h3>
          <p className="text-xs mt-0.5" style={{ color: "var(--text-faint)" }}>
            {unscored.length} of {total} runs are waiting on the scorer
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            className="btn btn-ghost !py-1 !px-2.5 text-xs"
            onClick={onRefresh}
          >
            ↻ Refresh
          </button>
          <button
            className="btn btn-primary !py-1.5 !px-3 text-xs"
            onClick={onScoreAll}
            disabled={!!batch || unscored.length === 0}
          >
            {batch
              ? `Scoring ${batch.done}/${batch.total}…`
              : `Score all (${unscored.length})`}
          </button>
        </div>
      </div>

      {batch && (
        <div
          className="h-2 rounded-full overflow-hidden mb-4"
          style={{ background: "var(--bg-elev-2)" }}
        >
          <div
            className="h-full transition-all"
            style={{
              width: `${(batch.done / batch.total) * 100}%`,
              background: "var(--accent)",
            }}
          />
        </div>
      )}

      {loading && unscored.length === 0 ? (
        <p
          className="text-sm animate-pulse-soft"
          style={{ color: "var(--text-dim)" }}
        >
          loading…
        </p>
      ) : unscored.length === 0 ? (
        <p className="text-sm" style={{ color: "var(--text-faint)" }}>
          Queue is empty — every run has a decision score.
        </p>
      ) : (
        <div className="max-h-[30rem] overflow-y-auto scrollbar-thin space-y-1">
          {unscored.map((r) => (
            <div
              key={r.id}
              className="flex items-center gap-3 px-3 py-2.5 rounded-lg"
              style={{ background: "var(--bg-elev-2)" }}
            >
              <div className="flex-1 min-w-0">
                <p className="text-sm truncate">{r.prompt_preview}</p>
                <div
                  className="text-xs mono mt-1 flex gap-3"
                  style={{ color: "var(--text-faint)" }}
                >
                  <span>{r.model.split("-").slice(0, 2).join(" ")}</span>
                  <span>{r.latency_ms}ms</span>
                  <span>{new Date(r.created_at).toLocaleTimeString()}</span>
                </div>
              </div>
              <button
                className="btn btn-ghost !py-1 !px-2.5 text-xs"
                onClick={() => onScoreOne(r.id)}
                disabled={!!batch}
              >
                Score
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/* ---------------------------------------------------------------- subview 2 */

function RubricSub({
  runs,
  loading,
}: {
  runs: RunSummary[];
  loading: boolean;
}) {
  // Aggregating client-side keeps this honest: it is the same per-dimension
  // data the Go scorer returned per run, just averaged for the cohort.
  const rubric = useMemo(() => {
    const scores = runs.flatMap((r) => (r.score ? [r.score] : []));
    if (scores.length === 0) return null;

    const avg = {} as Record<keyof Breakdown, number>;
    for (const dim of DIMENSIONS) {
      let sum = 0;
      for (const s of scores) sum += s.breakdown[dim];
      avg[dim] = sum / scores.length;
    }
    const weakest = DIMENSIONS.reduce((a, b) => (avg[a] <= avg[b] ? a : b));
    const overall =
      scores.reduce((acc, s) => acc + s.score, 0) / scores.length;

    return { avg, weakest, overall, n: scores.length };
  }, [runs]);

  if (loading && !rubric) {
    return (
      <p
        className="card p-8 text-center text-sm animate-pulse-soft"
        style={{ color: "var(--text-dim)" }}
      >
        loading…
      </p>
    );
  }

  if (!rubric) {
    return (
      <p
        className="card p-8 text-center text-sm"
        style={{ color: "var(--text-faint)" }}
      >
        Nothing scored yet. Clear the scoring queue first.
      </p>
    );
  }

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
        <div className="card p-4">
          <div
            className="text-2xl font-bold mono"
            style={{ color: "var(--accent)" }}
          >
            {rubric.overall.toFixed(1)}
          </div>
          <div className="text-xs mt-1" style={{ color: "var(--text-dim)" }}>
            Mean decision score
          </div>
        </div>
        <div className="card p-4">
          <div className="text-2xl font-bold mono">{rubric.n}</div>
          <div className="text-xs mt-1" style={{ color: "var(--text-dim)" }}>
            Runs in cohort
          </div>
        </div>
        <div className="card p-4">
          <div
            className="text-2xl font-bold mono"
            style={{ color: "var(--warn)" }}
          >
            {DIM_LABELS[rubric.weakest]}
          </div>
          <div className="text-xs mt-1" style={{ color: "var(--text-dim)" }}>
            Weakest dimension
          </div>
        </div>
      </div>

      <div className="card p-4">
        <h3 className="text-sm font-semibold mb-4">
          Per-dimension average across {rubric.n} scored runs
        </h3>
        <div className="space-y-3">
          {DIMENSIONS.map((dim) => {
            const v = rubric.avg[dim];
            return (
              <div key={dim}>
                <div className="flex justify-between text-xs mb-1">
                  <span
                    style={{
                      color:
                        dim === rubric.weakest
                          ? "var(--warn)"
                          : "var(--text-dim)",
                    }}
                  >
                    {DIM_LABELS[dim]}
                    {dim === rubric.weakest && " · weakest"}
                  </span>
                  <span className="mono">{v.toFixed(1)}</span>
                </div>
                <div
                  className="h-2 rounded-full overflow-hidden"
                  style={{ background: "var(--bg-elev-2)" }}
                >
                  <div
                    className="h-full rounded-full"
                    style={{ width: `${v}%`, background: barColor(v) }}
                  />
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

/* ---------------------------------------------------------------- subview 3 */

function CompareSub({
  runs,
  onError,
}: {
  runs: RunSummary[];
  onError: (msg: string) => void;
}) {
  const scored = useMemo(() => runs.filter((r) => r.score), [runs]);
  const [left, setLeft] = useState<Run | null>(null);
  const [right, setRight] = useState<Run | null>(null);

  const pick = useCallback(
    async (id: string, side: "left" | "right") => {
      const set = side === "left" ? setLeft : setRight;
      if (!id) {
        set(null);
        return;
      }
      try {
        set(await api.getRun(id));
      } catch (err) {
        onError(
          err instanceof ApiError ? err.message : "Could not load that run.",
        );
      }
    },
    [onError],
  );

  if (scored.length < 2) {
    return (
      <p
        className="card p-8 text-center text-sm"
        style={{ color: "var(--text-faint)" }}
      >
        Compare needs at least two scored runs. Clear the scoring queue first.
      </p>
    );
  }

  return (
    <div className="grid md:grid-cols-2 gap-5">
      {(["left", "right"] as const).map((side) => {
        const run = side === "left" ? left : right;
        return (
          <div key={side} className="space-y-4">
            <select
              className="input"
              value={run?.id ?? ""}
              onChange={(e) => pick(e.target.value, side)}
            >
              <option value="">Select a run…</option>
              {scored.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.score ? `[${r.score.grade}] ` : ""}
                  {r.prompt_preview.slice(0, 48)}
                </option>
              ))}
            </select>

            {run ? (
              <>
                <div className="card p-4">
                  <div className="label">Prompt</div>
                  <p className="text-sm leading-6 whitespace-pre-wrap max-h-28 overflow-y-auto scrollbar-thin">
                    {run.prompt}
                  </p>
                  <div className="label mt-3">Response</div>
                  <p className="text-sm leading-6 whitespace-pre-wrap max-h-40 overflow-y-auto scrollbar-thin">
                    {run.response || "(empty)"}
                  </p>
                  <div
                    className="text-xs mono mt-3 flex gap-3"
                    style={{ color: "var(--text-faint)" }}
                  >
                    <span>{run.latency_ms}ms</span>
                    <span>{run.completion_tokens} tok</span>
                    <span>temp {run.temperature}</span>
                  </div>
                </div>
                {run.score && <ScoreCard score={run.score} />}
              </>
            ) : (
              <div
                className="card p-8 text-center text-sm"
                style={{ color: "var(--text-faint)" }}
              >
                No run selected.
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function barColor(v: number): string {
  if (v >= 80) return "var(--good)";
  if (v >= 60) return "var(--warn)";
  return "var(--bad)";
}
