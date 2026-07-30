// Raporun aritmetiği.
//
// Bu dosya internal/analysis/scoring.go'nun bir kopyası, ve öyle kalmalı.
// Backend puanı hesaplayıp `overall_score` olarak gönderiyor; burada yeniden
// hesaplanmasının tek sebebi, o puanın **nasıl** çıktığını satır satır
// gösterebilmek. İki taraf ayrışırsa rapor kendi toplamıyla çelişir — ve
// aritmetiğini gösterip yanlış toplayan bir rapor, hiç göstermeyenden kötüdür.
// Bir kural burada değişecekse önce scoring.go'da değişir.

import type { Criterion, Finding } from "./types";

/** Bir rubrik satırının rapordaki hali. */
export interface Row {
  criterion: Criterion;
  /** Model o kriter için bir şey döndürmediyse null. */
  finding: Finding | null;
  /** Toplama katılıyor mu. */
  scored: boolean;
  /** [0, scale_max] aralığına kırpılmış puan; puanlanmadıysa null. */
  clamped: number | null;
  /** Bu satırın toplam puana katkısı; puanlanmadıysa null. */
  points: number | null;
}

export interface Breakdown {
  rows: Row[];
  /** Hiçbir şey değerlendirilemediyse null. Sıfır değil. */
  overall: number | null;
  /** Kanıtı olan kriterlerin ağırlık payı. */
  coverage: number;
  scoredWeight: number;
  totalWeight: number;
}

/** scoring.go'daki EffectiveScaleMax: sıfır bölmeyi sessiz NaN'a çevirmesin. */
function effectiveScaleMax(c: Criterion): number {
  return c.scale_max > 0 ? c.scale_max : 5;
}

export function breakdown(criteria: Criterion[], findings: Finding[]): Breakdown {
  const byKey = new Map(findings.map((f) => [f.key, f]));

  const rows: Row[] = [];
  let weightedSum = 0;
  let scoredWeight = 0;
  let totalWeight = 0;

  for (const criterion of criteria) {
    const finding = byKey.get(criterion.key) ?? null;

    // Ağırlığı ≤ 0 olan kriter tamamen atlanır: sıfır zaten katkı vermez,
    // negatif olan bir kriterin bir diğerini götürmesine izin verirdi.
    if (criterion.weight <= 0) {
      rows.push({ criterion, finding, scored: false, clamped: null, points: null });
      continue;
    }
    totalWeight += criterion.weight;

    if (!finding || !finding.evidence_found || finding.score === null) {
      rows.push({ criterion, finding, scored: false, clamped: null, points: null });
      continue;
    }

    // Kırpılır, reddedilmez: model 0-5 skalasında bazen 6 döndürüyor ve bunu
    // azami saymak, bulguyu tamamen atmaktan daha az bilgi kaybettiriyor.
    const max = effectiveScaleMax(criterion);
    const clamped = Math.max(0, Math.min(finding.score, max));

    weightedSum += criterion.weight * (clamped / max);
    scoredWeight += criterion.weight;

    rows.push({ criterion, finding, scored: true, clamped, points: null });
  }

  if (totalWeight <= 0) {
    return { rows, overall: null, coverage: 0, scoredWeight: 0, totalWeight: 0 };
  }
  const coverage = scoredWeight / totalWeight;

  if (scoredWeight <= 0) {
    return { rows, overall: null, coverage, scoredWeight, totalWeight };
  }

  // scoredWeight'e göre yeniden normalize edilir, totalWeight'e göre değil.
  // Tam rubrik ağırlığına bölmek kapsamı puanın içine katlardı: ele alınmamış
  // bir kriter, kötü ele alınmış biri kadar puanı aşağı çekerdi.
  const value = (100 * weightedSum) / scoredWeight;
  if (!Number.isFinite(value)) {
    return { rows, overall: null, coverage, scoredWeight, totalWeight };
  }

  // Satır katkıları aynı paydayı kullanır, yani tam olarak toplama eşitlenir.
  for (const row of rows) {
    if (!row.scored || row.clamped === null) continue;
    const max = effectiveScaleMax(row.criterion);
    row.points = (100 * row.criterion.weight * (row.clamped / max)) / scoredWeight;
  }

  return {
    rows,
    overall: Math.round(value * 100) / 100,
    coverage,
    scoredWeight,
    totalWeight,
  };
}
