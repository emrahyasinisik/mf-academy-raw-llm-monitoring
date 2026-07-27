"use client";

// The Flutter screen generator: a brief goes in, one Dart widget comes out.
//
// Why this is a form and not a chat
// ---------------------------------
// The adapter was fine-tuned on single-turn rows whose user message had a fixed
// four-line shape (Ekran / Açıklama / Alanlar-İçerik / State). It has never seen
// a follow-up turn, and it has never seen a brief phrased any other way. A chat
// box would invite both, and both are off-distribution — the model would still
// answer, worse, with nothing on screen to say why.
//
// So the form is the product surface and it assembles the trained layout
// verbatim. Raw mode exists for the operator who needs to reproduce an exact
// prompt from the dataset, not as the everyday path.
//
// The generation itself goes through POST /llm/generate, which already runs a
// prompt on the self-hosted host and records the run. There is no new endpoint
// here on purpose: single-shot with a system prompt is exactly what that route
// does, and what this model needs.

import { useCallback, useEffect, useMemo, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { ModelInfo, Run } from "@/lib/types";
import {
  FLUTTER_SYSTEM_PROMPT,
  STATE_CHOICES,
  SUBJECT_KINDS,
  buildBrief,
  extractDart,
  lintDart,
  type Finding,
  type StateChoice,
  type SubjectKind,
} from "@/lib/flutterContract";
import { DartBlock } from "@/components/ui/DartBlock";

// 0.3 is what the trial run used. Higher wanders off the house style the whole
// fine-tune exists to enforce; 0 makes it repeat one layout for every brief.
const DEFAULT_TEMPERATURE = 0.3;

// Above the ~1200 tokens a screen took in testing, because a truncated widget is
// this model's first failure mode and the extra headroom costs only time.
const DEFAULT_MAX_TOKENS = 2048;

export function CodegenView() {
  const [kind, setKind] = useState<SubjectKind>("ekran");
  const [screen, setScreen] = useState("");
  const [description, setDescription] = useState("");
  const [fields, setFields] = useState("");
  const [state, setState] = useState<StateChoice>("cubit");
  const [raw, setRaw] = useState("");
  const [rawMode, setRawMode] = useState(false);

  const [models, setModels] = useState<ModelInfo[]>([]);
  const [model, setModel] = useState("");
  const [serverAvailable, setServerAvailable] = useState(true);
  const [temperature, setTemperature] = useState(DEFAULT_TEMPERATURE);

  const [run, setRun] = useState<Run | null>(null);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");

  // Only server-capable models can run this: the adapter is compiled into the
  // self-hosted build, and a browser target would silently serve a different
  // model that has never seen the house style.
  useEffect(() => {
    api
      .models()
      .then((res) => {
        const usable = res.models.filter((m) => m.targets.includes("server"));
        setModels(usable);
        setServerAvailable(res.server_inference && usable.length > 0);
        const preferred = usable.find((m) => m.recommended) ?? usable[0];
        if (preferred) setModel(preferred.id);
      })
      .catch(() => setServerAvailable(false));
  }, []);

  const subject = SUBJECT_KINDS.find((k) => k.id === kind) ?? SUBJECT_KINDS[0];

  const prompt = useMemo(
    () => (rawMode ? raw : buildBrief({ kind, screen, description, fields, state })),
    [rawMode, raw, kind, screen, description, fields, state],
  );

  const ready = rawMode ? raw.trim().length > 10 : screen.trim().length > 2;

  const generate = useCallback(async () => {
    if (!ready || running || !model) return;
    setRunning(true);
    setError("");
    setRun(null);
    try {
      const res = await api.generateRun({
        model,
        prompt,
        system_prompt: FLUTTER_SYSTEM_PROMPT,
        temperature,
        max_tokens: DEFAULT_MAX_TOKENS,
      });
      setRun(res);
    } catch (e) {
      setError(
        e instanceof ApiError
          ? e.message
          : "Model yanıt vermedi. Çıkarım sunucusu kapalı ya da tünel düşmüş olabilir.",
      );
    } finally {
      setRunning(false);
    }
  }, [ready, running, model, prompt, temperature]);

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-7xl mx-auto w-full px-4 py-6 grid gap-6 lg:grid-cols-[380px_1fr]">
        <div className="space-y-4">
          <div>
            <h2 className="text-lg font-semibold">Flutter Ekran Üreteci</h2>
            <p className="text-sm mt-1 leading-relaxed" style={{ color: "var(--text-dim)" }}>
              Ekranı tarif et, ev stilinde (flutter_bloc + Material 3) tek dosyalık bir
              widget üretilir.
            </p>
          </div>

          {!serverAvailable && (
            <div
              className="px-3 py-2.5 rounded-lg text-xs leading-relaxed"
              style={{ background: "var(--accent-soft)", color: "var(--warn)" }}
            >
              Sunucu çıkarımı şu an kapalı. Bu ekran yalnızca kendi GPU kutusunda derlenmiş
              modelle çalışır — tüneli ve <span className="mono">mlc</span> servisini kontrol et.
            </div>
          )}

          <div className="card p-4 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold" style={{ color: "var(--text-dim)" }}>
                BRIEF
              </span>
              <button
                className="btn btn-ghost !py-1 !px-2.5 !text-xs"
                onClick={() => setRawMode((v) => !v)}
                title="Eğitim setinden birebir bir prompt denemek için"
              >
                {rawMode ? "Forma dön" : "Ham prompt"}
              </button>
            </div>

            {rawMode ? (
              <textarea
                className="input mono !text-xs"
                rows={10}
                value={raw}
                onChange={(e) => setRaw(e.target.value)}
                placeholder={"Ekran: …\nAçıklama: …\nAlanlar/İçerik: …\nState: flutter_bloc (Cubit)."}
              />
            ) : (
              <>
                <Field label="Tür">
                  <div className="flex gap-1.5">
                    {SUBJECT_KINDS.map((k) => (
                      <button
                        key={k.id}
                        onClick={() => setKind(k.id)}
                        className="px-2.5 py-1.5 rounded-lg text-xs font-semibold transition-colors"
                        style={{
                          background: kind === k.id ? "var(--accent-soft)" : "transparent",
                          border: `1px solid ${kind === k.id ? "var(--accent)" : "var(--border)"}`,
                          color: kind === k.id ? "var(--text)" : "var(--text-dim)",
                        }}
                      >
                        {k.label}
                      </button>
                    ))}
                  </div>
                </Field>
                <Field label={subject.label} hint={subject.hint}>
                  <input
                    className="input"
                    value={screen}
                    onChange={(e) => setScreen(e.target.value)}
                    placeholder={kind === "bilesen" ? "Yıldızlı ürün kartı" : "Bildirim tercihleri"}
                  />
                </Field>
                <Field label="Açıklama" hint="ne yapıyor">
                  <textarea
                    className="input"
                    rows={2}
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder="Ses, titreşim ve rahatsız etmeyin anahtarları."
                  />
                </Field>
                <Field label="Alanlar / İçerik" hint="opsiyonel">
                  <textarea
                    className="input"
                    rows={2}
                    value={fields}
                    onChange={(e) => setFields(e.target.value)}
                    placeholder="3 SwitchListTile, altta Kaydet butonu"
                  />
                </Field>
                <Field label="State">
                  <div className="flex gap-1.5">
                    {STATE_CHOICES.map((s) => (
                      <button
                        key={s.id}
                        onClick={() => setState(s.id)}
                        className="px-2.5 py-1.5 rounded-lg text-xs font-semibold transition-colors"
                        style={{
                          background: state === s.id ? "var(--accent-soft)" : "transparent",
                          border: `1px solid ${state === s.id ? "var(--accent)" : "var(--border)"}`,
                          color: state === s.id ? "var(--text)" : "var(--text-dim)",
                        }}
                      >
                        {s.label}
                      </button>
                    ))}
                  </div>
                </Field>
              </>
            )}
          </div>

          <div className="card p-4 space-y-3">
            <span className="text-xs font-semibold" style={{ color: "var(--text-dim)" }}>
              MODEL
            </span>
            <select
              className="input"
              value={model}
              onChange={(e) => setModel(e.target.value)}
              disabled={models.length === 0}
            >
              {models.length === 0 && <option value="">— sunucu modeli yok —</option>}
              {models.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.label} {m.size_hint && `· ${m.size_hint}`}
                </option>
              ))}
            </select>
            <label className="block">
              <span className="text-xs flex justify-between" style={{ color: "var(--text-dim)" }}>
                <span>Sıcaklık</span>
                <span className="mono">{temperature.toFixed(2)}</span>
              </span>
              <input
                type="range"
                min={0}
                max={1}
                step={0.05}
                value={temperature}
                onChange={(e) => setTemperature(parseFloat(e.target.value))}
                className="w-full mt-1.5"
              />
              <span className="text-xs" style={{ color: "var(--text-faint)" }}>
                0.30 eğitim testinde kullanılan değer; yukarısı ev stilinden sapar.
              </span>
            </label>
          </div>

          <button
            className="btn btn-primary w-full"
            onClick={generate}
            disabled={!ready || running || !model}
          >
            {running ? "Üretiyor…" : "Ekranı üret"}
          </button>

          {error && (
            <div
              className="px-3 py-2.5 rounded-lg text-xs"
              style={{ background: "var(--accent-soft)", color: "var(--bad)" }}
            >
              {error}
            </div>
          )}

          <details className="card p-3">
            <summary className="text-xs cursor-pointer" style={{ color: "var(--text-dim)" }}>
              Gönderilecek prompt
            </summary>
            <pre
              className="mono text-xs mt-2 whitespace-pre-wrap"
              style={{ color: "var(--text-faint)" }}
            >
              {prompt || "—"}
            </pre>
          </details>
        </div>

        <div className="min-w-0">
          {running && <Working />}
          {!running && !run && <Empty />}
          {!running && run && <Result run={run} state={state} />}
        </div>
      </div>
    </div>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="text-xs flex items-baseline gap-2 mb-1" style={{ color: "var(--text-dim)" }}>
        {label}
        {hint && (
          <span className="text-xs" style={{ color: "var(--text-faint)" }}>
            {hint}
          </span>
        )}
      </span>
      {children}
    </label>
  );
}

