"use client";

// The status rail: one strip, under the header, on every screen.
//
// This is the app's only ambient instrument, and it exists because the product
// is a front end for a machine that is genuinely sometimes off. It answers three
// questions without being asked — is the host reachable, what is it serving, and
// is it working right now — and it is the reason the views below it no longer
// need to explain the hardware in prose.
//
// The elapsed counter in particular replaces an apology. A generation on this
// host can take a minute, and the old interface handled that by warning the user
// in a sentence. A number that is visibly moving says the same thing, says it
// truthfully for whatever the run actually costs, and keeps saying it after the
// reader has stopped believing spinners.

import { useEffect, useState } from "react";
import { useMachine, type HostState } from "@/store/machine";

const HOST_COPY: Record<HostState, { label: string; tone: string }> = {
  checking: { label: "Bağlanıyor", tone: "var(--text-faint)" },
  online: { label: "Çevrimiçi", tone: "var(--ok)" },
  offline: { label: "Çevrimdışı", tone: "var(--bad)" },
};

/**
 * Counts up from a start time. Ticks at 100ms because the readout shows tenths —
 * a slower interval would visibly skip values, and a faster one would repaint
 * for nothing. Returns null when there is nothing to time, so the caller can
 * render the resting state instead of "0.0 s".
 *
 * The clock is only ever advanced from the interval callback, never from the
 * effect body. Seeding it synchronously on mount would be the obvious way to
 * avoid a blank first frame, and it is exactly the cascading-render pattern
 * react-hooks/set-state-in-effect exists to catch — so the seed is a literal
 * zero at the point of use instead, which is what the counter would read during
 * that first frame anyway.
 */
function useElapsed(startedAt: number | null): number | null {
  // The reading carries the job it belongs to. State outlives the run that
  // produced it, so a bare number would let the previous job's final time paint
  // for one frame at the start of the next one — a new generation opening on
  // "12.4 s" and counting up from there.
  const [clock, setClock] = useState<{ job: number; seconds: number } | null>(null);

  useEffect(() => {
    if (startedAt === null) return;
    const id = setInterval(
      () => setClock({ job: startedAt, seconds: (Date.now() - startedAt) / 1000 }),
      100,
    );
    return () => clearInterval(id);
  }, [startedAt]);

  if (startedAt === null) return null;
  return clock?.job === startedAt ? clock.seconds : 0;
}

export function StatusRail() {
  const { host, model, activity, startedAt, lastRun } = useMachine();
  const elapsed = useElapsed(startedAt);
  const working = activity !== null;
  const state = HOST_COPY[host];

  return (
    <div
      className="flex items-center gap-3 px-4 sm:px-5 h-9 text-xs overflow-hidden"
      style={{
        background: "color-mix(in srgb, var(--panel-2) 92%, var(--brand-wash))",
        borderBottom: "1px solid var(--line)",
        boxShadow: "var(--bevel)",
      }}
    >
      {/* The lamp and the word it stands for. aria-live is on this region only:
          the elapsed counter beside it changes ten times a second and would
          turn a screen reader into a metronome. */}
      <span
        className="flex items-center gap-2 shrink-0"
        role="status"
        aria-live="polite"
        style={{ color: working ? "var(--brand)" : state.tone }}
      >
        <span className={`lamp ${working || host === "online" ? "lamp-live" : ""}`} />
        <span className="mono uppercase tracking-wider font-medium">
          {working ? activity : state.label}
        </span>
      </span>

      {model && (
        <>
          <Divider />
          <span
            className="mono truncate min-w-0 hidden sm:block"
            style={{ color: "var(--text-faint)" }}
            title={model}
          >
            {model}
          </span>
        </>
      )}

      {/* The working track. Present only while a job is in flight, so the rail
          is quiet at rest — an always-animating strip stops carrying meaning
          within about a minute of looking at it. */}
      <span className="flex-1 min-w-0 flex justify-end sm:justify-center px-1">
        {working && (
          <span
            className="scan relative block w-full max-w-[220px] h-[3px] rounded-full overflow-hidden"
            style={{ background: "var(--bg-sunk)" }}
            aria-hidden
          />
        )}
      </span>

      {/* Right-hand readout: the live clock while working, the cost of the last
          run when idle. One slot, never both, so the rail keeps a fixed shape. */}
      <span className="shrink-0 flex items-center gap-3">
        {working && elapsed !== null ? (
          <span className="mono num" style={{ color: "var(--brand)" }} aria-hidden>
            {elapsed.toFixed(1)} s
          </span>
        ) : lastRun ? (
          <span
            className="mono num flex items-center gap-2.5"
            style={{ color: "var(--text-faint)" }}
            title={`Son çalıştırma · ${lastRun.model}`}
          >
            <span className="hidden sm:inline">son çalıştırma</span>
            <span style={{ color: "var(--text-dim)" }}>
              {lastRun.seconds.toFixed(1)} s
            </span>
            <span className="hidden sm:inline">{lastRun.tokens} tok</span>
            <span className="hidden md:inline">
              {lastRun.tokensPerSecond.toFixed(1)} tok/s
            </span>
          </span>
        ) : null}
      </span>
    </div>
  );
}

function Divider() {
  return (
    <span
      className="h-3 w-px shrink-0 hidden sm:block"
      style={{ background: "var(--line-strong)" }}
      aria-hidden
    />
  );
}
