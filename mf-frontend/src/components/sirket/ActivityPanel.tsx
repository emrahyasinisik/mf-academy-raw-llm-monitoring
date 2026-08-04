"use client";

// Aktivite feed — metadata only. Satırlar rapora derin link vermez: org admin
// başka üyenin vaka metnini buradan açmamalı.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { OrgActivityItem } from "@/lib/types";

const KIND_LABEL: Record<string, string> = {
  "member.joined": "Üye katıldı",
  "analysis.completed": "Analiz tamamlandı",
  "analysis.schema_invalid": "Şema geçersiz",
  "session.login": "Oturum açıldı",
};

export function ActivityPanel({
  limit = 50,
  compact = false,
}: {
  limit?: number;
  compact?: boolean;
}) {
  const [items, setItems] = useState<OrgActivityItem[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(
    (isCurrent: () => boolean = () => true) => {
      return api.org
        .activity({ limit })
        .then((res) => {
          if (!isCurrent()) return;
          setItems(res.items);
        })
        .catch((e: unknown) => {
          if (!isCurrent()) return;
          setError(e instanceof ApiError ? e.message : "Aktivite yüklenemedi.");
        })
        .finally(() => {
          if (!isCurrent()) return;
          setLoading(false);
        });
    },
    [limit],
  );

  useEffect(() => {
    let current = true;
    void load(() => current);
    return () => {
      current = false;
    };
  }, [load]);

  if (loading && items.length === 0) {
    return (
      <div className="space-y-2" aria-label="Yükleniyor">
        {[0, 1, 2].map((i) => (
          <div key={i} className="card p-3 space-y-2">
            <div className="skeleton h-3 w-40" />
            <div className="skeleton h-3 w-24" />
          </div>
        ))}
      </div>
    );
  }

  return (
    <section className="space-y-3">
      {!compact && (
        <div>
          <h3 className="font-display font-semibold">Aktivite</h3>
          <p className="text-xs mt-1" style={{ color: "var(--text-faint)" }}>
            Üye katılımı, analiz sonucu ve oturum — vaka metni yok, rapora link yok.
          </p>
        </div>
      )}

      {error && (
        <div className="notice notice-bad" role="alert">
          {error}
        </div>
      )}

      {items.length === 0 && !error ? (
        <p className="text-sm" style={{ color: "var(--text-dim)" }}>
          Henüz kayıtlı aktivite yok.
        </p>
      ) : (
        <ul className="space-y-2">
          {items.map((item, i) => (
            <li
              key={`${item.kind}-${item.id}`}
              className="card item-in p-3 flex flex-wrap items-baseline justify-between gap-2"
              style={{ ["--i" as string]: i }}
            >
              <div>
                <p className="text-sm font-medium">
                  {KIND_LABEL[item.kind] ?? item.kind}
                </p>
                <p className="text-xs mt-0.5" style={{ color: "var(--text-dim)" }}>
                  {item.actor_name || "—"}
                  {item.meta?.schema_valid === false
                    ? " · şema uymadı"
                    : item.meta?.schema_valid === true
                      ? " · şema uydu"
                      : null}
                </p>
              </div>
              <time
                className="text-xs mono"
                style={{ color: "var(--text-faint)" }}
                dateTime={item.at}
              >
                {formatWhen(item.at)}
              </time>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function formatWhen(iso: string): string {
  return new Date(iso).toLocaleString("tr-TR", {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}
