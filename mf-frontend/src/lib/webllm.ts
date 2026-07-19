// Browser-side LLM runner built on MLC-LLM's WebLLM. The model (Gemma by
// default) is downloaded once and executed on the GPU via WebGPU — nothing is
// sent to a server. This file must only ever run in the browser.

import type {
  MLCEngine,
  InitProgressReport,
  ChatCompletionMessageParam,
} from "@mlc-ai/web-llm";

export interface GenerateResult {
  content: string;
  promptTokens: number;
  completionTokens: number;
  latencyMs: number;
}

export type ProgressFn = (report: { text: string; progress: number }) => void;

// One cached engine per model id — reloading a model is expensive.
const engines = new Map<string, MLCEngine>();
let loadingModel: string | null = null;

/** True when the browser exposes WebGPU (required by WebLLM). */
export function webgpuSupported(): boolean {
  return typeof navigator !== "undefined" && "gpu" in navigator;
}

/** Load (or reuse) an engine for the given model id, reporting progress. */
export async function loadModel(
  modelId: string,
  onProgress?: ProgressFn,
): Promise<MLCEngine> {
  const existing = engines.get(modelId);
  if (existing) return existing;

  // Import lazily so the heavy WebLLM bundle is only fetched when needed and
  // never during server-side rendering.
  const { CreateMLCEngine } = await import("@mlc-ai/web-llm");

  loadingModel = modelId;
  const engine = await CreateMLCEngine(modelId, {
    initProgressCallback: (r: InitProgressReport) =>
      onProgress?.({ text: r.text, progress: r.progress }),
  });
  engines.set(modelId, engine);
  loadingModel = null;
  return engine;
}

export function isModelLoaded(modelId: string): boolean {
  return engines.has(modelId);
}

export function currentlyLoading(): string | null {
  return loadingModel;
}

/** Run a single prompt and return the answer plus timing/token telemetry. */
export async function generate(
  modelId: string,
  prompt: string,
  opts: { systemPrompt?: string; temperature?: number } = {},
  onProgress?: ProgressFn,
): Promise<GenerateResult> {
  const engine = await loadModel(modelId, onProgress);

  const messages: ChatCompletionMessageParam[] = [];
  if (opts.systemPrompt?.trim()) {
    messages.push({ role: "system", content: opts.systemPrompt });
  }
  messages.push({ role: "user", content: prompt });

  const started = performance.now();
  const reply = await engine.chat.completions.create({
    messages,
    temperature: opts.temperature ?? 0.7,
    stream: false,
  });
  const latencyMs = Math.round(performance.now() - started);

  const content = reply.choices[0]?.message?.content ?? "";
  return {
    content,
    promptTokens: reply.usage?.prompt_tokens ?? 0,
    completionTokens: reply.usage?.completion_tokens ?? 0,
    latencyMs,
  };
}
