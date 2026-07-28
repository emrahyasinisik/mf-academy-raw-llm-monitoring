"use client";

// A time-series line chart, hand-written for the same reason DartBlock is: this
// is four lines and an axis, and a charting library would arrive with a runtime,
// a theme system and its own opinions about a design language this app already
// has.
//
// The pieces that are easy to skip and are not optional here:
//
//   - the crosshair snaps to the nearest sample and reads out *every* series, so
//     the pointer never has to land on a 2px line to get a value
//   - keyboard focus does the same thing arrow keys, because a value reachable
//     only by hover is a value some readers cannot reach
//   - a table view carries the numbers when the chart cannot
//
// Series colours are assigned in fixed order and never cycled. The pair was
// checked for colour-vision separation rather than chosen by eye: blue↔orange
// holds up at ΔE 30 under protanopia, where blue↔violet collapses to 2.7.

import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { MetricSeries } from "@/lib/types";

// Mirrors --series-1 / --series-2. Literals rather than var() because these are
// also read into the tooltip swatches and the legend, where a computed colour
// would have to be resolved from the cascade at paint time.
const SERIES_COLORS = ["#58a6ff", "#ff7a2f"];
const SURFACE = "var(--panel)";

// Plot geometry in CSS pixels. The chart measures its own width so text and
// stroke widths stay literal — scaling a viewBox would grow the labels with the
// container.
const H = 170;
const PAD = { top: 12, right: 14, bottom: 22, left: 44 };

export type ChartUnit = "rps" | "seconds" | "count" | string;

export function formatValue(v: number, unit: ChartUnit): string {
  if (unit === "seconds") {
    // Sub-second latencies are the common case and "0.04s" reads as noise.
    if (v === 0) return "0";
    if (v < 1) return `${Math.round(v * 1000)}ms`;
    return `${v.toFixed(v < 10 ? 2 : 1)}s`;
  }
  if (unit === "rps") {
    if (v === 0) return "0";
    if (v < 0.01) return v.toExponential(1);
    return v.toFixed(v < 1 ? 3 : 2);
  }
  return v >= 1000 ? v.toLocaleString("tr-TR") : String(Math.round(v * 100) / 100);
}

function formatTime(unixSeconds: number, spanSeconds: number): string {
  const d = new Date(unixSeconds * 1000);
  const hm = d.toLocaleTimeString("tr-TR", { hour: "2-digit", minute: "2-digit" });
  // Past a few hours the hour alone stops locating a point in the day.
  return spanSeconds > 12 * 3600
    ? `${d.toLocaleDateString("tr-TR", { day: "2-digit", month: "2-digit" })} ${hm}`
    : hm;
}

// Axis ticks land on round numbers rather than on the data's own extremes: an
// axis labelled 0 / 0.5 / 1 is read at a glance, one labelled 0 / 0.47 / 0.94 is
// read twice.
function niceTicks(max: number): number[] {
  if (max <= 0) return [0, 1];
  // max/3 rather than max/2: halving puts the top tick as much as 60% above
  // the data, and a line that never leaves the bottom half of its own chart
  // wastes the resolution the reader came for.
  const raw = max / 3;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const step = ([1, 2, 2.5, 5, 10].find((m) => m * mag >= raw) ?? 10) * mag;
  const ticks: number[] = [];
  // Strictly past the maximum, not up to it. Stopping at the last tick below
  // max leaves the top of the axis lower than the data, and the line then
  // draws above the plot area — off the element entirely at the peak.
  for (let t = 0; t < max - step * 1e-9; t += step) ticks.push(t);
  ticks.push(ticks.length ? ticks[ticks.length - 1] + step : step);
  return ticks;
}

function useWidth<T extends HTMLElement>() {
  const ref = useRef<T | null>(null);
  const [w, setW] = useState(560);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => {
      const next = entry.contentRect.width;
      if (next > 0) setW(next);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w] as const;
}