function Empty() {
  return (
    <div className="h-full min-h-[24rem] grid place-items-center text-center">
      <div className="max-w-sm">
        <div
          className="mx-auto w-12 h-12 rounded-xl grid place-items-center text-xl mb-4"
          style={{ background: "var(--accent-soft)", color: "var(--accent)" }}
        >
          {"</>"}
        </div>
        <p className="text-sm leading-relaxed" style={{ color: "var(--text-dim)" }}>
          Soldaki brief&apos;i doldur. Üretilen kod burada sözdizimi renklendirmesiyle ve ev
          stili denetimiyle birlikte görünecek.
        </p>
      </div>
    </div>
  );
}

function Working() {
  return (
    <div className="h-full min-h-[24rem] grid place-items-center">
      <div className="flex items-center gap-2 text-sm animate-pulse-soft" style={{ color: "var(--text-dim)" }}>
        <span
          className="w-6 h-6 rounded-lg grid place-items-center text-xs"
          style={{ background: "var(--accent-soft)", color: "var(--accent)" }}
        >
          {"</>"}
        </span>
        Widget üretiliyor — 6 GB kartta bu bir dakikayı bulabilir…
      </div>
    </div>
  );
}

function Result({ run, state }: { run: Run; state: StateChoice }) {
  const extracted = extractDart(run.response);
  const findings = useMemo(
    () => lintDart(extracted.code, state),
    [extracted.code, state],
  );
  const errors = findings.filter((f) => f.severity === "error");
  const tps =
    run.latency_ms > 0 ? (run.completion_tokens / (run.latency_ms / 1000)).toFixed(1) : "—";

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Verdict clean={errors.length === 0} truncated={extracted.truncated} />
        <span className="pill mono" style={{ color: "var(--text-dim)" }}>
          {run.completion_tokens} tok
        </span>
        <span className="pill mono" style={{ color: "var(--text-dim)" }}>
          {(run.latency_ms / 1000).toFixed(1)} s
        </span>
        <span className="pill mono" style={{ color: "var(--text-dim)" }}>
          {tps} tok/s
        </span>
        <span className="pill mono" style={{ color: "var(--text-faint)" }}>
          {run.model}
        </span>
      </div>

      {extracted.truncated && (
        <Notice tone="var(--bad)">
          Kod bloğu kapanmadan bitti — çıktı <span className="mono">max_tokens</span>{" "}
          sınırında kesilmiş. Aşağıdaki widget eksik; briefi küçült ya da sınırı yükselt.
        </Notice>
      )}

      {!extracted.fenced && (
        <Notice tone="var(--warn)">
          Model <span className="mono">```dart</span> bloğu olmadan yanıt verdi — sözleşmenin
          ihlali. Sistem prompt&apos;unun eğitimdekiyle birebir aynı olduğunu doğrula.
        </Notice>
      )}

      {extracted.stray && (
        <Notice tone="var(--warn)">
          Kod bloğunun dışında metin var; sözleşme yalnızca kod istiyor:{" "}
          <span className="mono">{extracted.stray.slice(0, 160)}</span>
        </Notice>
      )}

      {findings.length > 0 && <Findings findings={findings} />}

      <DartBlock
        code={extracted.code}
        highlightLines={findings.map((f) => f.line).filter((l): l is number => l !== null)}
      />
    </div>
  );
}

function Verdict({ clean, truncated }: { clean: boolean; truncated: boolean }) {
  const [label, color] = truncated
    ? ["KESİLMİŞ", "var(--bad)"]
    : clean
      ? ["EV STİLİNE UYGUN", "var(--good)"]
      : ["İHLAL VAR", "var(--bad)"];
  return (
    <span
      className="px-3 py-1.5 rounded-lg text-sm font-semibold"
      style={{ background: "var(--accent-soft)", color }}
    >
      {label}
    </span>
  );
}

function Notice({ tone, children }: { tone: string; children: React.ReactNode }) {
  return (
    <div
      className="px-3 py-2.5 rounded-lg text-xs leading-relaxed"
      style={{ background: "var(--accent-soft)", color: tone }}
    >
      {children}
    </div>
  );
}

// The checker earns its place by being the same list the dataset scanner uses.
// A finding here means the adapter produced something that would have been
// rejected from its own training set — which is the most useful signal available
// short of running the Dart analyzer.
function Findings({ findings }: { findings: Finding[] }) {
  return (
    <div className="card overflow-hidden">
      <div
        className="px-3 py-2 text-xs"
        style={{ color: "var(--text-faint)", background: "var(--bg-elev-2)" }}
      >
        {findings.length} bulgu · ev stili denetimi
      </div>
      {findings.map((f, i) => (
        <div
          key={i}
          className="px-3 py-2 flex gap-2.5 items-baseline text-xs"
          style={{ borderTop: "1px solid var(--border)" }}
        >
          <span
            className="pill shrink-0"
            style={{ color: f.severity === "error" ? "var(--bad)" : "var(--warn)" }}
          >
            {f.severity === "error" ? "hata" : "uyarı"}
          </span>
          <span className="mono shrink-0" style={{ color: "var(--text-faint)" }}>
            {f.line ? `L${f.line}` : "—"}
          </span>
          <span className="min-w-0">
            <span className="mono" style={{ color: "var(--text)" }}>
              {f.rule}
            </span>
            <span style={{ color: "var(--text-dim)" }}> — {f.detail}</span>
          </span>
        </div>
      ))}
    </div>
  );
}
