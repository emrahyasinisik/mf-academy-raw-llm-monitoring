"use client";

// Özet — kutular + kısa aktivite. Detaylı chart'lar /sirket/kullanim'da,
// tam feed /sirket/aktivite'de.

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { OrgStats } from "@/lib/types";
import { ActivityPanel } from "./ActivityPanel";
import { UsageBoxes } from "./UsagePanel";

export function OverviewPanel() {
  const [data, setData] = useState<OrgStats | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback((isCurrent: () => boolean = () => true) => {
    return api.org
      .stats("30d")
      .then((res) => {
        if (!isCurrent()) return;
        setData(res);
      })
      .catch((e: unknown) => {
        if (!isCurrent()) return;
        setError(e instanceof ApiError ? e.message : "Özet yüklenemedi.");
      })
      .finally(() => {
        if (!isCurrent()) return;
        setLoading(false);
      });
  }, []);

  useEffect(() => {
    let current = true;
    void load(() => current);
    return () => {
      current = false;
    };
  }, [load]);

  return (
    <section className="space-y-6">
      <div>
        <h3 className="font-display font-semibold">Özet</h3>
        <p className="text-xs mt-1" style={{ color: "var(--text-faint)" }}>
          Son 30 günün kutuları. Grafikler için{" "}
          <Link href="/sirket/kullanim" className="underline underline-offset-2">
            Kullanım
          </Link>
          , ekip için{" "}
          <Link href="/sirket/ekip" className="underline underline-offset-2">
            Ekip
          </Link>
          .
        </p>
      </div>

      {error && (
        <div className="notice notice-bad" role="alert">
          {error}
        </div>
      )}

      {loading && !data ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4" aria-label="Yükleniyor">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="card p-4 space-y-2.5">
              <div className="skeleton h-3 w-24" />
              <div className="skeleton h-8 w-20" />
            </div>
          ))}
        </div>
      ) : (
        data && <UsageBoxes data={data} />
      )}

      <div className="space-y-3">
        <div className="flex items-baseline justify-between gap-3">
          <h4 className="font-display font-semibold text-[0.95rem]">Son aktivite</h4>
          <Link
            href="/sirket/aktivite"
            className="text-xs underline underline-offset-2"
            style={{ color: "var(--text-faint)" }}
          >
            Tümü
          </Link>
        </div>
        <ActivityPanel limit={8} compact />
      </div>
    </section>
  );
}
