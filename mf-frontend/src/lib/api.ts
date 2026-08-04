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
  AnalysisDomain,
  AnalysisPrompt,
  Assessment,
  AssessmentList,
  LLMSettings,
  ModelChoice,
  Adapter,
  MCPServer,
  AdminOverview,
  AdminLogEntry,
  MetricsResponse,
  MetricsWindow,
  WikiDocument,
  WikiHit,
  WikiAnswer,
  ActivationResult,
  DecisionTurn,
  DecisionResult,
  Conversation,
  ConversationList,
  AccountListResult,
  AccountDetail,
  CreateAccountRequest,
  CreateAccountResponse,
  AccountStatus,
  AccountType,
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
  async register(
    email: string,
    password: string,
    name: string,
    acceptedTerms: boolean,
  ) {
    const data = await request<TokenPair>("/auth/register", {
      method: "POST",
      body: JSON.stringify({
        email,
        password,
        name,
        accepted_terms: acceptedTerms,
      }),
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
  acceptTerms: () =>
    request<void>("/auth/accept-terms", { method: "POST" }),
  changePassword: (current_password: string, new_password: string) =>
    request<TokenPair>("/auth/change-password", {
      method: "POST",
      body: JSON.stringify({ current_password, new_password }),
    }),

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

  // ---- analysis (the product) ----
  //
  // analysisRun is slow by design: it waits on a GPU across a tunnel and can
  // take a minute. Callers must show progress rather than a spinner that looks
  // stuck, and must not retry on timeout — a retry doubles the queue on a card
  // that serves one request at a time.
  analysisDomains: () =>
    request<{ domains: AnalysisDomain[]; count: number }>("/analysis/domains"),
  analysisRun: (payload: {
    domain: string;
    subject: string;
    subject_title?: string;
    model?: string;
  }) =>
    request<Assessment>("/analysis/run", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  analysisList: (limit = 20, domain = "", before?: string) => {
    const params = new URLSearchParams({ limit: String(limit) });
    if (domain) params.set("domain", domain);
    if (before) params.set("before", before);
    return request<AssessmentList>(`/analysis?${params}`);
  },
  analysisGet: (id: string) => request<Assessment>(`/analysis/${id}`),
  // 204 döner; request() zaten gövdesiz yanıtı undefined'a çeviriyor.
  analysisDelete: (id: string) =>
    request<void>(`/analysis/${id}`, { method: "DELETE" }),
  // Prompt'un kendisi, gösterilmek için değil ölçülmek için: vaka alanının
  // karakter bütçesi seçili rubriğin sistem prompt'u kadar küçülüyor.
  analysisPrompt: (slug: string) =>
    request<AnalysisPrompt>(`/analysis/domains/${slug}/prompt`),

  // ---- DeepKwiki ----
  //
  // wikiSearch is a database query and returns in milliseconds. wikiAsk waits
  // on the same GPU the analysis does — so the two are never called together
  // behind one spinner, and search results are shown as soon as they arrive
  // rather than held back until the answer is ready.
  wikiSearch: (q: string, limit = 8) =>
    request<{ query: string; hits: WikiHit[]; count: number }>(
      `/wiki/search?q=${encodeURIComponent(q)}&limit=${limit}`,
    ),
  wikiAsk: (query: string) =>
    request<WikiAnswer>("/wiki/ask", {
      method: "POST",
      body: JSON.stringify({ query }),
    }),
  wikiDocuments: () =>
    request<{ documents: WikiDocument[]; count: number }>("/wiki/documents"),
  wikiDocument: (slug: string) => request<WikiDocument>(`/wiki/documents/${slug}`),
  // Admin-only; a non-admin gets 403 and the view says so rather than hiding
  // the control with no explanation.
  wikiIngest: (payload: {
    slug: string;
    title: string;
    body: string;
    source_url?: string;
    tags?: string[];
  }) =>
    request<WikiDocument>("/wiki/documents", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  wikiDelete: (slug: string) =>
    request<void>(`/wiki/documents/${slug}`, { method: "DELETE" }),

  // ---- Decision persona ----
  //
  // The whole conversation goes up each turn; the agent is stateless between
  // turns and researches live from the transcript. Slow by design: it waits on
  // a web search and then the GPU across a tunnel.
  //
  // conversation_id names the thread to record the turn in, and is omitted to
  // open a new one. It does not change what the agent sees — the transcript
  // above is still the input — so a turn whose history write fails still
  // answers, and says so by returning an empty conversation_id.
  decisionChat: (messages: DecisionTurn[], conversationId?: string) =>
    request<DecisionResult>("/decision/chat", {
      method: "POST",
      body: JSON.stringify({
        messages,
        ...(conversationId ? { conversation_id: conversationId } : {}),
      }),
    }),

  // Cursor-paginated, ordered by last activity rather than by creation: a thread
  // resumed after a week belongs at the top of the list, not buried under ones
  // nobody has opened since.
  conversations: (limit = 20, before?: string) => {
    const params = new URLSearchParams({ limit: String(limit) });
    if (before) params.set("before", before);
    return request<ConversationList>(`/decision/conversations?${params}`);
  },
  conversation: (id: string) =>
    request<Conversation>(`/decision/conversations/${id}`),
  renameConversation: (id: string, title: string) =>
    request<void>(`/decision/conversations/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ title }),
    }),
  deleteConversation: (id: string) =>
    request<void>(`/decision/conversations/${id}`, { method: "DELETE" }),

  // Which MCP servers this browser is allowed to connect to. Answered by the
  // server rather than bundled, so switching one off actually switches it off.
  mcpServers: () =>
    request<{ servers: MCPServer[]; count: number }>("/mcp-servers"),

  // ---- admin (403 for anyone without the role) ----
  admin: {
    overview: () => request<AdminOverview>("/admin/overview"),
    // The window is the server's vocabulary, not a duration the client invents:
    // each one carries a step chosen to keep the payload near 120 points, and an
    // arbitrary span would have no step to go with it.
    metrics: (window: MetricsWindow) =>
      request<MetricsResponse>(`/admin/metrics?window=${window}`),
    logs: (limit = 50, target = "") => {
      const params = new URLSearchParams({ limit: String(limit) });
      if (target) params.set("target", target);
      return request<{ entries: AdminLogEntry[]; has_more: boolean }>(
        `/admin/logs?${params}`,
      );
    },
    settings: () => request<LLMSettings>("/admin/settings"),
    // Partial by design: the panel has separate forms over one row, and saving
    // one must not clobber the other with whatever the browser last rendered.
    updateSettings: (patch: Partial<Omit<LLMSettings, "updated_at">>) =>
      request<LLMSettings>("/admin/settings", {
        method: "PATCH",
        body: JSON.stringify(patch),
      }),
    models: () =>
      request<{
        models: ModelChoice[];
        selected: {
          default_model: string;
          active_adapter_id: string | null;
          active_adapter_name: string;
          effective: string;
        };
      }>("/admin/models"),
    adapters: () =>
      request<{ adapters: Adapter[]; count: number }>("/admin/adapters"),
    // Returns the swap outcome, not just the settings: whether a running
    // engine actually changed adapter, and how long it took. The panel shows
    // both because a settings write that took effect and a swap that did not
    // is a real, reachable state.
    activateAdapter: (id: string) =>
      request<ActivationResult>(`/admin/adapters/${id}/activate`, { method: "POST" }),
    deactivateAdapter: () =>
      request<ActivationResult>("/admin/adapters/deactivate", { method: "POST" }),
    mcpServers: () =>
      request<{ servers: MCPServer[]; count: number }>("/admin/mcp-servers"),
    createMcpServer: (payload: Partial<MCPServer>) =>
      request<MCPServer>("/admin/mcp-servers", {
        method: "POST",
        body: JSON.stringify(payload),
      }),
    updateMcpServer: (id: string, patch: Partial<MCPServer>) =>
      request<MCPServer>(`/admin/mcp-servers/${id}`, {
        method: "PATCH",
        body: JSON.stringify(patch),
      }),
    deleteMcpServer: (id: string) =>
      request<{ status: string }>(`/admin/mcp-servers/${id}`, {
        method: "DELETE",
      }),
    accounts: {
      list: (opts: {
        q?: string;
        type?: AccountType;
        status?: AccountStatus;
        page?: number;
        limit?: number;
      } = {}) => {
        const params = new URLSearchParams();
        if (opts.q) params.set("q", opts.q);
        if (opts.type) params.set("type", opts.type);
        if (opts.status) params.set("status", opts.status);
        if (opts.page) params.set("page", String(opts.page));
        if (opts.limit) params.set("limit", String(opts.limit));
        const qs = params.toString();
        return request<AccountListResult>(
          `/admin/accounts${qs ? `?${qs}` : ""}`,
        );
      },
      create: (payload: CreateAccountRequest) =>
        request<CreateAccountResponse>("/admin/accounts", {
          method: "POST",
          body: JSON.stringify(payload),
        }),
      get: (id: string) => request<AccountDetail>(`/admin/accounts/${id}`),
      suspend: (id: string) =>
        request<{ status: AccountStatus }>(`/admin/accounts/${id}/suspend`, {
          method: "POST",
        }),
      unsuspend: (id: string) =>
        request<{ status: AccountStatus }>(`/admin/accounts/${id}/unsuspend`, {
          method: "POST",
        }),
    },
  },
};
