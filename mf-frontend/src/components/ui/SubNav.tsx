"use client";

// Tab strip for a master view's subviews. Deliberately styled as an underline
// rather than the header's filled pills, so the two navigation levels stay
// visually distinguishable.

export function SubNav<T extends string>({
  items,
  active,
  onSelect,
}: {
  items: readonly { id: T; label: string }[];
  active: T;
  onSelect: (id: T) => void;
}) {
  return (
    <div
      className="flex items-center gap-1 border-b overflow-x-auto scrollbar-thin"
      style={{ borderColor: "var(--border)" }}
    >
      {items.map((it) => {
        const on = it.id === active;
        return (
          <button
            key={it.id}
            onClick={() => onSelect(it.id)}
            className="px-3 py-2 text-sm font-medium whitespace-nowrap -mb-px border-b-2 transition-colors"
            style={{
              borderColor: on ? "var(--accent)" : "transparent",
              color: on ? "var(--text)" : "var(--text-dim)",
            }}
          >
            {it.label}
          </button>
        );
      })}
    </div>
  );
}
