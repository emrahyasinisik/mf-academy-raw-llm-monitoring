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

export interface Score {
  id: string;
  run_id: string;
  score: number;
  grade: string;
  breakdown: Record<string, number>;
  rationale: string;
  created_at: string;
}

export interface Run {
  id: string;
  user_id: string;
  model: string;
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

export interface ListResult {
  runs: Run[];
  total: number;
  limit: number;
  offset: number;
}

export interface Metrics {
  total_runs: number;
  scored_runs: number;
  avg_score: number;
  avg_latency_ms: number;
  avg_completion_tokens: number;
  runs_by_model: Record<string, number>;
  grade_distribution: Record<string, number>;
}

export interface ModelInfo {
  id: string;
  label: string;
  family: string;
  size_hint: string;
  recommended: boolean;
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
