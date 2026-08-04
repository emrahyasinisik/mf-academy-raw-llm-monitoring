// Shared API types — mirror the Go backend's JSON shapes.

export interface User {
  id: string;
  email: string;
  name: string;
  role: string;
  must_change_password: boolean;
  org_id: string | null;
  org_role: string;
  org_type: string;
  created_at: string;
  updated_at: string;
  terms_accepted_at: string | null;
  terms_version: string;
}

export interface LegalDocument {
  id: string;
  slug: string;
  title: string;
  version: string;
  body: string;
  requires_reconsent: boolean;
  is_draft: boolean;
  published_at?: string;
  published_by?: string;
  created_at: string;
}

export interface LegalListItem {
  slug: string;
  title: string;
  version: string;
  has_draft: boolean;
  published_at?: string;
  requires_reconsent: boolean;
}

export interface LegalSlugDetail {
  slug: string;
  draft: LegalDocument | null;
  history: LegalDocument[];
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
  redacted_at: string | null;
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
  redacted_at: string | null;
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
  redacted_at: string | null;
}

export interface AssessmentList {
  assessments: AssessmentSummary[];
  limit: number;
  next_cursor?: string;
  has_more: boolean;
}

/**
 * GET /analysis/domains/{slug}/prompt — modele giden prompt'un kendisi.
 *
 * Ekran bunu prompt'u göstermek için değil, **ölçmek** için okuyor: vaka
 * alanına kalan yer, pencereden sistem prompt'u düşüldükten sonra ne kalıyorsa
 * o, ve iki rubriğin sistem prompt'u arasında 565 karakter fark var.
 */
export interface AnalysisPrompt {
  domain: string;
  version: number;
  system_prompt: string;
  user_prompt_example: string;
  criteria: Criterion[];
  temperature: number;
}

/** GET /config'in `limits` bloğu. */
export interface AppLimits {
  max_prompt_tokens: number;
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
  /** Which engine serves generation: compiled, or the one that can hot-swap. */
  runtime: "mlc" | "hotswap";
  /**
   * The adapter the hot-swap runtime is *meant* to be applying. Recorded
   * because the engine forgets every scale when it restarts — without it an
   * activation would silently revert to the base model while the panel still
   * showed it as active.
   */
  active_gguf_adapter: string;
  retention_days: number;
  updated_at: string;
}

export interface AuditEntry {
  id: string;
  actor_id?: string;
  action: string;
  target: string;
  detail: Record<string, unknown>;
  created_at: string;
}

export interface AuditListResult {
  entries: AuditEntry[];
  total: number;
  page: number;
  limit: number;
}

/**
 * What activation returns. The settings write and the live swap are reported
 * separately because they can disagree: settings always succeeds, while the
 * swap fails if the runtime never loaded that adapter. One green tick over both
 * would claim an engine changed when it did not.
 */
