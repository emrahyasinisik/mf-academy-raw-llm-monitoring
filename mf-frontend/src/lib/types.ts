// Shared API types — mirror the Go backend's JSON shapes.

export interface User {
  id: string;
  email: string;
  name: string;
  role: string;
  created_at: string;
  updated_at: string;
}

export interface TokenPair {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  user: User;
}

export interface Breakdown {
  completion: number;
  latency: number;
  efficiency: number;
  keywords: number;
  length: number;
}

export interface Score {
  id: string;
  run_id: string;
  score: number;
  grade: string;
  breakdown: Breakdown;
  rationale: string;
  created_at: string;
}

// Where a run executed. The same model id can run on the visitor's own GPU via
// WebGPU or on the self-hosted MLC server; their latencies measure different
// machines, so runs are never compared across targets without saying so.
export type RunTarget = "browser" | "server";

export interface Run {
  id: string;
  user_id: string;
  model: string;
  target: RunTarget;
  prompt: string;
  response: string;
  system_prompt: string;
  prompt_tokens: number;
  completion_tokens: number;
  latency_ms: number;
  temperature: number;
  expected_keywords: string[];
  metadata: Record<string, unknown>;
  created_at: string;
  score?: Score | null;
}

// RunSummary is what the history list returns. The large text fields (prompt,
// response, system_prompt) are not included — fetch a single run with
// api.getRun(id) when the detail view needs them.
export interface RunSummary {
  id: string;
  model: string;
  target: RunTarget;
  prompt_preview: string;
  prompt_tokens: number;
  completion_tokens: number;
  latency_ms: number;
  created_at: string;
  score?: Score | null;
}

// Cursor-paginated page of runs. Pass next_cursor back as `before` to fetch the
// following page; has_more reports whether one exists.
export interface ListResult {
  runs: RunSummary[];
  limit: number;
  next_cursor?: string;
  has_more: boolean;
}

export interface Metrics {
  total_runs: number;
  scored_runs: number;
  avg_score: number;
  // Averaged across every run wherever it ran, so it describes neither target
  // once both are in use. Prefer by_target for anything about performance.
  avg_latency_ms: number;
  avg_completion_tokens: number;
  runs_by_model: Record<string, number>;
  grade_distribution: Record<string, number>;
  by_target: Record<string, TargetMetrics>;
}

// One target's slice of the summary — figures that may be compared within a
// target but never across one.
export interface TargetMetrics {
  runs: number;
  avg_latency_ms: number;
  avg_completion_tokens: number;
  avg_score: number;
  tokens_per_second: number;
}

export interface ModelInfo {
  id: string;
  label: string;
  family: string;
  size_hint: string;
  recommended: boolean;
  // Where this model can run. Not every catalogue entry is compiled for the
  // server, so the UI must not offer a pairing the backend will reject.
  targets: RunTarget[];
}

export interface CreateRunPayload {
  model: string;
  prompt: string;
  response: string;
  system_prompt?: string;
  prompt_tokens?: number;
  completion_tokens?: number;
  latency_ms: number;
  temperature?: number;
  expected_keywords?: string[];
  metadata?: Record<string, unknown>;
  auto_score?: boolean;
}

// Asks the backend to run the model itself. The mirror of CreateRunPayload: the
// answer, the token counts and the latency are absent because the server
// produces them rather than accepting them.
export interface GenerateRunPayload {
  model: string;
  prompt: string;
  system_prompt?: string;
  temperature?: number;
  max_tokens?: number;
  expected_keywords?: string[];
  auto_score?: boolean;
}
