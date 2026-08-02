// Signature visual: a row of ledger hash-marks that breathe like a rubric
// being scored. Pure atmosphere on auth; filled ticks on report headers.
// CSS-driven so it costs nothing when reduced-motion collapses the animation.

type Props = {
  /** How many ticks to draw. Defaults to 8 — a typical rubric width. */
  count?: number;
  /** 0–1 fraction lit from the left. Omit for ambient wave mode. */
  filled?: number;
  className?: string;
  /** Ambient wave (auth) vs static filled bar (reports). */
  mode?: "wave" | "fill";
};

export function CriterionContinuum({
  count = 8,
  filled,
  className = "",
  mode = "wave",
}: Props) {
  const ticks = Array.from({ length: count }, (_, i) => i);
  const onCount =
    mode === "fill" && filled !== undefined
      ? Math.round(Math.min(1, Math.max(0, filled)) * count)
      : 0;

  if (mode === "fill") {
    return (
      <div className={`score-track ${className}`} aria-hidden>
        {ticks.map((i) => (
          <span
            key={i}
            className="score-tick"
            data-on={i < onCount ? "1" : "0"}
            style={{ ["--i" as string]: i }}
          />
        ))}
      </div>
    );
  }

  return (
    <div
      className={`flex items-end gap-1.5 h-10 ${className}`}
      aria-hidden
    >
      {ticks.map((i) => {
        const h = 28 + ((i * 17) % 28);
        return (
          <span
            key={i}
            className="continuum-tick"
            style={{
              height: `${h}%`,
              minHeight: 8,
              ["--i" as string]: i,
              opacity: 0.35 + (i % 3) * 0.15,
            }}
          />
        );
      })}
    </div>
  );
}
