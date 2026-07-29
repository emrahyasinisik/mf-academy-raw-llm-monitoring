"use client";

// The history rail, shared by both products.
//
// One component rather than one per screen, because the two histories differ
// only in what a row *means* — a persona thread carries a verdict, a codegen run
// carries a latency — and not in how a history behaves. Selecting, renaming,
// deleting, paging and the empty state are the same interaction in both places,
// and two copies of them would drift on the first fix.
//
// So the caller supplies rows already reduced to what this renders: a title, an
// optional badge, a time. It does not know about conversations or runs, and it
// never fetches — the screen that owns the data owns the loading state too,
// because only that screen knows whether a refresh should interrupt what the
// user is looking at.

import { useCallback, useEffect, useRef, useState } from "react";

/** A badge on a row: the one fact that distinguishes this entry from its neighbours. */
export type HistoryBadge = {
  text: string;
  /** A CSS colour, usually one of --ok / --warn / --bad. Omitted for a neutral pill. */
  tone?: string;
};

export type HistoryItem = {
  id: string;
  title: string;
  badge?: HistoryBadge | null;
  /** ISO timestamp. Rendered relative, because "2 saat önce" is what a list is scanned for. */
  timestamp: string;
  /** Secondary line, e.g. "6 mesaj" or a model id. */
  detail?: string;
};

type Props = {
  items: HistoryItem[];
  activeId: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
  /** Omitted when the entries are not renameable — codegen runs are not. */
  onRename?: (id: string, title: string) => void;
  onDelete?: (id: string) => void;
  onLoadMore?: () => void;
  hasMore?: boolean;
  loading?: boolean;
  error?: string;
  newLabel: string;
  emptyText: string;
};

export function HistoryPanel({
  items,
  activeId,
  onSelect,
  onNew,
  onRename,
  onDelete,
  onLoadMore,
  hasMore = false,
  loading = false,
  error = "",
  newLabel,
  emptyText,
}: Props) {
  const [editing, setEditing] = useState<string | null>(null);

  return (
    <aside
      className="hidden lg:flex flex-col w-[264px] shrink-0 border-r"
      style={{ borderColor: "var(--line)", background: "var(--panel)" }}
      aria-label="Geçmiş"
    >
      <div
        className="px-3 py-2.5 flex items-center justify-between gap-2 border-b shrink-0"
        style={{ borderColor: "var(--line)" }}
      >
        <span className="eyebrow">Geçmiş</span>
        <button className="btn btn-quiet btn-sm" onClick={onNew}>
          {newLabel}
        </button>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-thin p-2 space-y-1">
        {error && <div className="notice notice-bad text-xs">{error}</div>}

        {/* Skeletons only on the first load. A refresh that replaced the list
            with placeholders would make every poll look like a page change. */}
        {loading && items.length === 0 && (
          <>
            <div className="skeleton h-14 rounded-[var(--r-sm)]" />
            <div className="skeleton h-14 rounded-[var(--r-sm)]" />
            <div className="skeleton h-14 rounded-[var(--r-sm)]" />
          </>
        )}

        {!loading && items.length === 0 && !error && (
          <p
            className="text-xs leading-relaxed px-2 py-6 text-center"
            style={{ color: "var(--text-faint)" }}
          >
            {emptyText}
          </p>
        )}

        {items.map((item) => (
          <Row
            key={item.id}
            item={item}
            active={item.id === activeId}
            editing={editing === item.id}
            onSelect={() => onSelect(item.id)}
            onStartRename={onRename ? () => setEditing(item.id) : undefined}
            onRename={(title) => {
              setEditing(null);
              if (title && title !== item.title) onRename?.(item.id, title);
            }}
            onCancelRename={() => setEditing(null)}
            onDelete={onDelete ? () => onDelete(item.id) : undefined}
          />
        ))}

        {hasMore && (
          <button
            className="btn btn-quiet btn-sm w-full mt-1"
            onClick={onLoadMore}
            disabled={loading}
          >
            {loading ? "Yükleniyor…" : "Daha eski"}
          </button>
        )}
      </div>
    </aside>
  );
}

