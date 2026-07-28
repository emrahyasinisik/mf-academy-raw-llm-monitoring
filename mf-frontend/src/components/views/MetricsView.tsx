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
import type { MetricsResponse, MetricsWindow } from "@/lib/types";

const WINDOWS: { id: MetricsWindow; label: string }[] = [
  { id: "1h", label: "Son 1 saat" },
  { id: "6h", label: "Son 6 saat" },
  { id: "24h", label: "Son 24 saat" },
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
      <div className="max-w-3xl mx-auto p-5">
        <div className="card p-5">
          <h2 className="text-sm font-semibold mb-2">
            Bu bölüm yönetici hesabı gerektiriyor
          </h2>
          <p className="text-xs leading-relaxed" style={{ color: "var(--text-dim)" }}>
            Metrikler tüm kullanıcıların trafiğini birlikte gösterir, o yüzden rol
            kontrolü sunucuda da var — bu ekranın gizlenmesi kolaylık, sınır değil.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-6xl mx-auto p-5 space-y-4">
      {/* One filter row, above everything it scopes. */}
      <div className="flex items-center gap-1 flex-wrap">
        {WINDOWS.map((w) => (
          <button
            key={w.id}
            onClick={() => setRange(w.id)}
            className={`btn ${range === w.id ? "btn-primary" : "btn-ghost"} !py-1.5 !px-3 text-xs`}
          >
            {w.label}
          </button>
        ))}
        <span className="flex-1" />
        <button
          onClick={() => setTable((t) => !t)}
          className={`btn ${table ? "btn-primary" : "btn-ghost"} !py-1.5 !px-3 text-xs`}
          aria-pressed={table}
        >
          Tablo
        </button>
      </div>

      {error && (
        <div className="card p-4">
          <p className="text-xs" style={{ color: "var(--bad)" }}>
            {error}
          </p>
          <p className="text-xs mt-2 leading-relaxed" style={{ color: "var(--text-dim)" }}>
            Metrikler kutudaki Prometheus&apos;tan geliyor, backend&apos;in kendi
            veritabanından değil. Kutu kapalıysa bu ekran boş kalır ve Yönetim →
            Genel&apos;deki sayılar çalışmaya devam eder.
          </p>
        </div>
      )}

      {data && (
        <div
          className="grid gap-4 lg:grid-cols-2 transition-opacity"
          style={{ opacity: stale ? 0.55 : 1 }}
        >
          {data.panels.map((p) => (
            <div key={p.id} className="card p-4">
              <h3 className="text-sm font-semibold">{p.title}</h3>
              <p
                className="text-xs mt-0.5 mb-2 leading-relaxed"
                style={{ color: "var(--text-dim)" }}
              >
                {p.help}
              </p>
              {p.error ? (
                <p className="text-xs py-8 text-center" style={{ color: "var(--warn)" }}>
                  {p.error}
                </p>
              ) : (
                <TimeChart series={p.series ?? []} unit={p.unit} showTable={table} />
              )}
            </div>
          ))}
        </div>
      )}

      {!data && !error && (
        <p className="text-xs" style={{ color: "var(--text-faint)" }}>
          Yükleniyor…
        </p>
      )}
    </div>
  );
}
