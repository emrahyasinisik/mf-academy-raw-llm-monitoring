"use client";

import type { ReactElement } from "react";
import { isRedacted } from "@/lib/report";
import { breakdown } from "@/lib/rubric";
import { CriterionContinuum } from "@/components/ui/CriterionContinuum";
import type { Assessment } from "@/lib/types";

/** Bir oranı yüzdeye çevirir. */
export const pct = (x: number) => `%${Math.round(x * 100)}`;

/**
 * Tamamlanmış bir raporun görünümü.
 *
 * İki kural pazarlık konusu değil ve ikisi de types.ts'te yazılı:
 * `score: null` sıfır değildir, ve `overall_score` kapsamsız gösterilmez.
 * Birincisi sessizliği başarısızlık diye puanlar, ikincisi 0.9 kapsamdaki 68
 * ile 0.3 kapsamdaki 68'i aynı şey gibi gösterir.
 */
export function Rapor({
  assessment,
  className = "",
}: {
  assessment: Assessment;
  className?: string;
}): ReactElement {
  if (isRedacted(assessment)) {
    const panelMode = className.includes("border-0");
    return (
      <article
        className={`${panelMode ? "p-0" : "card mt-6 p-5"} view-in ${className}`.trim()}
      >
        <h2 className="eyebrow">Rapor</h2>
        <p className="mt-2 text-sm">
          Bu raporun içeriği silindi. Puan ve kapsam kaydı duruyor, vaka metni ve
          kanıt alıntıları kaldırıldı.
        </p>
      </article>
    );
  }

  const b = breakdown(assessment.criteria_snapshot, assessment.findings);
  const scorePct = b.overall === null ? 0 : Math.min(100, b.overall);
  const coveragePct = Math.round(b.coverage * 100);
  const panelMode = className.includes("border-0");

  return (
    <article
      className={`mt-6 p-5 sm:p-6 rapor view-in ${className}`.trim()}
      style={
        panelMode
          ? undefined
          : {
              background: "var(--panel)",
              border: "1px solid var(--line)",
              borderRadius: "var(--r-lg)",
              boxShadow: "var(--bevel), var(--shadow-2)",
            }
      }
    >
      <header
        className="flex flex-wrap items-start justify-between gap-5 pb-5"
        style={{ borderBottom: "1px solid var(--line)" }}
      >
        <div className="min-w-0 flex-1">
          <p className="eyebrow mb-2">Rapor</p>
          <h2 className="font-display text-xl sm:text-2xl font-bold tracking-tight truncate">
            {assessment.subject_title}
          </h2>
          <p className="mono text-xs mt-1.5" style={{ color: "var(--text-faint)" }}>
            {assessment.domain_name} · v{assessment.domain_version} ·{" "}
            {new Date(assessment.created_at).toLocaleString("tr-TR")}
          </p>
          <CriterionContinuum
            className="mt-4 max-w-xs"
            count={Math.min(12, Math.max(6, b.rows.length))}
            mode="fill"
            filled={b.coverage}
          />
        </div>

        <div className="flex items-start gap-6 shrink-0">
          <div className="min-w-[4.5rem]">
            <div className="eyebrow">Toplam</div>
            <div className="num font-display text-3xl mt-1 tracking-tight">
              {b.overall === null ? "—" : b.overall.toFixed(1)}
            </div>
            <div className="score-fill mt-2" style={{ ["--p" as string]: scorePct }}>
              <span />
            </div>
          </div>
          <div className="min-w-[4.5rem]">
            <div className="eyebrow">Kapsam</div>
            <div className="num font-display text-3xl mt-1 tracking-tight">
              {pct(b.coverage)}
            </div>
            <div className="score-fill mt-2" style={{ ["--p" as string]: coveragePct }}>
              <span />
            </div>
          </div>
        </div>
      </header>

      {b.overall === null && (
        <div className="notice notice-warn mt-4">
          Bu vakada değerlendirilebilecek hiçbir kriter bulunamadı. Bu, sıfır puan
          değildir — metin rubriğin sorduğu şeylere dair kanıt taşımıyor.
        </div>
      )}

      <div className="mt-5 space-y-2">
        {b.rows.map((row, i) => (
          <details
            key={row.criterion.key}
            className="well p-3.5 item-in"
            style={{ ["--i" as string]: Math.min(i, 10) }}
            open
          >
            <summary className="flex flex-wrap items-center gap-x-3 gap-y-1.5 cursor-pointer list-none [&::-webkit-details-marker]:hidden">
              <span className="font-medium text-sm flex-1 min-w-0">
                {row.criterion.label}
              </span>

              <span className="mono text-xs num" style={{ color: "var(--text-faint)" }}>
                ağırlık {row.criterion.weight.toFixed(2)}
              </span>

              {row.scored && row.points !== null ? (
                <>
                  <span className="pill pill-brand num">
                    {row.clamped} /{" "}
                    {row.criterion.scale_max > 0 ? row.criterion.scale_max : 5}
                  </span>
                  <span className="mono text-xs num" style={{ color: "var(--text-dim)" }}>
                    +{row.points.toFixed(1)}
                  </span>
                </>
              ) : (
                <span className="pill pill-warn">kanıt yok</span>
              )}
            </summary>

            <div className="mt-3 space-y-2">
              {row.criterion.description && (
                <p className="text-xs" style={{ color: "var(--text-faint)" }}>
                  {row.criterion.description}
                </p>
              )}

              {row.finding?.rationale && (
                <p className="text-sm" style={{ color: "var(--text-dim)" }}>
                  {row.finding.rationale}
                </p>
              )}

              {row.finding && row.finding.evidence.length > 0 ? (
                <ul className="space-y-1.5">
                  {row.finding.evidence.map((q, qi) => (
                    <li
                      key={qi}
                      className="text-sm pl-3"
                      style={{
                        borderLeft: "2px solid var(--brand-line)",
                        color: "var(--text)",
                      }}
                    >
                      &ldquo;{q}&rdquo;
                    </li>
                  ))}
                </ul>
              ) : (
                !row.scored && (
                  <p className="text-xs" style={{ color: "var(--text-faint)" }}>
                    Metinde bu kritere dair bir ifade bulunamadı. Kanıtsız kriterler
                    toplamdan düşer ve kapsam sayısında raporlanır — düşük puan
                    olarak sayılmaz.
                  </p>
                )
              )}
            </div>
          </details>
        ))}
      </div>

      <footer
        className="mt-5 pt-3.5 flex flex-wrap gap-x-4 gap-y-1 mono text-xs"
        style={{ borderTop: "1px solid var(--line)", color: "var(--text-faint)" }}
      >
        <span>model {assessment.model}</span>
        <span className="num">{(assessment.latency_ms / 1000).toFixed(1)} sn</span>
        <span className="num">
          {assessment.prompt_tokens} + {assessment.completion_tokens} token
        </span>
        {!assessment.schema_valid && <span style={{ color: "var(--warn)" }}>şema onarıldı</span>}
      </footer>
    </article>
  );
}
