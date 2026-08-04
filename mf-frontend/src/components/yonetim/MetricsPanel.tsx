"use client";

// Prometheus telemetrisi — panelde, çünkü yalnızca yönetici görmeli ve
// ürün nav'ında herkese açık bir sekme yanlış sinyal veriyordu.
//
// Grafana gömülü değil: iframe API anahtarını taşıyamaz; Grafana operatörün
// öngörülmeyen soruları için ayrı kalır. Sorgular sunucuda; burada yalnızca
// gösterilir.

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { TimeChart } from "@/components/ui/TimeChart";
import { Segmented } from "@/components/ui/Segmented";
import type { MetricsResponse, MetricsWindow } from "@/lib/types";

const WINDOWS: { id: MetricsWindow; label: string }[] = [
  { id: "1h", label: "1 saat" },
  { id: "6h", label: "6 saat" },
  { id: "24h", label: "24 saat" },
];

export function MetricsPanel() {
  const [range, setRange] = useState<MetricsWindow>("1h");
  const [table, setTable] = useState(false);
  const [data, setData] = useState<MetricsResponse | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    // Guards the out-of-order case: click 24h then 1h quickly and the slower
    // response can land last. The flag makes a superseded request write nothing
    // rather than paint a window the reader has already left.
    let current = true;
    api.admin
      .metrics(range)
      .then((d) => {
        if (!current) return;
        setData(d);
        setError("");
      })
      .catch((e: ApiError) => current && setError(e.message));
    return () => {
      current = false;
    };
  }, [range]);

  // Derived rather than a loading flag: the response carries the window it
  // answers, so "showing something older than what was asked for" is a fact
  // about the data, not a second piece of state that can disagree with it.
  const stale = data !== null && data.window !== range;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h1 className="text-lg">Metrikler</h1>
          <p className="text-sm mt-1" style={{ color: "var(--text-dim)" }}>
            Çıkarım sunucusunun kendi telemetrisi, seçili aralık boyunca.
            Kutusu kapalıyken bu ekran boş kalır; Genel&apos;deki DB sayıları
            çalışmaya devam eder.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Segmented
            items={WINDOWS}
            active={range}
            onSelect={setRange}
            label="Zaman aralığı"
            size="sm"
          />
          <button
            type="button"
            onClick={() => setTable((t) => !t)}
            className={`btn btn-sm ${table ? "btn-ghost" : "btn-quiet"}`}
            aria-pressed={table}
            title="Grafiklerin altındaki sayıları tablo olarak göster"
          >
            Tablo
          </button>
        </div>
      </div>

      {error && (
        <div className="card p-4 space-y-2">
          <p className="text-sm font-semibold" style={{ color: "var(--bad)" }}>
            {error}
          </p>
          <p className="text-xs leading-relaxed" style={{ color: "var(--text-dim)" }}>
            Metrikler Prometheus&apos;tan geliyor, backend&apos;in kendi
            veritabanından değil. Çıkarım sunucusu kapalıyken bu ekran boş kalır.
          </p>
        </div>
      )}

      {data && (
        <div
          className="grid gap-4 lg:grid-cols-2"
          style={{
            opacity: stale ? 0.5 : 1,
            transition: "opacity var(--dur-2) var(--ease)",
          }}
        >
          {data.panels.map((p, i) => (
            <section
              key={p.id}
              className="card item-in p-4"
              style={{ ["--i" as string]: i }}
            >
              <h3 className="font-display font-semibold text-[0.95rem]">{p.title}</h3>
              <p
                className="text-xs mt-1 mb-3 leading-relaxed"
                style={{ color: "var(--text-dim)" }}
              >
                {p.help}
              </p>
              {p.error ? (
                <p
                  className="text-xs py-10 text-center"
                  style={{ color: "var(--warn)" }}
                >
                  {p.error}
                </p>
              ) : (
                <TimeChart series={p.series ?? []} unit={p.unit} showTable={table} />
              )}
            </section>
          ))}
        </div>
      )}

      {!data && !error && (
        <div className="grid gap-4 lg:grid-cols-2" aria-label="Yükleniyor">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="card p-4 space-y-3">
              <div className="skeleton h-4 w-40" />
              <div className="skeleton h-3 w-64" />
              <div className="skeleton h-[170px] w-full" />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
