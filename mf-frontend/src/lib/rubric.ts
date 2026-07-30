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

// ---- Prompt bütçesi ----
//
// Vaka metni motorun girdi penceresine sığmak zorunda, ve `analysis` yolunda
// bunu kontrol eden bir koruma yok (`decision`'da var — agent.go:78). Sığmayan
// bir istek mlc'den ham 400 olarak dönüyor, yani kullanıcı sebebini göremiyor.
// Bu yüzden sayaç göndermeden önce uyarıyor.
//
// Aşağıdaki üç oran tahmin değil, ölçüm: Qwen3-4B-Instruct-2507'nin kendi
// tokenizer'ıyla, 30 Temmuz 2026'da alındı. Tekrar ölçülmeden değiştirilmemeli.

/**
 * Türkçe düzyazının ölçülen sıkışması. İngilizce ortalamasından belirgin
 * biçimde kötü: eklemeli yapı ve çoğu İngilizce olan bir kelime dağarcığı
 * birleşince aynı cümle daha çok token ediyor.
 */
export const PROSE_CHARS_PER_TOKEN = 2.09;

/**
 * Sistem prompt'unun sıkışması. Düzyazıdan iyi, çünkü içeriği ASCII anahtarlar,
 * JSON şeması ve tekrar eden kalıplar — hepsi tokenizer'ın iyi bildiği şeyler.
 */
export const PROMPT_CHARS_PER_TOKEN = 3.0;

/** Kullanıcı mesajının sabit sarmalayıcısı (UserPrompt'un <<< >>> bloğu). */
export const WRAPPER_TOKENS = 43;

/** Sohbet şablonunun tur işaretleri ve yuvarlama için pay. */
const MARGIN_TOKENS = 24;

/** Metnin kaç token edeceğinin tahmini. Yukarı yuvarlar: düşük tahmin isteği kaybettirir. */
export function estimateTokens(text: string): number {
  if (!text) return 0;
  return Math.ceil(text.length / PROSE_CHARS_PER_TOKEN);
}

/**
 * Vaka metnine kalan karakter sayısı.
 *
 * @param windowTokens motorun kabul ettiği girdi penceresi (backend'den gelir)
 * @param systemPromptChars seçili rubriğin sistem prompt'unun uzunluğu
 */
export function caseBudgetChars(windowTokens: number, systemPromptChars: number): number {
  const systemTokens = Math.ceil(systemPromptChars / PROMPT_CHARS_PER_TOKEN);
  const left = windowTokens - systemTokens - WRAPPER_TOKENS - MARGIN_TOKENS;
  if (left <= 0) return 0;
  return Math.floor(left * PROSE_CHARS_PER_TOKEN);
}
