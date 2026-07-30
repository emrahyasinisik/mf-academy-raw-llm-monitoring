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
import type { AnalysisDomain } from "@/lib/types";

export function AnalizView() {
  const [domains, setDomains] = useState<AnalysisDomain[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);

  const loadDomains = useCallback(() => {
    api
      .analysisDomains()
      .then((d) => {
        setDomains(d.domains);
        setLoadError(null);
      })
      .catch((e: unknown) =>
        setLoadError(e instanceof ApiError ? e.message : "Rubrikler yüklenemedi."),
      );
  }, []);

  useEffect(loadDomains, [loadDomains]);

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
          <p className="mono text-xs mt-4" style={{ color: "var(--text-faint)" }}>
            {domains.length} rubrik yüklendi
          </p>
        )}
      </div>
    </div>
  );
}
