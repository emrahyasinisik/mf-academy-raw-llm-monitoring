"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { MCPServer } from "@/lib/types";

export function MCPPanel() {
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [error, setError] = useState("");
  const [form, setForm] = useState({
    slug: "",
    name: "",
    url: "",
    side: "frontend" as MCPServer["side"],
  });

  const reload = useCallback(() => {
    api.admin
      .mcpServers()
      .then((r) => setServers(r.servers))
      .catch((e: ApiError) => setError(e.message));
  }, []);
  useEffect(reload, [reload]);

  async function add() {
    setError("");
    try {
      await api.admin.createMcpServer({ ...form, enabled: false });
      setForm({ slug: "", name: "", url: "", side: "frontend" });
      reload();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Eklenemedi.");
    }
  }

  return (
    <div className="space-y-4">
      <section className="card p-4">
        <h3 className="font-display font-semibold mb-3">Kayıtlı sunucular</h3>
        {servers.length === 0 ? (
          <p className="text-xs" style={{ color: "var(--text-faint)" }}>
            Kayıtlı sunucu yok.
          </p>
        ) : (
          <ul className="space-y-1.5">
            {servers.map((s, i) => (
              <li
                key={s.id}
                className="item-in flex items-center gap-3 text-xs rounded-[var(--r-sm)] px-2.5 py-2"
                style={{
                  background: "var(--panel-2)",
                  border: "1px solid var(--line)",
                  ["--i" as string]: i,
                }}
              >
                <span
                  className="lamp"
                  style={{ color: s.enabled ? "var(--ok)" : "var(--text-faint)" }}
                />
                <span className="flex-1 min-w-0">
                  <span className="flex items-center gap-2">
                    <span className="truncate" style={{ color: "var(--text)" }}>
                      {s.name}
                    </span>
                    {s.kind === "internal" && (
                      <span className="pill pill-brand">dahili</span>
                    )}
                  </span>
                  <span
                    className="block truncate mono"
                    style={{ color: "var(--text-faint)" }}
                  >
                    {s.url || "bu servisin kendi /mcp adresi"} · {s.side}
                  </span>
                </span>

                <label className="flex items-center gap-1.5 shrink-0 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={s.enabled}
                    onChange={(e) =>
                      api.admin
                        .updateMcpServer(s.id, { enabled: e.target.checked })
                        .then(reload)
                        .catch((err: ApiError) => setError(err.message))
                    }
                  />
                  <span style={{ color: "var(--text-faint)" }}>açık</span>
                </label>

                {s.kind === "external" && (
                  <button
                    className="btn btn-danger btn-sm"
                    onClick={() => api.admin.deleteMcpServer(s.id).then(reload)}
                  >
                    sil
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="card p-4 space-y-3">
        <h3 className="font-display font-semibold">Harici sunucu ekle</h3>
        <div className="grid gap-2 sm:grid-cols-2">
          <input
            className="input"
            placeholder="slug"
            aria-label="slug"
            value={form.slug}
            onChange={(e) => setForm({ ...form, slug: e.target.value })}
          />
          <input
            className="input"
            placeholder="ad"
            aria-label="ad"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
          <input
            className="input sm:col-span-2 mono !text-xs"
            placeholder="https://…/mcp"
            aria-label="MCP adresi"
            value={form.url}
            onChange={(e) => setForm({ ...form, url: e.target.value })}
          />
          <select
            className="input"
            aria-label="Taraf"
            value={form.side}
            onChange={(e) =>
              setForm({ ...form, side: e.target.value as MCPServer["side"] })
            }
          >
            <option value="frontend">frontend</option>
            <option value="backend">backend</option>
            <option value="both">ikisi</option>
          </select>
          <button
            className="btn btn-primary"
            onClick={add}
            disabled={!form.slug || !form.url}
          >
            Ekle
          </button>
        </div>
        <p className="text-xs leading-relaxed" style={{ color: "var(--text-dim)" }}>
          Frontend&apos;e açılan bir sunucu{" "}
          <strong style={{ color: "var(--text)" }}>https</strong> olmak zorunda
          (localhost hariç): https bir sayfadan düz http bağlantısını tarayıcı
          engeller, yani böyle bir kayıt seçim listesinde yalnızca hata üretirdi.
        </p>
        {error && <div className="notice notice-bad">{error}</div>}
      </section>
    </div>
  );
}
