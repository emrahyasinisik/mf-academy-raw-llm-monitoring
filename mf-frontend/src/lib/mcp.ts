// MCP client for the browser, plus a WebMCP bridge.
//
// Two directions, and they are easy to confuse:
//
//   * MCP client (below, `McpClient`) — this page *calls* an MCP server. It is
//     how the frontend reaches the analysis engine over the same protocol an
//     agent would use, rather than through the bespoke REST client in api.ts.
//
//   * WebMCP bridge (`publishTools`) — this page *offers* its own capabilities
//     to an agent running in the browser, via the proposed
//     `navigator.modelContext` API.
//
// A note on WebMCP's maturity, because it affects what can be relied on here:
// it is a proposal, not a shipped standard, and no browser implements
// `navigator.modelContext` natively today. Everything below degrades to a
// no-op when it is absent, and nothing in the app is allowed to depend on it —
// the same operations are always reachable through the ordinary client. Built
// this way so the surface exists and is testable the day an implementation
// arrives, without holding the product hostage to a proposal.

import type { AnalysisDomain, Assessment, MCPServer } from "./types";

const ACCESS_KEY = "mf_access";

/** JSON-RPC error carried out of a call, distinct from a tool that failed. */
export class McpProtocolError extends Error {
  code: number;
  constructor(code: number, message: string) {
    super(message);
    this.code = code;
    this.name = "McpProtocolError";
  }
}

/**
 * A tool ran and reported failure. Separate from McpProtocolError on purpose:
 * this one carries a message meant to be read and acted on ("the inference host
 * is switched off"), whereas a protocol error means the call itself was
 * malformed and the user can do nothing about it.
 */
export class McpToolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "McpToolError";
  }
}

interface ContentBlock {
  type: string;
  text?: string;
}

interface CallResult {
  content: ContentBlock[];
  isError?: boolean;
}

/** The protocol version this client asks for. The server may answer with another. */
export const PROTOCOL_VERSION = "2025-06-18";

export class McpClient {
  private readonly url: string;
  private nextId = 1;
  private initialized: Promise<string> | null = null;
  /** Reported by the server during initialize; may differ from what we asked. */
  serverProtocol = "";
  serverName = "";
  instructions = "";

  constructor(baseUrl: string) {
    this.url = baseUrl.replace(/\/$/, "");
  }

  private token(): string | null {
    if (typeof window === "undefined") return null;
    return localStorage.getItem(ACCESS_KEY);
  }

  private async rpc<T>(method: string, params?: unknown): Promise<T> {
    const id = this.nextId++;
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      // Sent even though this deployment does not require it: a server that
      // does version-check by header rejects a request without one, and the
      // failure ("unsupported protocol") reads like our bug rather than a
      // missing header.
      "MCP-Protocol-Version": this.serverProtocol || PROTOCOL_VERSION,
    };
    const t = this.token();
    if (t) headers["Authorization"] = `Bearer ${t}`;

    const res = await fetch(this.url, {
      method: "POST",
      headers,
      body: JSON.stringify({ jsonrpc: "2.0", id, method, params }),
    });

    if (!res.ok) {
      throw new McpProtocolError(res.status, `MCP transport failed: ${res.status}`);
    }

    const body = await res.json();
    if (body.error) {
      throw new McpProtocolError(body.error.code, body.error.message);
    }
    return body.result as T;
  }

  /**
   * Handshake. Memoised, so concurrent callers share one round trip rather than
   * racing to initialize the same session — the tool list and the first tool
   * call almost always start together.
   */
  async initialize(): Promise<string> {
    if (!this.initialized) {
      this.initialized = this.rpc<{
        protocolVersion: string;
        serverInfo: { name: string; version: string };
        instructions?: string;
      }>("initialize", {
        protocolVersion: PROTOCOL_VERSION,
        clientInfo: { name: "mf-frontend", version: "1.0" },
        capabilities: {},
      }).then((r) => {
        this.serverProtocol = r.protocolVersion;
        this.serverName = r.serverInfo?.name ?? "";
        this.instructions = r.instructions ?? "";
        return r.protocolVersion;
      });
      // A failed handshake must not be cached, or every later call replays the
      // same rejected promise and the connection can never recover.
      this.initialized.catch(() => {
        this.initialized = null;
      });
    }
    return this.initialized;
  }

  async listTools(): Promise<{ name: string; title?: string; description: string }[]> {
    await this.initialize();
    const r = await this.rpc<{ tools: { name: string; title?: string; description: string }[] }>(
      "tools/list",
    );
    return r.tools;
  }

  /**
   * Call a tool and return its text payload parsed as JSON.
   *
   * An `isError` result is raised as McpToolError rather than returned, because
   * every caller here would otherwise have to remember to check the flag — and
   * the one that forgets renders an error message as if it were a report.
   */
  async callTool<T>(name: string, args: Record<string, unknown> = {}): Promise<T> {
    await this.initialize();
    const result = await this.rpc<CallResult>("tools/call", { name, arguments: args });

    const text = result.content
      ?.filter((c) => c.type === "text")
      .map((c) => c.text ?? "")
      .join("\n")
      .trim();

    if (result.isError) {
      throw new McpToolError(text || "the tool reported a failure with no message");
    }
    if (!text) return undefined as T;

    try {
      return JSON.parse(text) as T;
    } catch {
      // A tool is allowed to answer in prose. Returning the raw string beats
      // throwing, since the caller asked for whatever the tool had to say.
      return text as unknown as T;
    }
  }

  // ---- typed wrappers over this deployment's own tools ----

  listDomains(): Promise<{ domains: AnalysisDomain[] }> {
    return this.callTool("list_analysis_domains");
  }

  analyzeCase(domain: string, subject: string, subjectTitle = ""): Promise<Assessment> {
    return this.callTool("analyze_case", {
      domain,
      subject,
      subject_title: subjectTitle,
    });
  }

  getReport(id: string): Promise<Assessment> {
    return this.callTool("get_report", { id });
  }
}

