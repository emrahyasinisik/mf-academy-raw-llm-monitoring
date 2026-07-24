"use client";

// The report. This component is where the product's central claim either holds
// or quietly breaks, so two rules are enforced structurally rather than left to
// whoever writes the next screen:
//
//   1. The score never renders without its coverage. They are one component,
//      not two adjacent ones, so there is no arrangement of this UI in which
//      the number appears alone.
//
//   2. A criterion with no evidence is drawn as "değerlendirilemedi" — never as
//      a zero, an empty bar, or a dash that reads like a bad rating. "The deck
//      does not mention the team" and "the team is weak" are different findings
//      and a reader must not have to guess which one they are looking at.

import { useState } from "react";
import type { Assessment, Criterion, Finding } from "@/lib/types";

function scoreColor(score: number | null): string {
  if (score === null) return "var(--text-faint)";
  if (score >= 70) return "var(--good)";
  if (score >= 45) return "var(--warn)";
  return "var(--bad)";
}

/**
 * Coverage is deliberately not colour-graded on the same scale as the score.
 * Low coverage is not "bad" — it is a statement about the case, and colouring
 * it red would read as a verdict on the subject rather than on the evidence.
 */
function coverageTone(coverage: number): { color: string; label: string } {
  if (coverage >= 0.85) return { color: "var(--good)", label: "rubriğin tamamına yakını" };
  if (coverage >= 0.6) return { color: "var(--warn)", label: "rubriğin çoğu" };
  return { color: "var(--bad)", label: "rubriğin bir bölümü" };
}

export function ReportHeadline({ report }: { report: Assessment }) {
  const cov = coverageTone(report.coverage);
  const pct = Math.round(report.coverage * 100);
  const unassessed = report.findings.filter((f) => !f.evidence_found).length;

  return (
    <div className="card p-5">
      <div className="flex flex-wrap items-start gap-6">
        <div>
          <div className="text-xs uppercase tracking-wide" style={{ color: "var(--text-faint)" }}>
            Puan
          </div>
          <div
            className="text-4xl font-semibold tabular-nums leading-tight"
            style={{ color: scoreColor(report.overall_score) }}
          >
            {report.overall_score === null ? "—" : report.overall_score.toFixed(1)}
          </div>
          {report.overall_score === null && (
            <div className="text-xs mt-1" style={{ color: "var(--bad)" }}>
              Puanlanamadı — bu sıfır değildir
            </div>
          )}
        </div>

        {/* Rendered in the same block as the score, not beside it: the two are
            one reading and separating them is how a screenshot of the number
            alone ends up in somebody's email. */}
        <div className="flex-1 min-w-[220px]">
          <div className="text-xs uppercase tracking-wide" style={{ color: "var(--text-faint)" }}>
            Kapsam
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-2xl font-semibold tabular-nums" style={{ color: cov.color }}>
              %{pct}
            </span>
            <span className="text-xs" style={{ color: "var(--text-dim)" }}>
              {cov.label} kanıtlandı
            </span>
          </div>
          <div
            className="mt-2 h-1.5 rounded-full overflow-hidden"
            style={{ background: "var(--bg-elev-2)" }}
            role="img"
            aria-label={`Kapsam yüzde ${pct}`}
          >
            <div className="h-full rounded-full" style={{ width: `${pct}%`, background: cov.color }} />
          </div>
        </div>
      </div>

      {unassessed > 0 && report.overall_score !== null && (
        <p className="text-xs mt-4 leading-relaxed" style={{ color: "var(--text-dim)" }}>
          <strong style={{ color: "var(--text)" }}>{unassessed} kriter</strong> vaka metninde
          hiç geçmiyor ve puana <strong style={{ color: "var(--text)" }}>dahil edilmedi</strong>.
          Bu puan, rubriğin geri kalanı hakkındadır — eksik kriterler kötü puan almadı,
          değerlendirilemedi.
        </p>
      )}

      {!report.schema_valid && (
        <p
          className="text-xs mt-3 p-2.5 rounded"
          style={{ background: "var(--accent-soft)", color: "var(--text-dim)" }}
        >
          Model çıktısı şemaya birebir uymadı ve onarıldı
          {report.repair_attempts > 0 && ` (${report.repair_attempts} adım)`}. Rapor
          kullanılabilir ama bir karara dayanak yapmadan önce kanıtları oku.
        </p>
      )}
    </div>
  );
}

