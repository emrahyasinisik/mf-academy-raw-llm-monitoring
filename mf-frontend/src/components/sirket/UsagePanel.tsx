"use client";

// Şirket kullanım paneli — yönetim StatsPanel desenini kopyalar, import etmez.
// Prometheus yok: sayılar Postgres'ten, GPU kutusu kapalıyken de dolu kalır.

import { useCallback, useEffect, useMemo, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { changeLabel, changeTone, targetSeries } from "@/lib/stats";
import type { MetricSeries, OrgStats, StatsWindow } from "@/lib/types";
import { Segmented } from "@/components/ui/Segmented";
import { TimeChart } from "@/components/ui/TimeChart";
import { OrgStat } from "./OrgStat";

const WINDOWS: readonly { id: StatsWindow; label: string }[] = [
  { id: "30d", label: "30 gün" },
  { id: "90d", label: "90 gün" },
];

type ChartSpec = {
  id: string;
  title: string;
  help: string;
  series: MetricSeries[];
  unit: "count" | "percent";
};

export function UsagePanel() {
  const [statsWindow, setStatsWindow] = useState<StatsWindow>("30d");
  const [table, setTable] = useState(false);
  const [data, setData] = useState<OrgStats | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const loadStats = useCallback(
    (selectedWindow: StatsWindow, isCurrent: () => boolean = () => true) => {
      return api.org
        .stats(selectedWindow)
        .then((res) => {
          if (!isCurrent()) return;
          setData(res);
        })
        .catch((e: unknown) => {
          if (!isCurrent()) return;
          setError(e instanceof ApiError ? e.message : "Kullanım yüklenemedi.");
        })
        .finally(() => {
          if (!isCurrent()) return;
          setLoading(false);
        });
    },
    [],
  );

  const selectWindow = useCallback(
    (nextWindow: StatsWindow) => {
      if (nextWindow === statsWindow) return;
      setLoading(true);
      setError("");
      setStatsWindow(nextWindow);
    },
    [statsWindow],
  );

  useEffect(() => {
    let current = true;
    void loadStats(statsWindow, () => current);
    return () => {
      current = false;
    };
  }, [loadStats, statsWindow]);

  const charts = useMemo<ChartSpec[]>(() => {
    if (!data) return [];
    return [
      {
        id: "assessment-volume",
        title: "Analiz hacmi",
        help: "Şirket üyelerinin seçili aralıkta ürettiği rapor sayısı, gün gün.",
        series: [
          {
            label: "Analiz",
            points: data.assessments_per_day.map((d) => ({ t: d.t, v: d.v })),
          },
        ],
        unit: "count",
      },
      {
        id: "schema-validity",
        title: "Şema geçerliliği",
        help: "O gün üretilen raporlardan şemaya uyanların oranı.",
        series: [
          {
            label: "Şema geçerliliği",
            points: data.assessments_per_day
              .map((day, i) => {
                const valid = data.schema_valid_per_day[i]?.v ?? 0;
                if (day.v === 0) return null;
                return { t: day.t, v: valid / day.v };
              })
              .filter((p): p is { t: number; v: number } => p !== null),
          },
        ],
        unit: "percent",
      },
      {
        id: "runs-by-target",
        title: "Çalışma hacmi",
        help: "Tarayıcı ve sunucu çalışmaları ayrı okunur; hedefler aynı davranışı ölçmez.",
        series: targetSeries(data.runs_by_target),
        unit: "count",
      },
    ];
  }, [data]);

  const stale = data !== null && data.window !== statsWindow;

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h3 className="font-display font-semibold">Kullanım</h3>
          <p className="text-xs mt-1" style={{ color: "var(--text-faint)" }}>
            Şirket üyelerinin analiz ve çalışma hacmi — yalnızca sizin org&apos;unuz.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Segmented
            items={WINDOWS}
            active={statsWindow}
            onSelect={selectWindow}
            label="Zaman aralığı"
            size="sm"
          />
          <button
            className={`btn btn-sm ${table ? "btn-ghost" : "btn-quiet"}`}
            type="button"
            aria-pressed={table}
            title="Grafiklerdeki sayıları tablo olarak göster"
            onClick={() => setTable((t) => !t)}
          >
            Tablo
          </button>
        </div>
      </div>

      {error && (
        <div className="notice notice-bad" role="alert">
          {error}
        </div>
      )}

      {loading && !data ? (
        <UsageSkeleton />
      ) : (
        data && (
          <div
            className="space-y-4"
            style={{
              opacity: stale || loading ? 0.55 : 1,
              transition: "opacity var(--dur-2) var(--ease)",
            }}
          >
            <UsageBoxes data={data} />
            <div className="grid gap-4 lg:grid-cols-2">
              {charts.map((chart, i) => (
                <section
                  key={chart.id}
                  className="card item-in p-4"
                  style={{ ["--i" as string]: i }}
                >
                  <h4 className="font-display font-semibold text-[0.95rem]">
                    {chart.title}
                  </h4>
                  <p
                    className="text-xs mt-1 mb-3 leading-relaxed"
                    style={{ color: "var(--text-dim)" }}
                  >
                    {chart.help}
                  </p>
                  <TimeChart series={chart.series} unit={chart.unit} showTable={table} />
                </section>
              ))}
            </div>
            <MemberActivityTable rows={data.member_activity} />
          </div>
        )
      )}
    </section>
  );
}

