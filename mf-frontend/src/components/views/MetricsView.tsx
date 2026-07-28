"use client";

// The metrics tab. Four charts over the same window, drawn from Prometheus by
// way of the backend.
//
// Grafana is not embedded here and that is deliberate. An iframe cannot send the
// inference gateway's API key, so framing it would mean either publishing
// Grafana without that check or proxying every asset through the backend; and an
// embedded dashboard shows every viewer the same queries, which stops being
// acceptable the moment these numbers are scoped per user. Grafana stays the
// operator's tool for questions nobody anticipated. These are the four worth
// having without leaving the product.
//
// The queries themselves live on the server. Nothing here can ask for a series
// the panel was not built to show.

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { useAuth } from "@/store/auth";
import { TimeChart } from "@/components/ui/TimeChart";
import { Segmented } from "@/components/ui/Segmented";
import { RoleGate } from "@/components/ui/RoleGate";
import type { MetricsResponse, MetricsWindow } from "@/lib/types";

const WINDOWS: { id: MetricsWindow; label: string }[] = [
  { id: "1h", label: "1 saat" },
  { id: "6h", label: "6 saat" },
  { id: "24h", label: "24 saat" },
];

export function MetricsView() {
  const { user } = useAuth();
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

  if (user?.role !== "admin") {
    return (
      <RoleGate title="Bu bölüm yönetici hesabı gerektiriyor">
        Metrikler tüm kullanıcıların trafiğini birlikte gösterir, o yüzden rol
        kontrolü sunucuda da var — bu ekranın gizlenmesi kolaylık, sınır değil.
      </RoleGate>
    );
  }

  return (
    <div className="max-w-6xl mx-auto p-4 sm:p-5 space-y-4">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h2 className="font-display text-xl font-semibold tracking-tight">
            Metrikler
          </h2>
          <p className="text-sm mt-1" style={{ color: "var(--text-dim)" }}>
            Çıkarım sunucusunun kendi telemetrisi, seçili aralık boyunca.
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
            veritabanından değil. Çıkarım sunucusu kapalıyken bu ekran boş kalır ve
            Yönetim → Genel&apos;deki sayılar çalışmaya devam eder.
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

      {/* The shape of what is coming, so the layout does not jump when it does. */}
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
