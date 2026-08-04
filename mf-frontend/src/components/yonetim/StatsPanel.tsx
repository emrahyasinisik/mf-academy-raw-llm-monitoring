"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { api, ApiError } from "@/lib/api";
import {
  changeLabel,
  changeTone,
  daySeries,
  validitySeries,
} from "@/lib/stats";
import type { AdminStats, MetricSeries, StatsWindow } from "@/lib/types";
import { Segmented } from "@/components/ui/Segmented";
import { TimeChart } from "@/components/ui/TimeChart";
import { Stat } from "./Stat";

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

export function StatsPanel() {
  const [statsWindow, setStatsWindow] = useState<StatsWindow>("30d");
  const [table, setTable] = useState(false);
  const [data, setData] = useState<AdminStats | null>(null);
  const [error, setError] = useState("");
  const [loadingList, setLoadingList] = useState(true);

  const loadStats = useCallback(
    (selectedWindow: StatsWindow, isCurrent: () => boolean = () => true) => {
      return api.admin
        .stats(selectedWindow)
        .then((res) => {
          if (!isCurrent()) return;
          setData(res);
        })
        .catch((e: ApiError) => {
          if (!isCurrent()) return;
          setError(e.message);
        })
        .finally(() => {
          if (!isCurrent()) return;
          setLoadingList(false);
        });
    },
    [],
  );

  const selectWindow = useCallback(
    (nextWindow: StatsWindow) => {
      if (nextWindow === statsWindow) return;
      setLoadingList(true);
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
        id: "new-users",
        title: "Yeni kayıt (gün)",
        help: "Seçili aralıkta her gün açılan yeni üyelik sayısı.",
        series: [daySeries(data.days, "new_users", "Yeni kayıt")],
        unit: "count",
      },
      {
        id: "total-users",
        title: "Toplam üye",
        help: "Kümülatif üye toplamı ayrı eksende okunur; günlük kayıtla aynı grafikte çizilmez.",
        series: [daySeries(data.days, "cumulative_users", "Toplam üye")],
        unit: "count",
      },
      {
        id: "assessment-volume",
        title: "Analiz hacmi",
        help: "Seçili aralıkta gün gün üretilen rapor sayısı.",
        series: [daySeries(data.days, "assessments", "Analiz")],
        unit: "count",
      },
      {
        id: "schema-validity",
        title: "Şema geçerliliği",
        help: "Adapter aktive edildikten sonra düşerse geri alma sinyali.",
        series: [validitySeries(data.days)],
        unit: "percent",
      },
    ];
  }, [data]);

  const stale = data !== null && data.window !== statsWindow;

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h3 className="font-display font-semibold">Panel özeti</h3>
          <p className="text-xs mt-1" style={{ color: "var(--text-faint)" }}>
            Üyelik ve rapor akışı seçili zaman penceresine göre okunur.
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

      {loadingList && !data ? (
        <StatsSkeleton />
      ) : (
        data && (
          <div
            className="space-y-4"
            style={{
              opacity: stale || loadingList ? 0.55 : 1,
              transition: "opacity var(--dur-2) var(--ease)",
            }}
          >
            <StatsBoxes data={data} />
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
          </div>
        )
      )}
    </section>
  );
}

function StatsBoxes({ data }: { data: AdminStats }) {
  const { boxes } = data;
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <Stat
        index={0}
        label="Toplam üye"
        value={formatCount(boxes.total_users.value)}
        hint={`${changeLabel(boxes.total_users.change_pct)} önceki pencereye göre`}
        tone={changeTone(boxes.total_users.change_pct)}
      />
      <Stat
        index={1}
        label="Toplam rapor"
        value={formatCount(boxes.total_reports.value)}
        hint={`${changeLabel(boxes.total_reports.change_pct)} önceki pencereye göre`}
        tone={changeTone(boxes.total_reports.change_pct)}
      />
      <Stat
        index={2}
        label="Son 24 saat"
        value={formatCount(boxes.reports_last_24h.value)}
        hint={`${changeLabel(boxes.reports_last_24h.change_pct)} önceki 24 saate göre`}
        tone={changeTone(boxes.reports_last_24h.change_pct)}
      />
      <Stat
        index={3}
        label="Aktif adapter"
        value={boxes.active_adapter.name || "—"}
        hint={`şema uyumu ${formatPercent(boxes.active_adapter.valid_rate)}, ${formatPointChange(
          boxes.active_adapter.change_points,
        )}`}
        tone={changeTone(boxes.active_adapter.change_points)}
      />
    </div>
  );
}

function StatsSkeleton() {
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
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="card p-4 space-y-3">
            <div className="skeleton h-4 w-40" />
            <div className="skeleton h-3 w-64 max-w-full" />
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

function formatPercent(value: number): string {
  return `%${Math.round(value * 100)}`;
}

function formatPointChange(value: number | null): string {
  if (value === null) return "— puan farkı";
  const rounded = Math.round(value * 10) / 10;
  const abs = Math.abs(rounded).toLocaleString("tr-TR", {
    maximumFractionDigits: 1,
  });
  if (rounded > 0) return `+${abs} puan farkı`;
  if (rounded < 0) return `-${abs} puan farkı`;
  return "0 puan farkı";
}
