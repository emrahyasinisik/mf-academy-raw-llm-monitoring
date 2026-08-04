"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type {
  Adapter,
  ActivationResult,
  LLMSettings,
  ModelChoice,
} from "@/lib/types";

export function ModelPanel() {
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
