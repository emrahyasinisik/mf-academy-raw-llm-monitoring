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

// ---- Rubric analysis ----
//
// The product's core shapes. Two rules the UI must honour and the types are
// arranged to make hard to break:
//
//   * `score` is nullable per finding and `overall_score` is nullable overall.
//     Null is not zero — it means the case could not be assessed on that point.
//     Rendering a null as 0 would turn "the deck never mentions the team" into
//     "the team scored nothing", which is the exact conflation the backend's
//     scoring is built to avoid.
//
//   * `overall_score` is meaningless without `coverage`. 75 at 0.9 coverage and
//     75 at 0.3 coverage are different findings. Never display one alone.

export interface Criterion {
  key: string;
  label: string;
  description: string;
  guidance?: string;
  weight: number;
  scale_max: number;
}

export interface AnalysisDomain {
  id: string;
  slug: string;
  name: string;
  description: string;
  version: number;
  criteria: Criterion[];
  is_active: boolean;
}

export interface Finding {
  key: string;
  evidence_found: boolean;
  /** Null when the case gave nothing to rate. Never render as 0. */
  score: number | null;
  /** Verbatim quotes from the case, so a claim can be checked against source. */
  evidence: string[];
  rationale: string;
}

export interface Assessment {
  id: string;
  domain_id: string;
  domain_slug?: string;
  domain_name?: string;
  domain_version: number;
  /** The rubric as it stood when this ran; an old report is only readable against it. */
  criteria_snapshot: Criterion[];
  subject_title: string;
  subject?: string;
  findings: Finding[];
  /** Null when nothing could be assessed. Not zero. */
  overall_score: number | null;
  /** Share of rubric weight that had evidence. Always shown beside the score. */
  coverage: number;
  schema_valid: boolean;
  repair_attempts: number;
  model: string;
  target: RunTarget;
  latency_ms: number;
  prompt_tokens: number;
  completion_tokens: number;
  created_at: string;
}

export interface AssessmentSummary {
  id: string;
  domain_slug: string;
  domain_name: string;
  subject_title: string;
  overall_score: number | null;
  coverage: number;
  schema_valid: boolean;
  model: string;
  latency_ms: number;
  created_at: string;
}

export interface AssessmentList {
  assessments: AssessmentSummary[];
  limit: number;
  next_cursor?: string;
  has_more: boolean;
}

// ---- Admin ----

export interface LLMSettings {
  system_prompt: string;
  temperature: number;
  top_p: number;
  max_tokens: number;
  default_model: string;
  active_adapter_id: string | null;
  active_adapter_name: string;
  active_model_id: string;
  updated_at: string;
}

export interface ModelChoice {
  id: string;
  label: string;
  source: "builtin" | "adapter";
  available: boolean;
  status?: string;
  note?: string;
}

export interface Adapter {
  id: string;
  name: string;
  base_model: string;
  status: string;
  lora_rank: number;
  lora_alpha: number;
  target_modules: string[];
  mlc_model_id: string;
  notes: string;
  last_error: string;
  created_at: string;
  activated_at: string | null;
}

export interface MCPServer {
  id: string;
  slug: string;
  name: string;
  description: string;
  kind: "internal" | "external";
  url: string;
  transport: string;
  side: "frontend" | "backend" | "both";
  enabled: boolean;
}

export interface AdminOverview {
  total_users: number;
  total_runs: number;
  runs_last_24h: number;
  avg_latency_ms_24h: number;
  p95_latency_ms_24h: number;
  adapters_total: number;
  adapters_ready: number;
  assessments: number;
  assessments_last_24h: number;
  /** Operating health: a drop after activating an adapter is the rollback signal. */
  schema_valid_rate_24h: number;
  active_adapter_id: string | null;
}

export interface AdminLogEntry {
  id: string;
  user_email: string;
  model: string;
  target: RunTarget;
  prompt_tokens: number;
  completion_tokens: number;
  latency_ms: number;
  score: number | null;
  grade: string;
  created_at: string;
}
