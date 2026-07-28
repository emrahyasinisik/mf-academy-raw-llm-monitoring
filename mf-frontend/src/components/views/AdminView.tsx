"use client";

// The operator's cockpit. Four modules over the control-plane API.
//
// Gated twice, and the two gates do different jobs. The role check here decides
// what to render; the backend's RequireRole decides what is allowed. Only the
// second one is a security boundary — this one exists so a non-admin is not
// shown controls that would only ever return 403. Never rely on it for access:
// anything the browser knows, the browser's user can change.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { useAuth } from "@/store/auth";
import { Segmented } from "@/components/ui/Segmented";
import { RoleGate } from "@/components/ui/RoleGate";
import type {
  Adapter,
  AdminLogEntry,
  AdminOverview,
  LLMSettings,
  MCPServer,
  ModelChoice,
  ActivationResult,
} from "@/lib/types";

type Tab = "overview" | "model" | "mcp" | "logs";

const TABS: { id: Tab; label: string }[] = [
  { id: "overview", label: "Genel" },
  { id: "model", label: "Model & Ayarlar" },
  { id: "mcp", label: "MCP Sunucuları" },
  { id: "logs", label: "Log Monitörü" },
];

export function AdminView({
  sub,
  onNavigate,
}: {
  sub: string;
  onNavigate: (s: string) => void;
}) {
  const { user } = useAuth();
  const tab = (TABS.find((t) => t.id === sub)?.id ?? "overview") as Tab;

  if (user?.role !== "admin") {
    return (
      <RoleGate title="Bu bölüm yönetici hesabı gerektiriyor">
        Yönetici rolü API üzerinden verilmiyor. Sunucunun{" "}
        <code className="mono" style={{ color: "var(--text)" }}>
          ADMIN_EMAIL
        </code>{" "}
        değişkeninde adı geçen hesap, servis yeniden başladığında yükseltilir. Rol
        veren bir endpoint olsaydı, ileride çıkacak herhangi bir kimlik doğrulama
        açığı doğrudan tam yetkiye dönüşürdü.
      </RoleGate>
    );
  }

  return (
    <div className="max-w-6xl mx-auto p-4 sm:p-5 space-y-5">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h2 className="font-display text-xl font-semibold tracking-tight">Yönetim</h2>
        <Segmented
          items={TABS}
          active={tab}
          onSelect={(t) => onNavigate(t)}
          label="Yönetim bölümü"
          size="sm"
        />
      </div>

      {/* Keyed so switching tabs replays the entrance: these panels are torn
          down and rebuilt anyway, and without the key the new one would appear
          instantly while every other transition in the app animates. */}
      <div key={tab} className="view-in">
        {tab === "overview" && <OverviewPanel />}
        {tab === "model" && <ModelPanel />}
        {tab === "mcp" && <MCPPanel />}
        {tab === "logs" && <LogsPanel />}
      </div>
    </div>
  );
}

/** A single figure, its name, and what to do about it. */
function Stat({
  label,
  value,
  hint,
  tone,
  index = 0,
}: {
  label: string;
  value: string;
  hint?: string;
  tone?: string;
  index?: number;
}) {
  return (
    <div className="card item-in p-4" style={{ ["--i" as string]: index }}>
      <div className="eyebrow">{label}</div>
      <div
        className="font-display text-3xl font-semibold num mt-1.5 tracking-tight"
        style={{ color: tone ?? "var(--text)" }}
      >
        {value}
      </div>
      {hint && (
        <div
          className="text-xs mt-2 leading-relaxed"
          style={{ color: "var(--text-dim)" }}
        >
          {hint}
        </div>
      )}
    </div>
  );
}

