"use client";

// Master view: LLM Playground. Two subviews — Model is where Gemma (or another
// WebLLM model) is chosen and pulled into the browser, Run is where a prompt is
// executed against it, recorded to the backend, and scored.

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { generate, loadModel, webgpuSupported, isModelLoaded } from "@/lib/webllm";
import type { ModelInfo, Run } from "@/lib/types";
import { ScoreCard } from "../ui/ScoreCard";
import { SubNav } from "../ui/SubNav";

type Sub = "run" | "model";

const SUBS = [
  { id: "run" as const, label: "Run & score" },
  { id: "model" as const, label: "Model runtime" },
];

const isSub = (s: string): s is Sub => SUBS.some((x) => x.id === s);

export function PlaygroundView({
  sub,
  onSub,
}: {
  sub: string;
  onSub: (s: string) => void;
}) {
  const active: Sub = isSub(sub) ? sub : "run";

  const [models, setModels] = useState<ModelInfo[]>([]);
  const [modelId, setModelId] = useState<string>("");
  const [loaded, setLoaded] = useState(false);
  const [progress, setProgress] = useState<{ text: string; pct: number } | null>(
    null,
  );

  const [systemPrompt, setSystemPrompt] = useState("");
  const [prompt, setPrompt] = useState("Explain what a goroutine is in one paragraph.");
  const [keywords, setKeywords] = useState("goroutine, channel, concurrency");
  const [temperature, setTemperature] = useState(0.7);

  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);
  const supported = webgpuSupported();

  useEffect(() => {
    let active = true;
    api
      .models()
      .then((r) => {
        if (!active) return;
        setModels(r.models);
        const rec = r.models.find((m) => m.recommended) ?? r.models[0];
        if (rec) {
          setModelId(rec.id);
          setLoaded(isModelLoaded(rec.id));
        }
      })
      .catch(() => {
        if (active) setError("Could not load model catalog from backend.");
      });
    return () => {
      active = false;
    };
  }, []);

  // Whether the engine is resident is owned by the WebLLM module, not by React,
  // so it is sampled whenever the selection changes rather than derived during
  // render — a render-time read would be memoised and miss the load completing.
  function pickModel(id: string) {
    setModelId(id);
    setLoaded(isModelLoaded(id));
    setResult(null);
  }

  async function handleLoad() {
    setError(null);
    setProgress({ text: "Preparing…", pct: 0 });
    try {
      await loadModel(modelId, (r) =>
        setProgress({ text: r.text, pct: Math.round(r.progress * 100) }),
      );
      setLoaded(true);
      setProgress(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load model");
      setProgress(null);
    }
  }

  async function handleRun() {
    setError(null);
    setRunning(true);
    setResult(null);
    try {
      const gen = await generate(
        modelId,
        prompt,
        { systemPrompt, temperature },
        (r) => setProgress({ text: r.text, pct: Math.round(r.progress * 100) }),
      );
      setProgress(null);
      const expected = keywords
        .split(",")
        .map((k) => k.trim())
        .filter(Boolean);

      // Record the run and auto-score it in a single backend call.
      const run = await api.createRun({
        model: modelId,
        prompt,
        response: gen.content,
        system_prompt: systemPrompt,
        prompt_tokens: gen.promptTokens,
        completion_tokens: gen.completionTokens,
        latency_ms: gen.latencyMs,
        temperature,
        expected_keywords: expected,
        auto_score: true,
      });
      setResult(run);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Run failed");
    } finally {
      setRunning(false);
    }
  }

  return (
    <div className="max-w-6xl mx-auto p-5 space-y-5">
      <SubNav items={SUBS} active={active} onSelect={onSub} />

      {active === "model" ? (
        <ModelSub
          models={models}
          modelId={modelId}
          loaded={loaded}
          progress={progress}
          supported={supported}
          error={error}
          onPick={pickModel}
          onLoad={handleLoad}
        />
      ) : (
        <RunSub
          modelId={modelId}
          loaded={loaded}
          supported={supported}
          progress={progress}
          running={running}
          result={result}
          error={error}
          systemPrompt={systemPrompt}
          prompt={prompt}
          keywords={keywords}
          temperature={temperature}
          onSystemPrompt={setSystemPrompt}
          onPrompt={setPrompt}
          onKeywords={setKeywords}
          onTemperature={setTemperature}
          onRun={handleRun}
          onGoToModel={() => onSub("model")}
        />
      )}
    </div>
  );
}