function FindingRow({ criterion, finding }: { criterion: Criterion; finding?: Finding }) {
  const [open, setOpen] = useState(false);
  const assessed = !!finding?.evidence_found && finding.score !== null;
  const max = criterion.scale_max || 5;
  const pct = assessed ? ((finding!.score as number) / max) * 100 : 0;

  return (
    <div style={{ borderTop: "1px solid var(--border)" }}>
      <button
        onClick={() => setOpen((v) => !v)}
        className="w-full text-left px-4 py-3 flex items-center gap-3 hover:opacity-90"
        aria-expanded={open}
      >
        <span className="flex-1 min-w-0">
          <span className="text-sm block truncate">{criterion.label}</span>
          <span className="text-xs" style={{ color: "var(--text-faint)" }}>
            ağırlık {(criterion.weight * 100).toFixed(0)}%
          </span>
        </span>

        {assessed ? (
          <>
            <span className="w-24 h-1.5 rounded-full hidden sm:block" style={{ background: "var(--bg-elev-2)" }}>
              <span
                className="block h-full rounded-full"
                style={{ width: `${pct}%`, background: scoreColor(pct) }}
              />
            </span>
            <span className="text-sm tabular-nums w-12 text-right" style={{ color: scoreColor(pct) }}>
              {finding!.score}/{max}
            </span>
          </>
        ) : (
          // Deliberately not a 0, not an empty bar, not a dash. Those all read
          // as a rating; this reads as an absence, which is what it is.
          <span
            className="pill text-xs"
            style={{ color: "var(--text-faint)", borderColor: "var(--border)" }}
          >
            değerlendirilemedi
          </span>
        )}
        <span className="text-xs" style={{ color: "var(--text-faint)" }}>
          {open ? "▲" : "▼"}
        </span>
      </button>

      {open && (
        <div className="px-4 pb-4 space-y-3">
          <p className="text-xs leading-relaxed" style={{ color: "var(--text-dim)" }}>
            {criterion.description}
          </p>

          {finding?.rationale && (
            <p className="text-sm leading-relaxed">{finding.rationale}</p>
          )}

          {finding?.evidence?.length ? (
            <div className="space-y-1.5">
              <div className="text-xs uppercase tracking-wide" style={{ color: "var(--text-faint)" }}>
                Vakadan alıntı
              </div>
              {/* Verbatim quotes, presented as quotes. The whole defensibility
                  argument rests on a reader being able to check a claim against
                  the source, so these are never paraphrased or truncated. */}
              {finding.evidence.map((q, i) => (
                <blockquote
                  key={i}
                  className="text-xs leading-relaxed pl-3 py-1"
                  style={{ borderLeft: "2px solid var(--accent)", color: "var(--text-dim)" }}
                >
                  {q}
                </blockquote>
              ))}
            </div>
          ) : assessed ? (
            <p className="text-xs" style={{ color: "var(--warn)" }}>
              Bu kritere puan verilmiş ama alıntı gösterilmemiş — doğrulanamaz.
            </p>
          ) : (
            <p className="text-xs" style={{ color: "var(--text-faint)" }}>
              Vaka metninde bu kriteri değerlendirecek bir ifade bulunamadı.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

export function ReportCard({ report }: { report: Assessment }) {
  const findings = new Map(report.findings.map((f) => [f.key, f]));

  return (
    <div className="space-y-4">
      <ReportHeadline report={report} />

      <div className="card overflow-hidden">
        <div className="px-4 py-3 flex items-center justify-between gap-3">
          <h3 className="text-sm font-semibold">Kriterler</h3>
          <span className="text-xs" style={{ color: "var(--text-faint)" }}>
            {report.domain_name ?? report.domain_slug} · v{report.domain_version}
          </span>
        </div>

        {/* Iterated over the snapshot, not over the findings. The snapshot is
            the rubric this report was scored against, so a criterion the model
            failed to return still appears — as unassessed — instead of silently
            vanishing from a report somebody is about to act on. */}
        {report.criteria_snapshot.map((c) => (
          <FindingRow key={c.key} criterion={c} finding={findings.get(c.key)} />
        ))}
      </div>

      <div
        className="text-xs flex flex-wrap gap-x-4 gap-y-1"
        style={{ color: "var(--text-faint)" }}
      >
        <span>model: {report.model}</span>
        <span>gecikme: {(report.latency_ms / 1000).toFixed(1)}s</span>
        <span>
          token: {report.prompt_tokens} → {report.completion_tokens}
        </span>
        <span>{new Date(report.created_at).toLocaleString("tr-TR")}</span>
      </div>
    </div>
  );
}
