"use client";

import { useCallback, type ReactElement } from "react";
import { Rapor } from "@/components/ui/Rapor";
import { breakdown } from "@/lib/rubric";
import { clampReportPanelWidth } from "@/lib/reportPanelWidth";
import type { Assessment } from "@/lib/types";

export type ReportPanelProps = {
  open: boolean;
  width: number;
  onWidthChange: (px: number) => void;
  onClose: () => void;
  assessment: Assessment | null;
  loading: boolean;
  error: string;
  onRetry?: () => void;
};

export function ReportPanel({
  open,
  width,
  onWidthChange,
  onClose,
  assessment,
  loading,
  error,
  onRetry,
}: ReportPanelProps): ReactElement | null {
  const onResizeStart = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      e.preventDefault();
      document.body.style.userSelect = "none";

      const onMove = (ev: PointerEvent) => {
        const vw = window.innerWidth;
        onWidthChange(clampReportPanelWidth(vw - ev.clientX, vw));
      };

      const onEnd = () => {
        document.body.style.userSelect = "";
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onEnd);
        window.removeEventListener("pointercancel", onEnd);
        window.removeEventListener("lostpointercapture", onEnd);
      };

      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onEnd);
      window.addEventListener("pointercancel", onEnd);
      window.addEventListener("lostpointercapture", onEnd);
    },
    [onWidthChange],
  );

  if (!open) return null;

  const overall =
    assessment && !loading
      ? breakdown(assessment.criteria_snapshot, assessment.findings).overall
      : null;

  return (
    <aside
      className="flex flex-col shrink-0 border-l relative max-lg:fixed max-lg:inset-0 max-lg:z-40 w-full lg:[width:var(--report-w)] lg:static"
      style={{
        ["--report-w" as string]: `${width}px`,
        borderColor: "var(--line)",
        background: "var(--panel)",
      }}
      aria-label="Rapor paneli"
    >
      <div
        className="absolute left-0 top-0 bottom-0 w-1 cursor-col-resize max-lg:hidden touch-none"
        aria-orientation="vertical"
        role="separator"
        aria-label="Panel genişliği"
        onPointerDown={onResizeStart}
      />

      <header
        className="flex items-center justify-between gap-3 px-4 py-3 border-b shrink-0"
        style={{ borderColor: "var(--line)" }}
      >
        <div className="flex items-baseline gap-3 min-w-0">
          <span className="eyebrow shrink-0">Rapor</span>
          {overall !== null && (
            <span className="mono text-sm num" style={{ color: "var(--text-dim)" }}>
              {overall.toFixed(1)}
            </span>
          )}
        </div>
        <button type="button" className="btn btn-ghost btn-sm shrink-0" onClick={onClose}>
          Kapat
        </button>
      </header>

      <div className="flex-1 overflow-y-auto p-4">
        {loading && (
          <p className="mono text-xs" style={{ color: "var(--text-faint)" }}>
            Rapor üretiliyor…
          </p>
        )}

        {!loading && error && !assessment && (
          <div className="space-y-3">
            <div className="notice notice-bad" role="alert">
              {error}
            </div>
            {onRetry && (
              <button type="button" className="btn btn-primary btn-sm" onClick={onRetry}>
                Yeniden dene
              </button>
            )}
          </div>
        )}

        {!loading && assessment && (
          <>
            {error && (
              <div className="notice notice-warn mb-3" role="status">
                {error}
              </div>
            )}
            <Rapor assessment={assessment} className="mt-0 border-0 shadow-none" />
          </>
        )}
      </div>
    </aside>
  );
}
