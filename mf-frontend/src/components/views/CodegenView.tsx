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
//
// A note on what this screen no longer says. It used to narrate the hardware —
// how long a run takes on a particular card, which service to restart when it
// failed. That reads as somebody's workshop rather than a product, and it was
// also unreliable: the sentence was a guess, while the status rail above now
// counts the real elapsed seconds of the real run.

import { useCallback, useMemo, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { Run } from "@/lib/types";
import { useMachine } from "@/store/machine";
import {
  FLUTTER_SYSTEM_PROMPT,
  STATE_CHOICES,
  buildBrief,
  extractDart,
  lintDart,
  type Finding,
  type StateChoice,
} from "@/lib/flutterContract";
import { DartBlock } from "@/components/ui/DartBlock";

// 0.3 is what the trial run used. Higher wanders off the code standard the whole
// fine-tune exists to enforce; 0 makes it repeat one layout for every brief.
const DEFAULT_TEMPERATURE = 0.3;

// Above the ~1200 tokens a screen took in testing, because a truncated widget is
// this model's first failure mode and the extra headroom costs only time.
const DEFAULT_MAX_TOKENS = 2048;

export function CodegenView() {
  const { models, host, begin } = useMachine();

  const [screen, setScreen] = useState("");
  const [description, setDescription] = useState("");
  const [fields, setFields] = useState("");
  const [state, setState] = useState<StateChoice>("cubit");
  const [raw, setRaw] = useState("");
  const [rawMode, setRawMode] = useState(false);

  const [temperature, setTemperature] = useState(DEFAULT_TEMPERATURE);

  const [run, setRun] = useState<Run | null>(null);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");

  // The catalogue belongs to the machine store, so this view holds only the
  // operator's choice — and only once they have made one.
  //
  // Derived rather than defaulted through an effect. The list arrives after the
  // first render, and "copy the default into state when it loads" is the shape
  // that needs an effect, a guard against overwriting a deliberate pick, and a
  // cascading render to go with it. An empty `chosen` simply means "whatever the
  // catalogue recommends", which stays true as the catalogue changes.
  const [chosen, setChosen] = useState("");
  const fallback = useMemo(() => {
    const preferred = models.find((m) => m.recommended) ?? models[0];
    return preferred?.id ?? "";
  }, [models]);
  const model = chosen || fallback;

  const prompt = useMemo(
    () => (rawMode ? raw : buildBrief({ screen, description, fields, state })),
    [rawMode, raw, screen, description, fields, state],
  );

  const ready = rawMode ? raw.trim().length > 10 : screen.trim().length > 2;

  const generate = useCallback(async () => {
    if (!ready || running || !model) return;
    setRunning(true);
    setError("");
    setRun(null);
    // Hands the rail the job. Whatever happens below, `done` is what puts the
    // machine back to idle and records what the run cost.
    const done = begin("Ekran üretiliyor");
    try {
      const res = await api.generateRun({
        model,
        prompt,
        system_prompt: FLUTTER_SYSTEM_PROMPT,
        temperature,
        max_tokens: DEFAULT_MAX_TOKENS,
      });
      setRun(res);
      done(res);
    } catch (e) {
      done();
      setError(
        e instanceof ApiError
          ? e.message
          : "Üretim tamamlanamadı. Çıkarım sunucusu yanıt vermedi.",
      );
    } finally {
      setRunning(false);
    }
  }, [ready, running, model, prompt, temperature, begin]);

  const offline = host === "offline";

  return (
    <div className="h-full overflow-y-auto scrollbar-thin">
      <div className="max-w-[1400px] mx-auto w-full px-4 sm:px-5 py-6 grid gap-5 lg:grid-cols-[minmax(340px,400px)_1fr]">
        {/* ---- control column ---- */}
        <div className="space-y-4 min-w-0">
          <header>
            <h2 className="font-display text-xl font-semibold tracking-tight">
              Flutter Ekran Üreteci
            </h2>
            <p
              className="text-sm mt-1.5 leading-relaxed"
              style={{ color: "var(--text-dim)" }}
            >
              Ekranı tarif et; flutter_bloc ve Material 3 ile, projenin kod
              standardına uyan tek dosyalık bir widget üretilsin.
            </p>
          </header>

          {offline && (
            <div className="notice notice-warn">
              Çıkarım sunucusuna ulaşılamıyor. Üretim, sunucu çevrimiçi olduğunda
              yeniden açılır.
            </div>
          )}

          <section className="card p-4 space-y-3.5">
            <div className="flex items-center justify-between gap-2">
              <span className="eyebrow">Brief</span>
              <button
                className="btn btn-quiet btn-sm"
                onClick={() => setRawMode((v) => !v)}
                title="Eğitim setindeki bir prompt'u birebir çalıştır"
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
                aria-label="Ham prompt"
                placeholder={
                  "Ekran: …\nAçıklama: …\nAlanlar/İçerik: …\nState: flutter_bloc (Cubit)."
                }
              />
            ) : (
              <>
                <Field label="Ekran" hint="kısa ad">
                  <input
                    className="input"
                    value={screen}
                    onChange={(e) => setScreen(e.target.value)}
                    placeholder="Bildirim tercihleri"
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
                  <div
                    className="flex gap-1.5"
                    role="radiogroup"
                    aria-label="Durum yönetimi"
                  >
                    {STATE_CHOICES.map((s) => {
                      const on = state === s.id;
                      return (
                        <button
                          key={s.id}
                          role="radio"
                          aria-checked={on}
                          onClick={() => setState(s.id)}
                          className="px-2.5 py-1.5 rounded-[var(--r-xs)] text-xs font-semibold"
                          style={{
                            background: on ? "var(--brand-wash)" : "var(--panel-2)",
                            border: `1px solid ${on ? "var(--brand-line)" : "var(--line)"}`,
                            color: on ? "var(--brand)" : "var(--text-dim)",
                            transition:
                              "background var(--dur-1) var(--ease), border-color var(--dur-1) var(--ease), color var(--dur-1) var(--ease)",
                          }}
                        >
                          {s.label}
                        </button>
                      );
                    })}
                  </div>
                </Field>
              </>
            )}
          </section>

          <section className="card p-4 space-y-3.5">
            <span className="eyebrow">Model</span>
            <select
              className="input"
              value={model}
              onChange={(e) => setChosen(e.target.value)}
              disabled={models.length === 0}
              aria-label="Model"
            >
              {models.length === 0 && <option value="">— sunucu modeli yok —</option>}
              {models.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.label} {m.size_hint && `· ${m.size_hint}`}
                </option>
              ))}
            </select>

            <label className="block">
              <span
                className="text-xs flex justify-between items-baseline mb-1.5"
                style={{ color: "var(--text-dim)" }}
              >
                <span>Sıcaklık</span>
                <span className="mono num" style={{ color: "var(--text)" }}>
                  {temperature.toFixed(2)}
                </span>
              </span>
              <input
                type="range"
                min={0}
                max={1}
                step={0.05}
                value={temperature}
                onChange={(e) => setTemperature(parseFloat(e.target.value))}
              />
              <span
                className="text-xs block mt-1.5 leading-relaxed"
                style={{ color: "var(--text-faint)" }}
              >
                0.30 eğitimde kullanılan değer. Yükseldikçe çıktı kod standardından
                uzaklaşır.
              </span>
            </label>
          </section>

          <button
            className="btn btn-primary w-full"
            onClick={generate}
            disabled={!ready || running || !model}
          >
            {running ? "Üretiliyor…" : "Ekranı üret"}
          </button>

          {error && <div className="notice notice-bad">{error}</div>}

          <details className="card px-4 py-3">
            <summary
              className="text-xs cursor-pointer select-none"
              style={{ color: "var(--text-dim)" }}
            >
              Gönderilecek prompt
            </summary>
            <pre
              className="mono text-xs mt-2.5 whitespace-pre-wrap leading-relaxed"
              style={{ color: "var(--text-faint)" }}
            >
              {prompt || "—"}
            </pre>
          </details>
        </div>

        {/* ---- output column ----
            aria-live so the outcome is announced when it lands: the reader who
            started a minute-long generation is not necessarily still watching. */}
        <div className="min-w-0" aria-live="polite">
          {running ? <Working /> : run ? <Result run={run} state={state} /> : <Empty />}
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
      <span
        className="text-xs flex items-baseline gap-2 mb-1.5 font-medium"
        style={{ color: "var(--text-dim)" }}
      >
        {label}
        {hint && <span style={{ color: "var(--text-faint)" }}>{hint}</span>}
      </span>
      {children}
    </label>
  );
}

