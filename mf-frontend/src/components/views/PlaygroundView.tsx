"use client";

// Master view: LLM Playground. Loads Gemma (or another model) in the browser
// via WebLLM, runs a prompt, then records the run to the backend and shows the
// decision score returned by the rule-based scorer.

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { generate, loadModel, webgpuSupported, isModelLoaded } from "@/lib/webllm";
import type { ModelInfo, Run } from "@/lib/types";
import { ScoreCard } from "../ui/ScoreCard";

export function PlaygroundView() {
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
    api
      .models()
      .then((r) => {
        setModels(r.models);
        const rec = r.models.find((m) => m.recommended) ?? r.models[0];
        if (rec) setModelId(rec.id);
      })
      .catch(() => setError("Could not load model catalog from backend."));
  }, []);

  useEffect(() => {
    setLoaded(isModelLoaded(modelId));
  }, [modelId]);

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
    <div className="max-w-6xl mx-auto p-5 grid lg:grid-cols-2 gap-5">
      {/* Left: controls */}
      <div className="space-y-4">
        <div className="card p-4">
          <div className="flex items-center justify-between mb-3">
            <h2 className="font-semibold">Model</h2>
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

          <select
            className="input mb-3"
            value={modelId}
            onChange={(e) => {
              setModelId(e.target.value);
              setResult(null);
            }}
          >
            {models.map((m) => (
              <option key={m.id} value={m.id}>
                {m.label} · {m.size_hint}
                {m.recommended ? "  (recommended)" : ""}
              </option>
            ))}
          </select>

          {loaded ? (
            <div className="pill grade-A">● Model loaded</div>
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
              onClick={handleLoad}
              disabled={!supported || !modelId}
            >
              Load model into browser
            </button>
          )}
        </div>

        <div className="card p-4 space-y-3">
          <div>
            <label className="label">System prompt (optional)</label>
            <textarea
              className="input scrollbar-thin"
              rows={2}
              value={systemPrompt}
              onChange={(e) => setSystemPrompt(e.target.value)}
              placeholder="You are a concise Go instructor."
            />
          </div>
          <div>
            <label className="label">Prompt</label>
            <textarea
              className="input scrollbar-thin"
              rows={4}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label">Expected keywords (comma-sep)</label>
              <input
                className="input"
                value={keywords}
                onChange={(e) => setKeywords(e.target.value)}
                placeholder="term1, term2"
              />
            </div>
            <div>
              <label className="label">Temperature · {temperature.toFixed(1)}</label>
              <input
                type="range"
                min={0}
                max={1}
                step={0.1}
                value={temperature}
                onChange={(e) => setTemperature(parseFloat(e.target.value))}
                className="w-full mt-2 accent-[var(--accent)]"
              />
            </div>
          </div>
          <button
            className="btn btn-primary w-full"
            onClick={handleRun}
            disabled={running || !prompt.trim() || (!loaded && !isModelLoaded(modelId))}
          >
            {running ? "Generating…" : loaded ? "Run & score" : "Load model first"}
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