function Row({
  item,
  active,
  editing,
  onSelect,
  onStartRename,
  onRename,
  onCancelRename,
  onDelete,
}: {
  item: HistoryItem;
  active: boolean;
  editing: boolean;
  onSelect: () => void;
  onStartRename?: () => void;
  onRename: (title: string) => void;
  onCancelRename: () => void;
  onDelete?: () => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  // Two-step delete, in place. A confirm() dialog would be the cheaper build,
  // but these rows are the only copy of research that cannot be reproduced —
  // the persona searches live, so yesterday's evidence is gone — and a native
  // dialog is exactly the thing people dismiss without reading.
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    if (editing) inputRef.current?.select();
  }, [editing]);

  // Leaving the row cancels a pending delete, so an armed button never waits
  // somewhere the user has stopped looking.
  const disarm = useCallback(() => setConfirming(false), []);

  if (editing) {
    return (
      <div className="card p-1.5" style={{ background: "var(--panel-2)" }}>
        <input
          ref={inputRef}
          className="input text-xs"
          defaultValue={item.title}
          aria-label="Başlık"
          onKeyDown={(e) => {
            if (e.key === "Enter") onRename(e.currentTarget.value.trim());
            if (e.key === "Escape") onCancelRename();
          }}
          onBlur={(e) => onRename(e.currentTarget.value.trim())}
        />
      </div>
    );
  }

  return (
    <div
      className="group relative rounded-[var(--r-sm)] transition-colors"
      style={{
        background: active ? "var(--brand-wash)" : undefined,
        border: `1px solid ${active ? "var(--brand-line)" : "transparent"}`,
      }}
      onMouseLeave={disarm}
    >
      <button
        onClick={onSelect}
        className="w-full text-left px-2.5 py-2 min-w-0"
        aria-current={active ? "true" : undefined}
      >
        <div className="flex items-start gap-2 min-w-0">
          <span
            className="text-xs leading-snug flex-1 min-w-0 line-clamp-2"
            style={{ color: active ? "var(--text)" : "var(--text-dim)" }}
          >
            {item.title}
          </span>
          {item.badge && (
            <span
              className="pill shrink-0 mt-px"
              style={
                item.badge.tone
                  ? { color: item.badge.tone, borderColor: item.badge.tone }
                  : undefined
              }
            >
              {item.badge.text}
            </span>
          )}
        </div>
        <div
          className="flex items-center gap-1.5 mt-1 text-[11px] mono"
          style={{ color: "var(--text-faint)" }}
        >
          <span>{relativeTime(item.timestamp)}</span>
          {item.detail && (
            <>
              <span aria-hidden>·</span>
              <span className="truncate">{item.detail}</span>
            </>
          )}
        </div>
      </button>

      {/* Revealed on hover and on keyboard focus. focus-within is what keeps
          these reachable by tab — hover alone would hide them from anyone not
          using a mouse. */}
      {(onStartRename || onDelete) && (
        <div
          className="absolute top-1 right-1 flex gap-0.5 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 transition-opacity"
          style={{ background: "var(--panel-2)", borderRadius: "var(--r-xs)" }}
        >
          {onStartRename && !confirming && (
            <IconButton label="Yeniden adlandır" onClick={onStartRename}>
              <path d="M11.5 2.5a1.4 1.4 0 0 1 2 2L6 12l-2.5.5L4 10z" />
            </IconButton>
          )}
          {onDelete &&
            (confirming ? (
              <button
                className="btn btn-danger btn-sm px-1.5 py-0.5 text-[11px]"
                onClick={() => {
                  setConfirming(false);
                  onDelete();
                }}
              >
                Sil?
              </button>
            ) : (
              <IconButton label="Sil" onClick={() => setConfirming(true)}>
                <path d="M3.5 4.5h9M6.5 4.5V3h3v1.5M5 4.5l.5 8h5l.5-8" />
              </IconButton>
            ))}
        </div>
      )}
    </div>
  );
}

function IconButton({
  label,
  onClick,
  children,
}: {
  label: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      title={label}
      aria-label={label}
      className="p-1 rounded-[var(--r-xs)] hover:opacity-100 opacity-70"
      style={{ color: "var(--text-dim)" }}
    >
      <svg
        width="14"
        height="14"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth={1.4}
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden
      >
        {children}
      </svg>
    </button>
  );
}

/**
 * Relative time in Turkish, to the coarsest unit that is still true.
 *
 * Coarse on purpose: the list is scanned, not read. "3 gün önce" answers the
 * question a history list is asked ("is this the one from Monday?") and an exact
 * timestamp does not — it has to be decoded first. The absolute time stays
 * available in the row's tooltip via the caller's data.
 */
function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";

  const secs = Math.max(0, (Date.now() - then) / 1000);
  if (secs < 60) return "az önce";

  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins} dk önce`;

  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours} saat önce`;

  const days = Math.floor(hours / 24);
  if (days < 30) return `${days} gün önce`;

  // Past a month, the month name carries more than a day count does.
  return new Date(then).toLocaleDateString("tr-TR", {
    day: "numeric",
    month: "short",
  });
}