/**
 * Client for this deployment's own MCP endpoint.
 *
 * Derived from the REST base URL rather than configured separately: the two are
 * always the same origin here, and a second variable is a second thing to get
 * out of sync.
 */
export function localMcpClient(): McpClient {
  const base = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
  return new McpClient(`${base}/mcp`);
}

/**
 * Build a client for a server the operator registered.
 *
 * The internal one has no URL of its own — its address is this deployment — so
 * it is resolved rather than read from the row. Returns null for a transport a
 * browser cannot open: stdio means a local subprocess, which a web page has no
 * way to spawn, and offering it in a picker would produce an entry that only
 * ever fails.
 */
export function clientFor(server: MCPServer): McpClient | null {
  if (server.kind === "internal") return localMcpClient();
  if (server.transport === "stdio") return null;
  if (!server.url) return null;
  return new McpClient(server.url);
}

// ---- WebMCP: offering this page's capabilities to an in-browser agent ----

/** The shape of `navigator.modelContext` as proposed. */
interface ModelContext {
  registerTool?(tool: WebMcpTool): void;
  provideContext?(ctx: { tools: WebMcpTool[] }): void;
}

interface WebMcpTool {
  name: string;
  description: string;
  inputSchema: Record<string, unknown>;
  execute(args: Record<string, unknown>): Promise<{ content: ContentBlock[] }>;
}

declare global {
  interface Navigator {
    modelContext?: ModelContext;
  }
}

export function webMcpAvailable(): boolean {
  return typeof navigator !== "undefined" && !!navigator.modelContext;
}

/**
 * Offer this page's tools to an agent running in the browser.
 *
 * Returns whether anything was registered, so the UI can say "unavailable in
 * this browser" honestly instead of showing a control that silently does
 * nothing.
 *
 * Two shapes are attempted because the proposal has had both: an imperative
 * `registerTool` and a declarative `provideContext`. Trying each is a few lines
 * and avoids the surface working in one implementation and not another.
 */
export function publishTools(
  onAnalyze: (domain: string, subject: string) => Promise<Assessment>,
): boolean {
  if (!webMcpAvailable()) return false;
  const ctx = navigator.modelContext!;

  const tool: WebMcpTool = {
    name: "analyze_case_in_page",
    description:
      "Bu sayfadaki analiz motorunu kullanarak bir vakayı rubriğe göre puanlar. " +
      "Sonuçtaki overall_score'u ASLA coverage olmadan aktarma.",
    inputSchema: {
      type: "object",
      properties: {
        domain: { type: "string", description: "Rubrik slug'ı" },
        subject: { type: "string", description: "Analiz edilecek vaka metni" },
      },
      required: ["domain", "subject"],
    },
    async execute(args) {
      const report = await onAnalyze(
        String(args.domain ?? ""),
        String(args.subject ?? ""),
      );
      return {
        content: [{ type: "text", text: JSON.stringify(report, null, 2) }],
      };
    },
  };

  try {
    if (typeof ctx.provideContext === "function") {
      ctx.provideContext({ tools: [tool] });
      return true;
    }
    if (typeof ctx.registerTool === "function") {
      ctx.registerTool(tool);
      return true;
    }
  } catch {
    // An experimental API throwing is not a reason to break the page.
    return false;
  }
  return false;
}

// ---- React-facing store for the WebMCP registration ----
//
// Registering tools is a side effect on an external system, and whether it
// succeeded is state that lives outside React. That pairing is exactly what
// useSyncExternalStore exists for, and using it here avoids the alternative —
// calling setState from an effect body — which causes a cascading render and is
// flagged by react-hooks/set-state-in-effect.
//
// It is also the SSR-safe shape: getServerSnapshot returns false, so the server
// and the first client render agree, and the value only changes once the
// browser has actually been asked.

let registered = false;
let handler: ((domain: string, subject: string) => Promise<Assessment>) | null = null;
const listeners = new Set<() => void>();

function emit() {
  for (const l of listeners) l();
}

/**
 * Set the function the published tool will call. Kept outside the store so the
 * subscription itself has no dependencies and never re-registers — a tool
 * re-registered on every render would leave duplicates in an agent's tool list.
 */
export function setWebMcpHandler(
  fn: (domain: string, subject: string) => Promise<Assessment>,
): void {
  handler = fn;
}

/** subscribe for useSyncExternalStore. Registers on first subscription. */
export function subscribeWebMcp(onChange: () => void): () => void {
  listeners.add(onChange);
  if (!registered && handler) {
    registered = publishTools((d, s) => handler!(d, s));
    if (registered) emit();
  }
  return () => {
    listeners.delete(onChange);
  };
}

export function getWebMcpSnapshot(): boolean {
  return registered;
}

/** The server never has a browser API, so it never has tools published. */
export function getWebMcpServerSnapshot(): boolean {
  return false;
}