function OverviewPanel() {
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

function ModelPanel() {
  const [models, setModels] = useState<ModelChoice[]>([]);
  const [effective, setEffective] = useState("");
  const [settings, setSettings] = useState<LLMSettings | null>(null);
  const [adapters, setAdapters] = useState<Adapter[]>([]);
  const [status, setStatus] = useState("");
  const [saving, setSaving] = useState(false);
  const [swap, setSwap] = useState<ActivationResult | null>(null);

  const reload = useCallback(() => {
    api.admin.models().then((r) => {
      setModels(r.models);
      setEffective(r.selected.effective);
    });
    api.admin.settings().then(setSettings);
    api.admin.adapters().then((r) => setAdapters(r.adapters));
  }, []);

  // Keeps the swap outcome and refreshes the panel in one step. The result is
  // held in state rather than folded into `status`, because it is not a
  // transient toast — an operator comparing adapters reads the millisecond
  // figure, and it has to stay on screen while they do.
  const announce = useCallback(
    (res: ActivationResult) => {
      setSwap(res);
      reload();
    },
    [reload],
  );

  useEffect(reload, [reload]);

  async function save(patch: Partial<LLMSettings>) {
    setSaving(true);
    setStatus("");
    try {
      const next = await api.admin.updateSettings(patch);
      setSettings(next);
      reload();
      setStatus("Kaydedildi.");
    } catch (e) {
      setStatus(e instanceof ApiError ? e.message : "Kaydedilemedi.");
    } finally {
      setSaving(false);
    }
  }

  if (!settings) {
    return (
      <div className="space-y-4">
        {[0, 1, 2].map((i) => (
          <div key={i} className="card p-4 space-y-3">
            <div className="skeleton h-4 w-32" />
            <div className="skeleton h-9 w-full" />
            <div className="skeleton h-3 w-3/4" />
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <section className="card p-4 space-y-3">
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <h3 className="font-display font-semibold">Model seçimi</h3>
          <span className="text-xs" style={{ color: "var(--text-faint)" }}>
            şu an çalışan:{" "}
            <strong className="mono" style={{ color: "var(--text)" }}>
              {effective}
            </strong>
          </span>
        </div>

        <select
          className="input"
          value={settings.default_model}
          onChange={(e) => save({ default_model: e.target.value })}
          disabled={saving}
          aria-label="Varsayılan model"
        >
          {/* Empty means "no explicit choice", which falls through to the active
              adapter. Spelled out rather than left blank so the operator can
              tell it apart from a control that failed to load. */}
          <option value="">(seçim yok — aktif adapter, yoksa varsayılan)</option>
          {models.map((m) => (
            <option key={m.id} value={m.id} disabled={!m.available}>
              {m.label}
              {m.note ? ` — ${m.note}` : ""}
            </option>
          ))}
        </select>

        <p className="text-xs leading-relaxed" style={{ color: "var(--text-dim)" }}>
          Buradaki seçim aktif adapter&apos;ın{" "}
          <strong style={{ color: "var(--text)" }}>üstünde</strong> çalışır. Bir
          adapter varken temel modeli bilerek servis etmek için kullanılır — bir
          değerlendirme sırasında normal olan durum budur.
        </p>
      </section>

      <section className="card p-4">
        <h3 className="font-display font-semibold mb-3">Adapter build&apos;leri</h3>
        {adapters.length === 0 ? (
          <p className="text-xs" style={{ color: "var(--text-faint)" }}>
            Kayıtlı build yok. Pipeline için mf-inference/peft/README.md.
          </p>
        ) : (
          <ul className="space-y-1.5">
            {adapters.map((a, i) => (
              <li
                key={a.id}
                className="item-in flex items-center gap-3 text-xs rounded-[var(--r-sm)] px-2.5 py-2"
                style={{
                  background: "var(--panel-2)",
                  border: "1px solid var(--line)",
                  ["--i" as string]: i,
                }}
              >
                <span className="flex-1 min-w-0">
                  <span className="block truncate" style={{ color: "var(--text)" }}>
                    {a.name}
                  </span>
                  <span className="mono" style={{ color: "var(--text-faint)" }}>
                    r={a.lora_rank} α={a.lora_alpha} · {a.status}
                    {a.last_error && ` · ${a.last_error}`}
                  </span>
                </span>
                {/* Which artefacts exist decides what activation means, so it
                    is shown on the row rather than discovered by clicking. */}
                <span className="flex gap-1 shrink-0">
                  {a.gguf_adapter && (
                    <span
                      className="pill pill-ok"
                      title={`GGUF: ${a.gguf_adapter} — anında geçiş`}
                    >
                      hot-swap
                    </span>
                  )}
                  {a.mlc_model_id && (
                    <span className="pill" title={`Derlenmiş model: ${a.mlc_model_id}`}>
                      derlenmiş
                    </span>
                  )}
                </span>
                {a.status === "active" ? (
                  <button
                    className="btn btn-ghost btn-sm"
                    onClick={() => api.admin.deactivateAdapter().then(announce)}
                  >
                    devre dışı bırak
                  </button>
                ) : (
                  <button
                    className="btn btn-ghost btn-sm"
                    disabled={a.status !== "ready"}
                    title={
                      a.status !== "ready"
                        ? "Yalnızca tamamlanmış bir build aktive edilebilir"
                        : ""
                    }
                    onClick={() =>
                      api.admin
                        .activateAdapter(a.id)
                        .then(announce)
                        .catch((e: ApiError) => setStatus(e.message))
                    }
                  >
                    aktive et
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
        {/* The measured outcome of the last activation. Reported rather than
            assumed, because the settings write and the live swap can disagree:
            a build published but not yet picked up by the runtime activates
            fine and swaps nothing. */}
        {swap && (
          <div
            className={`notice mt-3 view-in ${swap.hot_swapped ? "notice-ok" : "notice-warn"}`}
          >
            {swap.hot_swapped ? (
              <>
                <strong>
                  Canlı geçiş yapıldı — <span className="mono num">{swap.swap_ms} ms</span>.
                </strong>{" "}
                Yeniden başlatma yok, yeniden derleme yok.
              </>
            ) : (
              swap.note
            )}
          </div>
        )}

        <p className="text-xs mt-3 leading-relaxed" style={{ color: "var(--text-dim)" }}>
          İki yol var. <strong style={{ color: "var(--text)" }}>hot-swap</strong>{" "}
          etiketli bir build&apos;in ağırlıkları llama.cpp&apos;de zaten yüklüdür;
          aktive etmek bir ölçeği 0&apos;dan 1&apos;e çeker ve milisaniyeler sürer.{" "}
          <strong style={{ color: "var(--text)" }}>derlenmiş</strong> etiketli bir
          build&apos;de MLC kernelleri adapter var olmadan önce üretilmiştir,
          dolayısıyla çalışma zamanında LoRA yuvası yoktur — orada değişen,
          sunucudan hangi derlenmiş modelin isteneceğidir. Yeni bir GGUF yayınlamak
          yine de llamacpp konteynerinin yeniden başlatılmasını ister; yüklü
          adapter&apos;lar arasında geçiş istemez.
        </p>
      </section>

      <section className="card p-4 space-y-3">
        <h3 className="font-display font-semibold">Üretim parametreleri</h3>
        <label className="block">
          <span className="label">Sistem promptu</span>
          <textarea
            className="input mono !text-xs"
            rows={5}
            defaultValue={settings.system_prompt}
            onBlur={(e) =>
              e.target.value !== settings.system_prompt &&
              save({ system_prompt: e.target.value })
            }
          />
        </label>

        <div className="grid gap-3 sm:grid-cols-3">
          {(["temperature", "top_p", "max_tokens"] as const).map((k) => (
            <label key={k}>
              <span className="label mono">{k}</span>
              <input
                className="input mono num"
                type="number"
                step={k === "max_tokens" ? 1 : 0.05}
                defaultValue={settings[k]}
                onBlur={(e) => {
                  const v = Number(e.target.value);
                  if (v !== settings[k]) save({ [k]: v } as Partial<LLMSettings>);
                }}
              />
            </label>
          ))}
        </div>

        <p className="text-xs leading-relaxed" style={{ color: "var(--text-dim)" }}>
          Bu ayarlar sohbet yolunu etkiler. Analiz yolunun sıcaklığı bilerek
          sabitlenmiştir: rubrik doldurmak çıkarım işidir ve örnekleme çeşitliliği,
          ürünün üzerine kurulduğu tutarlılığın doğrudan karşıtıdır.
        </p>

        {status && (
          <p className="text-xs mono" style={{ color: "var(--text-dim)" }}>
            {status}
          </p>
        )}
      </section>
    </div>
  );
}

function MCPPanel() {
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

function LogsPanel() {
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