function Empty() {
  return (
    <div className="h-full min-h-[26rem] grid place-items-center text-center px-6">
      <div className="max-w-sm">
        <div
          className="mx-auto w-11 h-11 rounded-[var(--r-md)] grid place-items-center mb-4"
          style={{
            background: "var(--brand-wash)",
            border: "1px solid var(--brand-line)",
            color: "var(--brand)",
          }}
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 16 16"
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden
          >
            <path d="M5.5 4.5 2 8l3.5 3.5M10.5 4.5 14 8l-3.5 3.5" />
          </svg>
        </div>
        <h3 className="font-display font-semibold mb-1.5">Henüz çıktı yok</h3>
        <p className="text-sm leading-relaxed" style={{ color: "var(--text-dim)" }}>
          Soldaki brief&apos;i doldur ve ekranı üret. Kod burada sözdizimi
          renklendirmesi ve kod standardı denetimiyle birlikte görünür.
        </p>
      </div>
    </div>
  );
}

/**
 * The waiting state, shaped like the answer.
 *
 * A skeleton of the block that is coming, rather than a spinner and a sentence
 * guessing at the duration. The real elapsed time is on the status rail, which
 * is counting the actual request — so this only has to hold the space.
 */
function Working() {
  return (
    <div className="space-y-3" aria-label="Üretiliyor">
      <div className="flex gap-2">
        <div className="skeleton h-7 w-40" />
        <div className="skeleton h-7 w-20" />
        <div className="skeleton h-7 w-20" />
      </div>
      <div className="card overflow-hidden">
        <div
          className="px-3 py-2.5"
          style={{
            background: "var(--panel-2)",
            borderBottom: "1px solid var(--line)",
          }}
        >
          <div className="skeleton h-3 w-24" />
        </div>
        <div className="p-4 space-y-2.5">
          {[92, 74, 84, 55, 88, 66, 79, 48, 90, 71, 60, 83].map((w, i) => (
            <div
              key={i}
              className="skeleton h-3"
              style={{ width: `${w}%`, animationDelay: `${i * 70}ms` }}
            />
          ))}
        </div>
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
  const seconds = run.latency_ms / 1000;
  const tps = seconds > 0 ? (run.completion_tokens / seconds).toFixed(1) : "—";

  return (
    <div className="space-y-3">
      <Verdict
        clean={errors.length === 0}
        truncated={extracted.truncated}
        stats={[
          { label: "token", value: String(run.completion_tokens) },
          { label: "süre", value: `${seconds.toFixed(1)} s` },
          { label: "hız", value: `${tps} tok/s` },
        ]}
        model={run.model}
      />

      {extracted.truncated && (
        <div className="notice notice-bad">
          Kod bloğu kapanmadan bitti — çıktı <span className="mono">max_tokens</span>{" "}
          sınırında kesilmiş. Aşağıdaki widget eksik; brief&apos;i küçült ya da sınırı
          yükselt.
        </div>
      )}

      {!extracted.fenced && (
        <div className="notice notice-warn">
          Model <span className="mono">```dart</span> bloğu olmadan yanıt verdi —
          sözleşmenin ihlali. Sistem prompt&apos;unun eğitimdekiyle birebir aynı
          olduğunu doğrula.
        </div>
      )}

      {extracted.stray && (
        <div className="notice notice-warn">
          Kod bloğunun dışında metin var; sözleşme yalnızca kod istiyor:{" "}
          <span className="mono">{extracted.stray.slice(0, 160)}</span>
        </div>
      )}

      {findings.length > 0 && <Findings findings={findings} />}

      <DartBlock
        code={extracted.code}
        highlightLines={findings
          .map((f) => f.line)
          .filter((l): l is number => l !== null)}
      />
    </div>
  );
}

/**
 * The headline: did this output pass, and what did it cost.
 *
 * One band rather than a row of loose pills, with the tone carried by a thick
 * left edge. The verdict is the first thing to read on the screen and it earns
 * the emphasis — everything downstream is the evidence for it.
 */
function Verdict({
  clean,
  truncated,
  stats,
  model,
}: {
  clean: boolean;
  truncated: boolean;
  stats: { label: string; value: string }[];
  model: string;
}) {
  const [label, tone] = truncated
    ? ["Kesilmiş", "var(--bad)"]
    : clean
      ? ["Standarda uygun", "var(--ok)"]
      : ["İhlal var", "var(--bad)"];

  return (
    <div
      className="card view-in flex flex-wrap items-center gap-x-5 gap-y-3 p-4"
      style={{ borderLeft: `3px solid ${tone}` }}
    >
      <div className="flex items-center gap-2.5 min-w-0">
        <span className="lamp" style={{ color: tone }} />
        <span
          className="font-display font-semibold tracking-tight"
          style={{ color: tone }}
        >
          {label}
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 ml-auto">
        {stats.map((s) => (
          <div key={s.label} className="leading-tight">
            <div className="mono num text-sm" style={{ color: "var(--text)" }}>
              {s.value}
            </div>
            <div className="eyebrow" style={{ fontSize: "0.6rem" }}>
              {s.label}
            </div>
          </div>
        ))}
        <span
          className="mono text-xs truncate max-w-[220px]"
          style={{ color: "var(--text-faint)" }}
          title={model}
        >
          {model}
        </span>
      </div>
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
        className="px-3.5 py-2.5 eyebrow"
        style={{
          background: "var(--panel-2)",
          borderBottom: "1px solid var(--line)",
        }}
      >
        {findings.length} bulgu · kod standardı denetimi
      </div>
      {findings.map((f, i) => (
        <div
          key={i}
          className="item-in px-3.5 py-2.5 flex gap-3 items-baseline text-xs"
          style={{
            borderTop: i === 0 ? undefined : "1px solid var(--line)",
            // Capped so a long list finishes animating in well under a second.
            ["--i" as string]: Math.min(i, 8),
          }}
        >
          <span
            className={`pill shrink-0 ${f.severity === "error" ? "pill-bad" : "pill-warn"}`}
          >
            {f.severity === "error" ? "hata" : "uyarı"}
          </span>
          <span className="mono shrink-0 num" style={{ color: "var(--text-faint)" }}>
            {f.line ? `L${f.line}` : "—"}
          </span>
          <span className="min-w-0 leading-relaxed">
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
