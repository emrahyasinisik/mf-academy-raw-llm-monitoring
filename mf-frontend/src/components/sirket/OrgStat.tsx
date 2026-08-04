"use client";

/** A single figure for the company panel — local copy of the admin Stat so
 *  /sirket never imports from /yonetim. */
export function OrgStat({
  label,
  value,
  hint,
  tone,
  index = 0,
}: {
  label: string;
  value: string;
  hint?: string;
  tone?: string;
  index?: number;
}) {
  return (
    <div className="card item-in p-4" style={{ ["--i" as string]: index }}>
      <div className="eyebrow">{label}</div>
      <div
        className="font-display text-3xl font-semibold num mt-1.5 tracking-tight"
        style={{ color: tone ?? "var(--text)" }}
      >
        {value}
      </div>
      {hint && (
        <div
          className="text-xs mt-2 leading-relaxed"
          style={{ color: "var(--text-dim)" }}
        >
          {hint}
        </div>
      )}
    </div>
  );
}
