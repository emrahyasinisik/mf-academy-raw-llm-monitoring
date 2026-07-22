// Typed API client for the Go backend. Handles token storage and a single
// transparent refresh-on-401 retry so callers never deal with token plumbing.

import type {
  TokenPair,
  User,
  Run,
  ListResult,
  Metrics,
  ModelInfo,
  CreateRunPayload,
  GenerateRunPayload,
  Score,
} from "./types";

const BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const ACCESS_KEY = "mf_access";
const REFRESH_KEY = "mf_refresh";

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

function getAccess(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(ACCESS_KEY);
}
function getRefresh(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(REFRESH_KEY);
}
export function setTokens(access: string, refresh: string) {
  localStorage.setItem(ACCESS_KEY, access);
  localStorage.setItem(REFRESH_KEY, refresh);
}
export function clearTokens() {
  localStorage.removeItem(ACCESS_KEY);
  localStorage.removeItem(REFRESH_KEY);
}
export function isAuthed(): boolean {
  return !!getAccess();
}

async function parseError(res: Response): Promise<ApiError> {
  let code = "error";
  let message = res.statusText;
  try {
    const body = await res.json();
    code = body.error ?? code;
    message = body.message ?? message;
  } catch {
    /* non-JSON error body */
  }
  return new ApiError(res.status, code, message);
}

// Core request with one automatic refresh retry on 401.
async function request<T>(
  path: string,
  init: RequestInit = {},
  retry = true,
): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  const access = getAccess();
  if (access) headers.set("Authorization", `Bearer ${access}`);

  const res = await fetch(`${BASE}${path}`, { ...init, headers });

  if (res.status === 401 && retry && getRefresh()) {
    const refreshed = await tryRefresh();
    if (refreshed) return request<T>(path, init, false);
  }

  if (!res.ok) throw await parseError(res);
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

async function tryRefresh(): Promise<boolean> {
  const refresh = getRefresh();
  if (!refresh) return false;
  try {
    const res = await fetch(`${BASE}/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refresh }),
    });
    if (!res.ok) {
      clearTokens();
      return false;
    }
    const data = (await res.json()) as TokenPair;
    setTokens(data.access_token, data.refresh_token);
    return true;
  } catch {
    return false;
  }
}

export const api = {
  // ---- config ----
  config: () => request<Record<string, unknown>>("/config"),

  // ---- auth ----
  async register(email: string, password: string, name: string) {
    const data = await request<TokenPair>("/auth/register", {
      method: "POST",
      body: JSON.stringify({ email, password, name }),
    });
    setTokens(data.access_token, data.refresh_token);
    return data;
  },
  async login(email: string, password: string) {
    const data = await request<TokenPair>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
    setTokens(data.access_token, data.refresh_token);
    return data;
  },
  async logout() {
    const refresh = getRefresh();
    try {
      await request("/auth/logout", {
        method: "POST",
        body: JSON.stringify({ refresh_token: refresh }),
      });
    } finally {
      clearTokens();
    }
  },
  me: () => request<User>("/auth/me"),
  sessions: () =>
    request<{ sessions: unknown[]; count: number }>("/auth/sessions"),

  // ---- llm ----
  // server_inference reports whether this deployment has an inference host
  // wired. It can be false — the host is a desktop machine — so the UI hides
  // the server option rather than offering a button that can only fail.
  models: () =>
    request<{ models: ModelInfo[]; server_inference: boolean }>("/llm/models"),
  // Records a run the browser already produced, with its own timings.
  createRun: (payload: CreateRunPayload) =>
    request<Run>("/llm/runs", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  // Has the backend run the model on the self-hosted host, then record it.
  // Slower to return than createRun by design: it waits on a GPU across a
  // tunnel, so callers should show progress rather than assume it is quick.
  generateRun: (payload: GenerateRunPayload) =>
    request<Run>("/llm/generate", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  // Cursor-paginated. Omit `before` for the newest page; pass the previous
  // page's next_cursor to continue. Returns summaries only — use getRun for a
  // run's full prompt and response.
  listRuns: (limit = 20, before?: string, model = "") => {
    const params = new URLSearchParams({ limit: String(limit) });
    if (before) params.set("before", before);
    if (model) params.set("model", model);
    return request<ListResult>(`/llm/runs?${params}`);
  },
  getRun: (id: string) => request<Run>(`/llm/runs/${id}`),
  deleteRun: (id: string) =>
    request<{ status: string }>(`/llm/runs/${id}`, { method: "DELETE" }),
  scoreRun: (id: string) =>
    request<Score>(`/llm/runs/${id}/score`, { method: "POST" }),
  metrics: () => request<Metrics>("/llm/metrics"),
};