export interface ActivationResult {
  settings: LLMSettings;
  hot_swapped: boolean;
  /** Measured duration of the swap. "Instant" is the claim; this is the proof. */
  swap_ms: number;
  note: string;
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
  /**
   * The GGUF file published to the hot-swap runtime. Independent of
   * mlc_model_id: which artefacts exist decides whether activating this build
   * is instant or needs a rebuild.
   */
  gguf_adapter: string;
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

export type StatsWindow = "30d" | "90d";

export interface StatBox {
  value: number;
  previous: number;
  change_pct: number | null;
}

/**
 * Yalnızca ad. Şema uyumu buradan çıkarıldı: oran penceredeki TÜM analizlerin
 * ortalaması, hangi adapter'ın ürettiğine bakmıyor. Adapter adının altında
 * gösterilince 90 günlük pencerede birkaç yapının ve temel modelin ortalaması
 * tek bir yapının başarısı gibi okunuyordu.
 */
export interface ActiveAdapterBox {
  name: string;
}

/** Pencerenin şema uyumu — her adapter ve temel model birlikte. */
export interface SchemaValidityBox {
  rate: number;
  previous_rate: number;
  change_points: number | null;
}

export interface StatsBoxes {
  total_users: StatBox;
  total_reports: StatBox;
  reports_last_24h: StatBox;
  active_adapter: ActiveAdapterBox;
  schema_validity: SchemaValidityBox;
}

export interface StatsDay {
  /** UTC gün başı, Unix saniye. */
  t: number;
  new_users: number;
  cumulative_users: number;
  assessments: number;
  schema_valid: number;
}

export interface StatsTargetSeries {
  target: string;
  points: MetricPoint[];
}

export interface CategoryCount {
  key: string;
  count: number;
}

export interface StatsFunnel {
  registered: number;
  consented: number;
  analyzed: number;
}

export interface CohortRow {
  week_start: number;
  size: number;
  week_2: number;
  week_4: number;
  mature_weeks: number;
}

/**
 * Aynı vakanın tekrar tekrar koşulduğunda ne kadar oynadığı — panelin
 * yayınlanabilir tek ölçümü.
 *
 * Kartın kendisi null olabilir: ölçülemeyen bir grup için sıfır fark basmak,
 * hiç yapılmamış bir ölçümü kusursuz ilan etmek olur.
 */
export interface ConsistencyCard {
  group: string;
  runs: number;
  created_at: string;
  total_spread: number;
  min_total: number;
  max_total: number;
  /** Ölçülecek kriter yoksa "" — yanındaki sapma da null olur. */
  volatile_criterion: string;
  /** Null = ölçülmedi. 0 basmak "mükemmel kararlı" okunur. */
  volatile_std_dev: number | null;
  /**
   * Bulguları silinmiş bacak sayısı. Puan, kapsam ve şema geçerliliği
   * redaksiyondan sağ çıkar, bulgular çıkmaz — ve kriter sapması bulgulardan
   * hesaplanır. 30 günlük saklama süpürmesinden sonra bu her grubun normal
   * hali, o yüzden eksik gözlem sayısı kartta yazıyor.
   */
  redacted_runs: number;
}

export interface AdminStats {
  window: StatsWindow;
  from: string;
  to: string;
  boxes: StatsBoxes;
  days: StatsDay[];
  org_types: CategoryCount[];
  runs_by_target: StatsTargetSeries[];
  funnel: StatsFunnel;
  cohorts: CohortRow[];
  consistency: ConsistencyCard | null;
}

/** Chart spans the server knows how to step. */
export type MetricsWindow = "1h" | "6h" | "24h";

export interface MetricPoint {
  /** Unix seconds. */
  t: number;
  v: number;
}

export interface MetricSeries {
  /** Empty on single-series panels — the title already names what is plotted. */
  label: string;
  points: MetricPoint[];
}

export interface MetricPanel {
  id: string;
  title: string;
  /** How the axis formats: "rps" | "seconds" | "count". */
  unit: string;
  help: string;
  series?: MetricSeries[];
  /**
   * Set when this panel's query failed. Per-panel rather than per-response so
   * one broken query costs one chart instead of the page.
   */
  error?: string;
}

export interface MetricsResponse {
  window: MetricsWindow;
  step_seconds: number;
  panels: MetricPanel[];
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

export type AccountType = "individual" | "company";
export type AccountStatus = "active" | "suspended";

export interface AccountSummary {
  id: string;
  name: string;
  type: AccountType;
  tax_id: string;
  seat_limit: number;
  status: AccountStatus;
  member_count: number;
  assessment_count: number;
  last_activity_at: string | null;
  created_at: string;
}

export interface AccountMember {
  id: string;
  email: string;
  name: string;
  org_role: string;
  created_at: string;
}

export interface AccountSession {
  id: string;
  user_agent: string;
  ip: string;
  created_at: string;
  expires_at: string;
}

export interface AccountDetail extends AccountSummary {
  members: AccountMember[];
  sessions: AccountSession[];
}

export interface AccountListResult {
  accounts: AccountSummary[];
  total: number;
  page: number;
  limit: number;
}

export interface CreateAccountRequest {
  type: AccountType;
  name: string;
  email: string;
  tax_id?: string;
  seat_limit?: number;
}

export interface CreateAccountResponse {
  account: AccountSummary;
  temporary_password: string;
  owner: AccountMember;
}

// ---- Company panel (/org) ----
//
// Scoped by the JWT's org_id on the server — the client never sends an org id.
// Assignable roles stop at admin|member; owner is minted only by platform admin.

export type OrgAssignableRole = "admin" | "member";

export interface OrgSummary {
  id: string;
  name: string;
  type: string;
  seat_limit: number;
  status: string;
  member_count: number;
}

export interface OrgMeResponse {
  org: OrgSummary;
  role: string;
}

export interface OrgMember {
  id: string;
  email: string;
  name: string;
  org_role: string;
  created_at: string;
  last_activity_at?: string | null;
}

export interface OrgMemberListResult {
  members: OrgMember[];
}

export interface CreateOrgMemberRequest {
  name: string;
  email: string;
  org_role: OrgAssignableRole;
}

export interface CreateOrgMemberResponse {
  member: OrgMember;
  temporary_password: string;
}

/** Org panel usage boxes — Postgres only, scoped to the actor's company. */
export interface OrgMemberSeatBox {
  count: number;
  seat_limit: number;
}

export interface OrgSchemaBox {
  rate: number;
}

export interface OrgStatsBoxes {
  members: OrgMemberSeatBox;
  reports_last_24h: StatBox;
  reports_window: StatBox;
  schema_validity: OrgSchemaBox;
}

export interface OrgDayPoint {
  t: number;
  v: number;
}

export interface OrgMemberAct {
  user_id: string;
  name: string;
  count: number;
  last_at?: string | null;
}

export interface OrgStats {
  window: StatsWindow;
  from: string;
  to: string;
  boxes: OrgStatsBoxes;
  assessments_per_day: OrgDayPoint[];
  schema_valid_per_day: OrgDayPoint[];
  runs_by_target: StatsTargetSeries[];
  member_activity: OrgMemberAct[];
}

export type OrgActivityKind =
  | "member.joined"
  | "analysis.completed"
  | "analysis.schema_invalid"
  | "session.login";

/** Metadata-only feed item — no case text, prompts, or findings. */
export interface OrgActivityItem {
  id: string;
  kind: OrgActivityKind | string;
  at: string;
  actor_name?: string;
  meta?: Record<string, unknown>;
}

export interface OrgActivityResponse {
  items: OrgActivityItem[];
}

// ---- DeepKwiki ----

export interface WikiDocument {
  id: string;
  slug: string;
  title: string;
  source_url: string;
  tags: string[];
  chunks: number;
  /** Only present when a single document is fetched; the listing omits it. */
  body?: string;
  created_at: string;
  updated_at: string;
}

/**
 * How a passage was found. The three differ enough in precision that the UI
 * labels them: "all" means every query term is in the passage, "fuzzy" means it
 * merely resembles the query and may be irrelevant.
 */
export type WikiMatch = "all" | "any" | "fuzzy";

export interface WikiHit {
  document_slug: string;
  title: string;
  source_url: string;
  ordinal: number;
  heading: string;
  /** Verbatim. This is what a claim is checked against. */
  body: string;
  /** The same passage with matches wrapped in « », for display only. */
  snippet: string;
  rank: number;
  matched: WikiMatch;
}

export interface WikiSource {
  n: number;
  document_slug: string;
  title: string;
  source_url: string;
  heading: string;
  body: string;
  /** Whether the answer actually referred to this passage. */
  cited: boolean;
}

export interface WikiAnswer {
  query: string;
  text: string;
  sources: WikiSource[];
  /** False when the answer cited nothing supplied — must not be shown as a result. */
  grounded: boolean;
  /** True when the corpus does not cover the question. Not a failure. */
  no_results: boolean;
  model: string;
  latency_ms: number;
}

// ---- Decision persona ----

export type DecisionRole = "user" | "assistant";

/**
 * One message in the persona conversation.
 *
 * The client still owns the transcript for the purposes of a turn — the whole
 * history goes up each time and the agent reads it from the request, not the
 * database. Persistence is a side effect of a turn, so this stays the wire
 * shape; `Conversation` below is what the server kept, and it is only read when
 * resuming a thread.
 */
export interface DecisionTurn {
  role: DecisionRole;
  content: string;
}

/** A numbered piece of evidence the persona was given, cited in the reply as [n]. */
export interface DecisionSource {
  n: number;
  title: string;
  url: string;
  kind: "web" | "wiki";
}

/** A tool the persona ran this turn, shown so a verdict traces to live research. */
export interface ResearchStep {
  tool: string;
  query: string;
  results: number;
  /**
   * Which implementation ran: "duckduckgo", "tavily", "deepkwiki". Optional
   * because threads recorded before this field existed are replayed from the
   * same table and carry no provider — a resumed conversation must not render
   * "undefined" for turns that predate the column's contents.
   */
  provider?: string;
  /**
   * The tool's failure, or absent when it answered — including when it answered
   * with nothing. Without this, `results: 0` means both "the subject has no
   * coverage" and "the tool never ran", and the screen showed the first while
   * the truth was the second.
   */
  error?: string;
}

/** One reply from the persona: the message plus what it researched to get there. */
export interface DecisionResult {
  reply: string;
  sources: DecisionSource[];
  research: ResearchStep[];
  model: string;
  /**
   * The thread this turn was recorded in — the server's, and new on the first
   * turn of a conversation.
   *
   * Empty when the turn could not be recorded. That is a signal, not an
   * omission: the answer above is real and must be shown, but storing this id
   * would later resume a thread the server does not have. Callers keep whatever
   * id they already held rather than overwriting it with "".
   */
  conversation_id: string;
}

// ---- Persona history ----

/**
 * One row of the history list. No message bodies: a transcript runs to tens of
 * kilobytes of evidence-laden replies, and the list renders a title, a badge and
 * a time. Read a full thread with api.conversation(id).
 */
export interface ConversationSummary {
  id: string;
  title: string;
  /** Linked rubric report, if the thread produced one. */
  assessment_id?: string | null;
  /** The decision this thread reached, or null while it is still researching. */
  verdict: string | null;
  verdict_score: number | null;
  /** Messages, counting both sides — so one exchange is two. */
  turns: number;
  last_turn_at: string;
  created_at: string;
}

/** A stored turn. Sources and research are empty on the user's own messages. */
export interface ConversationMessage {
  role: DecisionRole;
  content: string;
  sources: DecisionSource[];
  research: ResearchStep[];
  model: string;
}

/** A thread with its full transcript, for resuming it. */
export interface Conversation {
  id: string;
  title: string;
  /** Linked rubric report, if the thread produced one. */
  assessment_id?: string | null;
  verdict: string | null;
  verdict_score: number | null;
  messages: ConversationMessage[];
  last_turn_at: string;
  created_at: string;
}

/** Cursor-paginated page of threads. Pass next_cursor back as `before`. */
export interface ConversationList {
  conversations: ConversationSummary[];
  limit: number;
  next_cursor?: string;
  has_more: boolean;
}