export function UsageBoxes({ data }: { data: OrgStats }) {
  const { boxes } = data;
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <OrgStat
        index={0}
        label="Üyeler / koltuk"
        value={`${boxes.members.count} / ${boxes.members.seat_limit}`}
        hint="Anlık doluluk; pencereye bağlı değil"
      />
      <OrgStat
        index={1}
        label="Son 24 saat"
        value={formatCount(boxes.reports_last_24h.value)}
        hint={`${changeLabel(boxes.reports_last_24h.change_pct)} önceki 24 saate göre`}
        tone={changeTone(boxes.reports_last_24h.change_pct)}
      />
      <OrgStat
        index={2}
        label="Pencere raporları"
        value={formatCount(boxes.reports_window.value)}
        hint={`${changeLabel(boxes.reports_window.change_pct)} önceki pencereye göre`}
        tone={changeTone(boxes.reports_window.change_pct)}
      />
      <OrgStat
        index={3}
        label="Şema uyumu"
        value={formatPercent(boxes.schema_validity.rate)}
        hint="Seçili penceredeki oran"
      />
    </div>
  );
}

function MemberActivityTable({ rows }: { rows: OrgStats["member_activity"] }) {
  if (rows.length === 0) {
    return (
      <section className="card p-4">
        <h4 className="font-display font-semibold text-[0.95rem]">Üye aktivitesi</h4>
        <p className="text-sm mt-2" style={{ color: "var(--text-dim)" }}>
          Bu pencerede analiz üreten üye yok.
        </p>
      </section>
    );
  }
  return (
    <section className="card p-4 overflow-x-auto">
      <h4 className="font-display font-semibold text-[0.95rem]">Üye aktivitesi</h4>
      <p className="text-xs mt-1 mb-3" style={{ color: "var(--text-dim)" }}>
        Üye başına analiz sayısı ve son analiz zamanı — vaka içeriği yok.
      </p>
      <table className="w-full text-sm">
        <thead>
          <tr style={{ color: "var(--text-faint)" }}>
            <th className="text-left font-medium py-2">Üye</th>
            <th className="text-right font-medium py-2">Analiz</th>
            <th className="text-right font-medium py-2">Son</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.user_id} style={{ borderTop: "1px solid var(--line)" }}>
              <td className="py-2">{row.name || "—"}</td>
              <td className="py-2 text-right num">{formatCount(row.count)}</td>
              <td className="py-2 text-right" style={{ color: "var(--text-dim)" }}>
                {row.last_at ? formatDateTime(row.last_at) : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

function UsageSkeleton() {
  return (
    <div className="space-y-4" aria-label="Yükleniyor">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="card p-4 space-y-2.5">
            <div className="skeleton h-3 w-24" />
            <div className="skeleton h-8 w-20" />
            <div className="skeleton h-3 w-32" />
          </div>
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        {[0, 1].map((i) => (
          <div key={i} className="card p-4 space-y-3">
            <div className="skeleton h-4 w-40" />
            <div className="skeleton h-[170px] w-full" />
          </div>
        ))}
      </div>
    </div>
  );
}

function formatCount(value: number): string {
  return value.toLocaleString("tr-TR");
}

function formatPercent(rate: number): string {
  return `%${(rate * 100).toLocaleString("tr-TR", { maximumFractionDigits: 1 })}`;
}

function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("tr-TR", {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}
