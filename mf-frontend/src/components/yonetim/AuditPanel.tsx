"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { AuditEntry } from "@/lib/types";

const actionLabels: Record<string, string> = {
  "account.create": "Hesap açıldı",
  "account.suspend": "Hesap askıya alındı",
  "account.unsuspend": "Hesap açıldı (askı kalktı)",
  "account.delete": "Hesap silindi",
  "legal.publish": "Belge yayınlandı",
  "settings.update": "Ayar güncellendi",
  "adapter.activate": "Adapter etkinleştirildi",
  "adapter.deactivate": "Adapter kapatıldı",
};

export function AuditPanel() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const limit = 50;

  const load = useCallback(async (p: number) => {
    setLoading(true);
    setError("");
    try {
      const res = await api.admin.audit.list(p, limit);
      setEntries(res.entries);
      setTotal(res.total);
      setPage(res.page);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Denetim kaydı yüklenemedi.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(1);
  }, [load]);

  const pages = Math.max(1, Math.ceil(total / limit));

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg">Denetim kaydı</h1>
        <p className="mt-1 text-sm" style={{ color: "var(--muted)" }}>
          Kim ne yaptı — hesap, belge, ayar, adapter. İçerik veya kişisel veri
          yazılmaz.
        </p>
      </div>

      {error && (
        <p className="notice text-sm" role="alert">
          {error}
        </p>
      )}

      {loading ? (
        <p className="text-sm" style={{ color: "var(--muted)" }}>
          Yükleniyor…
        </p>
      ) : entries.length === 0 ? (
        <p className="text-sm" style={{ color: "var(--muted)" }}>
          Henüz kayıt yok.
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr style={{ color: "var(--muted)", textAlign: "left" }}>
                <th className="py-2 pr-3">Zaman</th>
                <th className="py-2 pr-3">İşlem</th>
                <th className="py-2 pr-3">Hedef</th>
                <th className="py-2">Ayrıntı</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr key={e.id} style={{ borderTop: "1px solid var(--line)" }}>
                  <td className="py-2 pr-3 whitespace-nowrap">
                    {new Date(e.created_at).toLocaleString("tr-TR")}
                  </td>
                  <td className="py-2 pr-3">
                    {actionLabels[e.action] ?? e.action}
                  </td>
                  <td className="py-2 pr-3 font-mono text-xs">{e.target || "—"}</td>
                  <td className="py-2 font-mono text-xs">
                    {JSON.stringify(e.detail ?? {})}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {pages > 1 && (
        <div className="flex gap-2">
          <button
            type="button"
            className="btn"
            disabled={page <= 1}
            onClick={() => void load(page - 1)}
          >
            Önceki
          </button>
          <span className="text-sm self-center">
            {page} / {pages}
          </span>
          <button
            type="button"
            className="btn"
            disabled={page >= pages}
            onClick={() => void load(page + 1)}
          >
            Sonraki
          </button>
        </div>
      )}
    </div>
  );
}
