"use client";

// A segmented control: several mutually exclusive options, rendered as one
// recessed track with the active option raised out of it.
//
// It replaces the pattern this app used in three places — a row of buttons where
// the active one was `btn-primary` and the rest were `btn-ghost`. That reads as
// "one important button next to some less important buttons" rather than as a
// single control with a current value, and it made the admin tabs look like four
// competing actions.
//
// Generic over the option id so callers keep their union types instead of
// widening to string at the boundary.

export function Segmented<T extends string>({
  items,
  active,
  onSelect,
  label,
  size = "md",
}: {
  items: readonly { id: T; label: string }[];
  active: T;
  onSelect: (id: T) => void;
  /** Names the group for assistive tech, e.g. "Zaman aralığı". */
  label: string;
  size?: "sm" | "md";
}) {
  const pad = size === "sm" ? "px-2.5 py-1 text-xs" : "px-3.5 py-1.5 text-sm";

  return (
    <div
      role="group"
      aria-label={label}
      className="inline-flex p-1 rounded-[var(--r-sm)] gap-0.5 max-w-full overflow-x-auto scrollbar-thin"
      style={{
        background: "var(--bg-sunk)",
        border: "1px solid var(--line)",
        boxShadow: "inset 0 1px 2px rgba(0,0,0,.4)",
      }}
    >
      {items.map((it) => {
        const on = it.id === active;
        return (
          <button
            key={it.id}
            onClick={() => onSelect(it.id)}
            aria-pressed={on}
            className={`${pad} rounded-[var(--r-xs)] font-semibold whitespace-nowrap`}
            style={{
              background: on ? "var(--panel-3)" : "transparent",
              color: on ? "var(--text)" : "var(--text-dim)",
              boxShadow: on ? "var(--bevel), var(--shadow-1)" : undefined,
              transition:
                "background var(--dur-2) var(--ease), color var(--dur-2) var(--ease)",
            }}
          >
            {it.label}
          </button>
        );
      })}
    </div>
  );
}