/* ---------------------------------------------------------------- subview 1 */

function ModelSub({
  models,
  modelId,
  loaded,
  progress,
  supported,
  error,
  onPick,
  onLoad,
}: {
  models: ModelInfo[];
  modelId: string;
  loaded: boolean;
  progress: { text: string; pct: number } | null;
  supported: boolean;
  error: string | null;
  onPick: (id: string) => void;
  onLoad: () => void;
}) {
  return (
    <div className="grid lg:grid-cols-2 gap-5">
      <div className="card p-4">
        <div className="flex items-center justify-between mb-3">
          <h2 className="font-semibold">Browser runtime</h2>
          <span
            className="pill"
            style={{
              color: supported ? "var(--good)" : "var(--bad)",
              borderColor: "var(--border)",
            }}
          >
            {supported ? "WebGPU ready" : "WebGPU unavailable"}
          </span>
        </div>

        {!supported && (
          <p className="text-xs mb-3" style={{ color: "var(--bad)" }}>
            This browser has no WebGPU. Use Chrome or Edge 111+ to run models
            in-browser.
          </p>
        )}

        <label className="label">Active model</label>
        <select
          className="input mb-3"
          value={modelId}
          onChange={(e) => onPick(e.target.value)}
        >
          {models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.label} · {m.size_hint}
              {m.recommended ? "  (recommended)" : ""}
            </option>
          ))}
        </select>

        {loaded ? (
          <div className="pill grade-A">● Model loaded — weights cached</div>
        ) : progress ? (
          <div>
            <div
              className="h-2 rounded-full overflow-hidden"
              style={{ background: "var(--bg-elev-2)" }}
            >
              <div
                className="h-full transition-all"
                style={{ width: `${progress.pct}%`, background: "var(--accent)" }}
              />
            </div>
            <p
              className="text-xs mt-2 mono truncate"
              style={{ color: "var(--text-dim)" }}
            >
              {progress.pct}% · {progress.text}
            </p>
          </div>
        ) : (
          <button
            className="btn btn-primary w-full"
            onClick={onLoad}
            disabled={!supported || !modelId}
          >
            Load model into browser
          </button>
        )}

        {error && (
          <p className="text-sm mt-3" style={{ color: "var(--bad)" }}>
            {error}
          </p>
        )}

        <p className="text-xs mt-4 leading-5" style={{ color: "var(--text-faint)" }}>
          Weights download once and are cached by the browser, so a reload does
          not re-fetch them. Inference runs fully on this device — no prompt or
          response is ever sent to a model provider.
        </p>
      </div>

      <div className="card p-4">
        <h2 className="font-semibold mb-3">Model catalog</h2>
        <div className="space-y-1">
          {models.length === 0 ? (
            <p className="text-sm" style={{ color: "var(--text-faint)" }}>
              Catalog unavailable.
            </p>
          ) : (
            models.map((m) => (
              <button
                key={m.id}
                onClick={() => onPick(m.id)}
                className="w-full text-left px-3 py-2.5 rounded-lg transition-colors"
                style={{
                  background:
                    m.id === modelId ? "var(--accent-soft)" : "transparent",
                }}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm truncate">{m.label}</span>
                  {m.recommended && (
                    <span className="pill grade-A">recommended</span>
                  )}
                </div>
                <div
                  className="text-xs mono mt-1 flex gap-3"
                  style={{ color: "var(--text-faint)" }}
                >
                  <span>{m.family}</span>
                  <span>{m.size_hint}</span>
                </div>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  );
}

/* ---------------------------------------------------------------- subview 2 */

function RunSub({
  modelId,
  loaded,
  supported,
  progress,
  running,
  result,
  error,
  systemPrompt,
  prompt,
  keywords,
  temperature,
  onSystemPrompt,
  onPrompt,
  onKeywords,
  onTemperature,
  onRun,
  onGoToModel,
}: {
  modelId: string;
  loaded: boolean;
  supported: boolean;
  progress: { text: string; pct: number } | null;
  running: boolean;
  result: Run | null;
  error: string | null;
  systemPrompt: string;
  prompt: string;
  keywords: string;
  temperature: number;
  onSystemPrompt: (v: string) => void;
  onPrompt: (v: string) => void;
  onKeywords: (v: string) => void;
  onTemperature: (v: number) => void;
  onRun: () => void;
  onGoToModel: () => void;
}) {
  const ready = loaded || isModelLoaded(modelId);

  return (
    <div className="grid lg:grid-cols-2 gap-5">
      {/* Left: controls */}
      <div className="space-y-4">
        {/* Compact runtime strip — the full controls live in the Model subview */}
        <div className="card p-3 flex items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="text-xs" style={{ color: "var(--text-dim)" }}>
              Model
            </div>
            <div className="text-sm mono truncate">{modelId || "—"}</div>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <span
              className="pill"
              style={{
                color: ready ? "var(--good)" : "var(--warn)",
                borderColor: "var(--border)",
              }}
            >
              {ready ? "● loaded" : "not loaded"}
            </span>
            <button
              className="btn btn-ghost !py-1 !px-2.5 text-xs"
              onClick={onGoToModel}
            >
              Configure →
            </button>
          </div>
        </div>

        <div className="card p-4 space-y-3">
          <div>
            <label className="label">System prompt (optional)</label>
            <textarea
              className="input scrollbar-thin"
              rows={2}
              value={systemPrompt}
              onChange={(e) => onSystemPrompt(e.target.value)}
              placeholder="You are a concise Go instructor."
            />
          </div>
          <div>
            <label className="label">Prompt</label>
            <textarea
              className="input scrollbar-thin"
              rows={4}
              value={prompt}
              onChange={(e) => onPrompt(e.target.value)}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label">Expected keywords (comma-sep)</label>
              <input
                className="input"
                value={keywords}
                onChange={(e) => onKeywords(e.target.value)}
                placeholder="term1, term2"
              />
            </div>
            <div>
              <label className="label">
                Temperature · {temperature.toFixed(1)}
              </label>
              <input
                type="range"
                min={0}
                max={1}
                step={0.1}
                value={temperature}
                onChange={(e) => onTemperature(parseFloat(e.target.value))}
                className="w-full mt-2 accent-[var(--accent)]"
              />
            </div>
          </div>
          <button
            className="btn btn-primary w-full"
            onClick={onRun}
            disabled={running || !prompt.trim() || !ready}
          >
            {running
              ? "Generating…"
              : ready
                ? "Run & score"
                : supported
                  ? "Load the model first"
                  : "WebGPU unavailable"}
          </button>
          {error && (
            <p className="text-sm" style={{ color: "var(--bad)" }}>
              {error}
            </p>
          )}
        </div>
      </div>

      {/* Right: output */}
      <div className="space-y-4">
        <div className="card p-4 min-h-[8rem]">
          <h2 className="font-semibold mb-3">Response</h2>
          {running && !result ? (
            <p
              className="text-sm animate-pulse-soft"
              style={{ color: "var(--text-dim)" }}
            >
              {progress ? `${progress.pct}% · ${progress.text}` : "Thinking…"}
            </p>
          ) : result ? (
            <p className="text-sm leading-6 whitespace-pre-wrap">
              {result.response || "(empty response)"}
            </p>
          ) : (
            <p className="text-sm" style={{ color: "var(--text-faint)" }}>
              Run a prompt to see Gemma&apos;s answer and its decision score.
            </p>
          )}
        </div>

        {result && (
          <>
            <div className="grid grid-cols-3 gap-3">
              <Stat label="Latency" value={`${result.latency_ms}ms`} />
              <Stat label="Compl. tokens" value={`${result.completion_tokens}`} />
              <Stat label="Prompt tokens" value={`${result.prompt_tokens}`} />
            </div>
            {result.score && <ScoreCard score={result.score} />}
          </>
        )}
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="card p-3 text-center">
      <div className="text-xl font-bold mono">{value}</div>
      <div className="text-xs mt-1" style={{ color: "var(--text-dim)" }}>
        {label}
      </div>
    </div>
  );
}
