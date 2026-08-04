"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { AdminLogEntry } from "@/lib/types";

export function LogsPanel() {
  const [entries, setEntries] = useState<AdminLogEntry[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.admin
      .logs(50)
      .then((r) => setEntries(r.entries))
      .catch((e: ApiError) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  if (error) return <div className="notice notice-bad">{error}</div>;

  return (
    <div className="card overflow-hidden">
      <div className="px-4 py-3.5" style={{ borderBottom: "1px solid var(--line)" }}>
        <h3 className="font-display font-semibold">Son çalıştırmalar</h3>
        {/* Said explicitly, because an operator reasonably expects a log to
            contain the request. This one carries timings and outcomes only:
            debugging latency does not require reading what people asked, and a
            log that holds it becomes a liability the moment it is exported. */}
        <p className="text-xs mt-1" style={{ color: "var(--text-faint)" }}>
          Prompt ve yanıt metinleri bilerek yok — burada yalnızca zamanlama ve
          sonuç var.
        </p>
      </div>

      {loading ? (
        <div className="p-4 space-y-2">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <div key={i} className="skeleton h-6 w-full" />
          ))}
        </div>
      ) : entries.length === 0 ? (
        <p className="text-xs p-4" style={{ color: "var(--text-faint)" }}>
          Henüz kayıtlı çalıştırma yok.
        </p>
      ) : (
        <div className="overflow-x-auto scrollbar-thin">
          <table className="w-full text-xs">
            <thead>
              <tr style={{ background: "var(--panel-2)" }}>
                {["kullanıcı", "model", "hedef", "token", "gecikme", "puan", "zaman"].map(
                  (h) => (
                    <th
                      key={h}
                      scope="col"
                      className="text-left px-4 py-2.5 eyebrow font-medium"
                      style={{ borderBottom: "1px solid var(--line)" }}
                    >
                      {h}
                    </th>
                  ),
                )}
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr
                  key={e.id}
                  style={{ borderTop: "1px solid var(--line)" }}
                  className="hover:bg-[var(--panel-2)] transition-colors"
                >
                  <td className="px-4 py-2.5 truncate max-w-[160px]">{e.user_email}</td>
                  <td
                    className="px-4 py-2.5 truncate max-w-[180px] mono"
                    style={{ color: "var(--text-dim)" }}
                  >
                    {e.model}
                  </td>
                  <td className="px-4 py-2.5">
                    <span className="pill">{e.target}</span>
                  </td>
                  <td
                    className="px-4 py-2.5 mono num"
                    style={{ color: "var(--text-dim)" }}
                  >
                    {e.prompt_tokens}→{e.completion_tokens}
                  </td>
                  <td className="px-4 py-2.5 mono num">
                    {(e.latency_ms / 1000).toFixed(1)}s
                  </td>
                  <td className="px-4 py-2.5 mono num">
                    {e.score === null ? "—" : e.score.toFixed(0)}
                  </td>
                  <td
                    className="px-4 py-2.5 mono"
                    style={{ color: "var(--text-faint)" }}
                  >
                    {new Date(e.created_at).toLocaleTimeString("tr-TR")}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
