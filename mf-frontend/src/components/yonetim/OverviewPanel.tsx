"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { AdminOverview } from "@/lib/types";
import { Stat } from "./Stat";

export function OverviewPanel() {
  const [data, setData] = useState<AdminOverview | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api.admin
      .overview()
      .then(setData)
      .catch((e: ApiError) => setError(e.message));
  }, []);

  if (error) return <div className="notice notice-bad">{error}</div>;
  if (!data) {
    return (
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {[0, 1, 2, 3, 4, 5].map((i) => (
          <div key={i} className="card p-4 space-y-2.5">
            <div className="skeleton h-3 w-24" />
            <div className="skeleton h-8 w-20" />
            <div className="skeleton h-3 w-32" />
          </div>
        ))}
      </div>
    );
  }

  // The one number on this panel that says "roll back". A drop here after
  // activating an adapter means the build made output worse, and it is easy to
  // miss among the volume counters, so it leads and carries its own colour.
  const schemaPct = Math.round(data.schema_valid_rate_24h * 100);
  const schemaTone =
    data.assessments_last_24h === 0
      ? "var(--text-faint)"
      : schemaPct >= 80
        ? "var(--ok)"
        : schemaPct >= 40
          ? "var(--warn)"
          : "var(--bad)";

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <Stat
        index={0}
        label="Şema uyumu (24s)"
        value={data.assessments_last_24h === 0 ? "—" : `%${schemaPct}`}
        tone={schemaTone}
        hint="Adapter aktive ettikten sonra bu düşerse geri al. Onarılmış her çıktı, güvenilmemesi gereken bir rapordur."
      />
      <Stat
        index={1}
        label="Analiz (24s)"
        value={String(data.assessments_last_24h)}
        hint={`toplam ${data.assessments}`}
      />
      <Stat
        index={2}
        label="p95 gecikme (24s)"
        value={`${(data.p95_latency_ms_24h / 1000).toFixed(1)}s`}
        hint={`ortalama ${(data.avg_latency_ms_24h / 1000).toFixed(1)}s`}
      />
      <Stat
        index={3}
        label="Adapter build"
        value={`${data.adapters_ready}/${data.adapters_total}`}
        hint="servis edilebilir / toplam"
      />
      <Stat
        index={4}
        label="Çalıştırma"
        value={String(data.runs_last_24h)}
        hint={`toplam ${data.total_runs}`}
      />
      <Stat index={5} label="Kullanıcı" value={String(data.total_users)} />
      <GrafanaCard />
    </div>
  );
}

// Grafana is linked, not embedded. An iframe cannot send the gateway's
// X-API-Key, so framing it would mean either publishing Grafana without that
// check or proxying every asset through the backend; the reasoning is in
// mf-observability/README.md.
//
// Read at module scope because NEXT_PUBLIC_* is inlined at build time — there
// is no runtime value to wait for. Unset is a supported state: a deployment
// with no tunnel renders nothing here rather than a button that cannot work.
const GRAFANA_URL = process.env.NEXT_PUBLIC_GRAFANA_URL ?? "";

function GrafanaCard() {
  if (!GRAFANA_URL) return null;
  return (
    <a
      href={GRAFANA_URL}
      target="_blank"
      // noreferrer as well as noopener: the target is a hostname that is not
      // meant to be discoverable, and Referer would leak it into any logging on
      // the other side.
      rel="noopener noreferrer"
      className="card card-action item-in p-4 block"
      style={{ ["--i" as string]: 6 }}
    >
      <div className="eyebrow">Grafana</div>
      <div className="font-display text-2xl font-semibold mt-1.5 flex items-center gap-2 tracking-tight">
        Panolar
        <span style={{ color: "var(--brand)" }}>→</span>
      </div>
      <div
        className="text-xs mt-2 leading-relaxed"
        style={{ color: "var(--text-dim)" }}
      >
        Metrikler ve loglar ayrı sekmede. Çıkarım sunucusu kapalıyken açılmaz —
        burada gördüğün sayılar backend&apos;den gelir, Grafana&apos;dan değil.
      </div>
    </a>
  );
}