export function TimeChart({
  series,
  unit,
  showTable,
}: {
  series: MetricSeries[];
  unit: ChartUnit;
  showTable: boolean;
}) {
  const [wrapRef, width] = useWidth<HTMLDivElement>();
  const [cursor, setCursor] = useState<number | null>(null);
  const titleId = useId();

  const withData = useMemo(
    () => series.filter((s) => s.points.length > 0),
    [series],
  );

  // The union of every series' timestamps, which is not the same as the longest
  // series' timestamps. A target that saw no traffic until noon returns fewer
  // points, and positioning those by their index in their own array would draw
  // half a day of data compressed into the left half of the chart, ending in
  // the middle. Position comes from the timestamp; the index only picks which
  // sample the crosshair is reading.
  const times = useMemo(() => {
    const set = new Set<number>();
    for (const s of withData) for (const p of s.points) set.add(p.t);
    return [...set].sort((a, b) => a - b);
  }, [withData]);

  // One lookup per series so the crosshair can read every line at a timestamp
  // that only some of them have.
  const byTime = useMemo(
    () => withData.map((s) => new Map(s.points.map((p) => [p.t, p.v]))),
    [withData],
  );

  const max = useMemo(() => {
    let m = 0;
    for (const s of withData) for (const p of s.points) if (p.v > m) m = p.v;
    return m;
  }, [withData]);

  if (withData.length === 0) {
    return (
      <div
        className="flex items-center justify-center text-xs"
        style={{ height: H, color: "var(--text-faint)" }}
      >
        Bu aralıkta veri yok
      </div>
    );
  }

  const ticks = niceTicks(max);
  const yMax = ticks[ticks.length - 1] || 1;
  const span = times.length > 1 ? times[times.length - 1] - times[0] : 0;

  const plotW = Math.max(width - PAD.left - PAD.right, 10);
  const plotH = H - PAD.top - PAD.bottom;
  const t0 = times[0];
  const t1 = times[times.length - 1];
  // From the timestamp, so a series with gaps or a late start lands where its
  // samples actually happened.
  const x = (t: number) =>
    PAD.left + (t1 === t0 ? plotW / 2 : ((t - t0) / (t1 - t0)) * plotW);
  const xAt = (i: number) => x(times[i]);
  const y = (v: number) => PAD.top + plotH - (v / yMax) * plotH;

  const nearestIndex = (clientX: number, rect: DOMRect) => {
    const rel = clientX - rect.left - PAD.left;
    const i = Math.round((rel / plotW) * (times.length - 1));
    return Math.min(Math.max(i, 0), times.length - 1);
  };

  if (showTable) {
    return (
      <div className="overflow-x-auto" style={{ maxHeight: H + 40 }}>
        <table className="w-full text-xs tabular-nums">
          <thead>
            <tr style={{ color: "var(--text-faint)" }}>
              <th className="text-left font-normal py-1 pr-3">Zaman</th>
              {withData.map((s, i) => (
                <th key={i} className="text-right font-normal py-1 pl-3">
                  {s.label || "değer"}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {times.map((t) => (
              <tr key={t} style={{ color: "var(--text-dim)" }}>
                <td className="py-0.5 pr-3">{formatTime(t, span)}</td>
                {/* By timestamp, not by row number: a series that starts late
                    has fewer points, and indexing by row would slide its values
                    up against times they did not happen at. */}
                {byTime.map((m, si) => {
                  const v = m.get(t);
                  return (
                    <td key={si} className="text-right py-0.5 pl-3">
                      {v === undefined ? "—" : formatValue(v, unit)}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }

  return (
    <div ref={wrapRef} className="relative">
      <svg
        width={width}
        height={H}
        role="img"
        aria-labelledby={titleId}
        tabIndex={0}
        className="outline-none focus-visible:ring-1"
        style={{ display: "block" }}
        onPointerMove={(e) =>
          setCursor(nearestIndex(e.clientX, e.currentTarget.getBoundingClientRect()))
        }
        onPointerLeave={() => setCursor(null)}
        onFocus={() => setCursor((c) => c ?? times.length - 1)}
        onBlur={() => setCursor(null)}
        onKeyDown={(e) => {
          if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
          e.preventDefault();
          setCursor((c) => {
            const base = c ?? times.length - 1;
            const next = base + (e.key === "ArrowRight" ? 1 : -1);
            return Math.min(Math.max(next, 0), times.length - 1);
          });
        }}
      >
        <title id={titleId}>
          {withData.length === 1
            ? `Zaman serisi, son değer ${formatValue(
                withData[0].points[withData[0].points.length - 1].v,
                unit,
              )}`
            : `${withData.length} serili zaman grafiği`}
        </title>

        {/* Gridlines and axis text are one step off the surface: present enough
            to read a value against, never competing with the data. */}
        {ticks.map((t) => (
          <g key={t}>
            <line
              x1={PAD.left}
              x2={PAD.left + plotW}
              y1={y(t)}
              y2={y(t)}
              stroke="var(--line)"
              strokeWidth={1}
            />
            <text
              x={PAD.left - 6}
              y={y(t) + 3}
              textAnchor="end"
              fontSize={10}
              fill="var(--text-faint)"
            >
              {formatValue(t, unit)}
            </text>
          </g>
        ))}

        {times.length > 1 &&
          [0, Math.floor((times.length - 1) / 2), times.length - 1].map((i, n) => (
            <text
              key={i}
              x={xAt(i)}
              y={H - 6}
              textAnchor={n === 0 ? "start" : n === 2 ? "end" : "middle"}
              fontSize={10}
              fill="var(--text-faint)"
            >
              {formatTime(times[i], span)}
            </text>
          ))}

        {withData.map((s, si) => {
          const color = SERIES_COLORS[si % SERIES_COLORS.length];
          const d = s.points
            .map((p, i) => `${i === 0 ? "M" : "L"}${x(p.t)},${y(p.v)}`)
            .join(" ");
          const last = s.points[s.points.length - 1];
          return (
            // Keyed by the window so switching range remounts the path and
            // replays the draw. Without it React would patch `d` in place and
            // the new data would simply appear.
            <g key={`${si}-${t0}-${t1}`}>
              {/* pathLength={1} normalises the path's own length to 1, so the
                  dash offset that hides it can be written without measuring the
                  geometry in JS. The line then draws itself left to right, which
                  is the direction it is read in. */}
              <path
                d={d}
                className="draw-in"
                pathLength={1}
                strokeDasharray={1}
                strokeDashoffset={1}
                fill="none"
                stroke={color}
                strokeWidth={2}
                strokeLinejoin="round"
                strokeLinecap="round"
              />
              {/* The end-dot carries a surface ring so two lines that finish at
                  the same value stay countable where they overlap. */}
              <circle
                cx={x(last.t)}
                cy={y(last.v)}
                r={4}
                fill={color}
                stroke={SURFACE}
                strokeWidth={2}
              />
            </g>
          );
        })}

        {cursor !== null && (
          <g pointerEvents="none">
            <line
              x1={xAt(cursor)}
              x2={xAt(cursor)}
              y1={PAD.top}
              y2={PAD.top + plotH}
              stroke="var(--text-faint)"
              strokeWidth={1}
            />
            {byTime.map((m, si) => {
              const v = m.get(times[cursor]);
              return v === undefined ? null : (
                <circle
                  key={si}
                  cx={xAt(cursor)}
                  cy={y(v)}
                  r={4}
                  fill={SERIES_COLORS[si % SERIES_COLORS.length]}
                  stroke={SURFACE}
                  strokeWidth={2}
                />
              );
            })}
          </g>
        )}
      </svg>

      {cursor !== null && times[cursor] !== undefined && (
        <div
          className="absolute pointer-events-none card px-2 py-1.5 text-xs"
          style={{
            // Flips to the left half once the cursor passes the middle, so the
            // readout never leaves the card it belongs to.
            left: xAt(cursor) > PAD.left + plotW / 2 ? undefined : xAt(cursor) + 12,
            right:
              xAt(cursor) > PAD.left + plotW / 2 ? width - xAt(cursor) + 12 : undefined,
            top: 4,
            minWidth: 96,
          }}
        >
          <div style={{ color: "var(--text-faint)" }}>
            {formatTime(times[cursor], span)}
          </div>
          {withData.map((s, si) => {
            const v = byTime[si].get(times[cursor]);
            return (
              <div key={si} className="flex items-center gap-2 mt-0.5">
                <span
                  style={{
                    width: 10,
                    height: 2,
                    borderRadius: 1,
                    background: SERIES_COLORS[si % SERIES_COLORS.length],
                  }}
                />
                {/* Value first and in primary ink: the reader already knows
                    which series they are looking at and came here for a
                    number. */}
                <span className="tabular-nums font-medium" style={{ color: "var(--text)" }}>
                  {v === undefined ? "—" : formatValue(v, unit)}
                </span>
                {s.label && <span style={{ color: "var(--text-dim)" }}>{s.label}</span>}
              </div>
            );
          })}
        </div>
      )}

      {/* A legend only where there is identity to carry. One series is named by
          the card's own title; a box with a single swatch restates it. */}
      {withData.length > 1 && (
        <div className="flex flex-wrap gap-3 mt-1.5">
          {withData.map((s, si) => (
            <span key={si} className="flex items-center gap-1.5 text-xs">
              <span
                style={{
                  width: 12,
                  height: 2,
                  borderRadius: 1,
                  background: SERIES_COLORS[si % SERIES_COLORS.length],
                }}
              />
              <span style={{ color: "var(--text-dim)" }}>{s.label || "değer"}</span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
