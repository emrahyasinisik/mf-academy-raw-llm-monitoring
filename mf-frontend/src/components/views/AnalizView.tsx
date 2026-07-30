"use client";

// Rubrik analizi: bir vaka girer, rubrik-puanlı bir rapor çıkar.
//
// Ürünün kendisi bu ekran. Motor uzun süredir çalışıyordu ama yalnızca API ve
// MCP üzerinden ulaşılabiliyordu, yani alıcıya açıp gösterilebilecek bir yeri
// yoktu — satılabilir ama gösterilemez bir durum.
//
// Neden bir sohbet değil de form: rubrik doldurmak çıkarım işidir, konuşma
// değil. Aynı vaka aynı okumayı vermeli, ve ürünün satıldığı şey tam olarak o
// tutarlılık. Sıcaklık da bu yüzden backend'de sabitlenmiş (analysisTemperature
// 0.1), operatörün sohbet için ayarladığı bir kaydırıcıya bırakılmamış.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { caseBudgetChars, estimateTokens } from "@/lib/rubric";
import type { AnalysisDomain, AppLimits } from "@/lib/types";

export function AnalizView() {
  const [domains, setDomains] = useState<AnalysisDomain[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [slug, setSlug] = useState("");
  const [title, setTitle] = useState("");
  const [subject, setSubject] = useState("");

  // Pencere dağıtıma göre değişiyor, o yüzden sunucudan geliyor. Ölçülen
  // karakter/token oranları lib/rubric.ts'te sabit — onlar dile ve tokenizer'a
  // ait, bu dağıtıma değil.
  const [windowTokens, setWindowTokens] = useState(1200);
  // Ölçüm, ait olduğu rubrikle birlikte saklanıyor. Ayrı bir sayı tutup rubrik
  // değişince effect içinden sıfırlamak, bir kare boyunca önceki rubriğin
  // bütçesini gösterme ihtimali bırakıyordu — ve yanlış bütçe, bütçesizlikten
  // kötü. Böyle tutulunca eskimiş ölçüm render'da elenmiş oluyor.
  const [prompt, setPrompt] = useState<{ slug: string; chars: number } | null>(null);

  const loadDomains = useCallback(() => {
    api
      .analysisDomains()
      .then((d) => {
        setDomains(d.domains);
        setLoadError(null);
        // Varsayılan rubrik yatırılabilirlik: beachhead ICP hızlandırma
        // programları ve melek ağları.
        const first =
          d.domains.find((x) => x.slug === "startup-investability") ?? d.domains[0];
        if (first) setSlug((s) => s || first.slug);
      })
      .catch((e: unknown) =>
        setLoadError(e instanceof ApiError ? e.message : "Rubrikler yüklenemedi."),
      );
  }, []);

  useEffect(loadDomains, [loadDomains]);

  // Pencere: sunucunun bildirdiği sayı, alınamazsa backend'in kendi varsayılanı.
  useEffect(() => {
    api
      .config()
      .then((c) => {
        const limits = (c as { limits?: Partial<AppLimits> }).limits;
        if (limits?.max_prompt_tokens) setWindowTokens(limits.max_prompt_tokens);
      })
      .catch(() => {
        /* Varsayılan yeterli: bütçe biraz yanılır, gönderim engellenmez. */
      });
  }, []);

  // Seçili rubriğin sistem prompt'u ne kadar yer yiyor. İki rubrik arasında
  // 565 karakter fark var, yani bu rubrik başına okunmak zorunda.
  useEffect(() => {
    if (!slug) return;
    let cancelled = false;
    api
      .analysisPrompt(slug)
      .then((p) => {
        if (!cancelled) setPrompt({ slug, chars: p.system_prompt.length });
      })
      .catch(() => {
        if (!cancelled) setPrompt(null);
      });
    return () => {
      cancelled = true;
    };
  }, [slug]);

  // Başka bir rubriğe ait ölçüm burada eleniyor.
  const systemChars = prompt && prompt.slug === slug ? prompt.chars : null;

  // Ölçüm henüz gelmediyse bütçe gösterilmiyor: yanlış bir sayı göstermek,
  // sayı göstermemekten kötü.
  const budget = systemChars === null ? null : caseBudgetChars(windowTokens, systemChars);
  const over = budget !== null && subject.length > budget;
  const canRun = slug !== "" && subject.trim() !== "" && !over;

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-6xl px-4 sm:px-5 py-6">
        <h1 className="font-display text-lg font-semibold">Analiz</h1>
        <p className="text-sm mt-1" style={{ color: "var(--text-dim)" }}>
          Bir vaka girin, rubrik-puanlı bir rapor alın.
        </p>

        {loadError && (
          <div className="notice notice-bad mt-4" role="alert">
            {loadError}
          </div>
        )}

        {!loadError && domains.length === 0 && (
          <p className="mono text-xs mt-4" style={{ color: "var(--text-faint)" }}>
            rubrikler yükleniyor…
          </p>
        )}

        {domains.length > 0 && (
          <div className="card mt-5 p-4 space-y-4">
            <div>
              <label className="label" htmlFor="analiz-rubrik">
                Rubrik
              </label>
              <select
                id="analiz-rubrik"
                className="input"
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
              >
                {domains.map((d) => (
                  <option key={d.slug} value={d.slug}>
                    {d.name} · v{d.version} · {d.criteria.length} kriter
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label className="label" htmlFor="analiz-baslik">
                Vaka başlığı
              </label>
              <input
                id="analiz-baslik"
                className="input"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="Şirket veya vaka adı"
              />
            </div>

            <div>
              <div className="flex items-baseline justify-between gap-3">
                <label className="label" htmlFor="analiz-vaka">
                  Vaka metni
                </label>
                <span
                  className="mono text-xs num"
                  style={{ color: over ? "var(--bad)" : "var(--text-faint)" }}
                >
                  {subject.length}
                  {budget !== null && ` / ${budget}`} karakter
                  {budget !== null && ` · ~${estimateTokens(subject)} token`}
                </span>
              </div>
              <textarea
                id="analiz-vaka"
                className="input"
                rows={10}
                value={subject}
                onChange={(e) => setSubject(e.target.value)}
                placeholder="Değerlendirilecek metni buraya yapıştırın."
                aria-invalid={over}
                aria-describedby={over ? "analiz-vaka-uyari" : undefined}
              />
            </div>

            {over && budget !== null && (
              <div className="notice notice-warn" id="analiz-vaka-uyari" role="alert">
                Metin bu rubriğin bıraktığı yerden {subject.length - budget} karakter
                uzun. Rubriğin kendisi de modele gönderiliyor ve pencereden yer
                kaplıyor; bu sınırın üstünde istek değerlendirilmeden reddedilir.
              </div>
            )}

            <div className="flex items-center gap-3">
              <button className="btn btn-primary" disabled={!canRun}>
                Analiz et
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
