"use client";

// The investment persona: one screen, one conversation at a time, and a rail of
// the ones before it. The user names a market, brand, product or technology; the
// persona researches it live and works toward an investability verdict, asking a
// clarifying question when it must. After a verdict, the same screen can produce
// a rubric report into a side panel — research, decision and report share one
// workspace rather than three destinations.
//
// The transcript is both local state and server state, and the split matters: a
// turn is produced from what this component holds, and stored as a side effect.
// So the screen never waits on history to answer, and a failed write costs the
// record rather than the reply.

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api, ApiError } from "@/lib/api";
import type {
  AppLimits,
  Assessment,
  ConversationSummary,
  DecisionSource,
  DecisionTurn,
  ResearchStep,
} from "@/lib/types";
import { assemblePersonaCase, parseIntake } from "@/lib/personaCase";
import { isRedacted } from "@/lib/report";
import {
  clampReportPanelWidth,
  loadReportPanelWidth,
  saveReportPanelWidth,
} from "@/lib/reportPanelWidth";
import { caseBudgetChars } from "@/lib/rubric";
import { parseVerdict, stripVerdictLines } from "@/lib/verdict";
import { useMachine } from "@/store/machine";
import { RichText } from "@/components/ui/RichText";
import { HistoryPanel, type HistoryItem } from "@/components/ui/HistoryPanel";
import { CriterionContinuum } from "@/components/ui/CriterionContinuum";
import { IntakeFields } from "@/components/ui/IntakeFields";
import { ReportPanel } from "@/components/ui/ReportPanel";

// A message as this screen keeps it: the wire turn plus, for the persona's
// replies, what it researched to get there. The history sent to the server is
// just the role/content pair — the rest is display.
type ChatMessage =
  | { role: "user"; content: string }
  | {
      role: "assistant";
      content: string;
      sources: DecisionSource[];
      research: ResearchStep[];
      model: string;
    };

const VERDICT_TONE: Record<string, string> = {
  yatırılabilir: "var(--ok)",
  temkinli: "var(--warn)",
  yatırılamaz: "var(--bad)",
};

// Openers, so the empty screen is an invitation rather than a blank field. Each
// is a shape the persona handles well: a named company, a market, a technology.
const OPENERS = [
  "Acme AI — seed aşaması B2B SaaS",
  "Türkiye'de hızlı market teslimatı pazarı",
  "Katı hal batarya üreticileri",
];

/** When host config is unreachable — case assembly still needs a window. */
const WINDOW_TOKENS_FALLBACK = 8192;

function toneFor(label: string): string {
  const key = label.toLowerCase();
  for (const k of Object.keys(VERDICT_TONE)) {
    if (key.includes(k)) return VERDICT_TONE[k];
  }
  return "var(--brand)";
}

const HISTORY_PAGE = 20;

export function PersonaView() {
  const { begin } = useMachine();
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);

  const [topic, setTopic] = useState("");
  const [purpose, setPurpose] = useState("");
  const [panelOpen, setPanelOpen] = useState(false);
  const [panelWidth, setPanelWidth] = useState(420);
  const [report, setReport] = useState<Assessment | null>(null);
  const [reportLoading, setReportLoading] = useState(false);
  const [reportError, setReportError] = useState("");
  const [linkedAssessmentId, setLinkedAssessmentId] = useState<string | null>(
    null,
  );
  const [windowTokens, setWindowTokens] = useState(WINDOW_TOKENS_FALLBACK);

  // ---- history ----
  // The id of the thread on screen. Null for a conversation that has not had a
  // successful turn yet — the server assigns the id on the first one, so "new"
  // and "unsaved" are the same state here rather than two.
  const [threadId, setThreadId] = useState<string | null>(null);
  const [threads, setThreads] = useState<ConversationSummary[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [hasMore, setHasMore] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [historyError, setHistoryError] = useState("");

  // Promise-chained rather than async/await, and it deliberately sets no state
  // before the request goes out. The body of an async function runs
  // synchronously up to its first await, so a `setLoading(true)` at the top
  // fires inside whatever called it — which for the mount fetch is an effect,
  // and a synchronous setState there is the cascading render React warns about.
  // Callers that need a spinner raise it themselves, from the event that asked.
  const loadHistory = useCallback(
    (before?: string) =>
      api
        .conversations(HISTORY_PAGE, before)
        .then((res) => {
          setThreads((prev) =>
            before ? [...prev, ...res.conversations] : res.conversations,
          );
          setCursor(res.next_cursor);
          setHasMore(res.has_more);
          setHistoryError("");
        })
        .catch((e) =>
          setHistoryError(
            e instanceof ApiError ? e.message : "Geçmiş yüklenemedi.",
          ),
        )
        .finally(() => setHistoryLoading(false)),
    [],
  );

  useEffect(() => {
    void loadHistory();
  }, [loadHistory]);

  useEffect(() => {
    const sync = () => setPanelWidth(loadReportPanelWidth(window.innerWidth));
    sync();
    window.addEventListener("resize", sync);
    return () => window.removeEventListener("resize", sync);
  }, []);

  // Same source AnalizView uses — host window shrinks the case budget.
  useEffect(() => {
    api
      .config()
      .then((c) => {
        const limits = (c as { limits?: Partial<AppLimits> }).limits;
        if (limits?.max_prompt_tokens) setWindowTokens(limits.max_prompt_tokens);
      })
      .catch(() => {
        /* Fallback already set. */
      });
  }, []);

  // Keep the newest message in view as the conversation grows.
  useEffect(() => {
    scrollRef.current?.scrollTo({
      top: scrollRef.current.scrollHeight,
      behavior: "smooth",
    });
  }, [messages, running]);

  const clearReportLink = useCallback(async (id: string) => {
    try {
      await api.patchConversation(id, { assessment_id: null });
    } catch {
      /* Link clear is best-effort; panel already shows the missing report. */
    }
    setLinkedAssessmentId(null);
    setThreads((prev) =>
      prev.map((t) => (t.id === id ? { ...t, assessment_id: null } : t)),
    );
  }, []);

  const hydrateReport = useCallback(
    async (assessmentId: string, conversationId: string) => {
      setPanelOpen(true);
      setReportLoading(true);
      setReportError("");
      setReport(null);
      try {
        const a = await api.analysisGet(assessmentId);
        if (isRedacted(a)) {
          // Show the redacted shell, then drop the stale link so reopen stays honest.
          setReport(a);
          await clearReportLink(conversationId);
        } else {
          setReport(a);
        }
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) {
          setReportError("Bu rapor artık yok.");
          await clearReportLink(conversationId);
        } else {
          setReportError(
            e instanceof ApiError ? e.message : "Rapor yüklenemedi.",
          );
        }
      } finally {
        setReportLoading(false);
      }
    },
    [clearReportLink],
  );

  const produceReport = useCallback(async () => {
    if (!threadId || reportLoading) return;
    setPanelOpen(true);
    setReportLoading(true);
    setReportError("");
    const done = begin("Rapor üretiliyor");
    try {
      const prompt = await api.analysisPrompt("startup-investability");
      const budget = caseBudgetChars(windowTokens, prompt.system_prompt.length);
      const lastAssistant = [...messages]
        .reverse()
        .find((m) => m.role === "assistant");
      const body = assemblePersonaCase({
        topic:
          topic ||
          threads.find((t) => t.id === threadId)?.title ||
          "Vaka",
        purpose,
        userReplies: messages
          .filter((m) => m.role === "user")
          .map((m) => m.content),
        lastAssistantBody: lastAssistant
          ? stripVerdictLines(lastAssistant.content)
          : "",
        sources: (
          lastAssistant && lastAssistant.role === "assistant"
            ? lastAssistant.sources
            : []
        ).map((s) => ({
          title: s.title || s.url,
          url: s.url,
        })),
        budgetChars: budget,
      });
      const a = await api.analysisRun({
        domain: "startup-investability",
        subject_title: body.subject_title,
        subject: body.subject,
      });
      setReport(a);
      setLinkedAssessmentId(a.id);
      try {
        await api.patchConversation(threadId, { assessment_id: a.id });
        setThreads((prev) =>
          prev.map((t) =>
            t.id === threadId ? { ...t, assessment_id: a.id } : t,
          ),
        );
      } catch {
        setReportError("Rapor hazır; konuşmaya bağlanamadı.");
      }
    } catch (e) {
      setReportError(
        e instanceof ApiError ? e.message : "Rapor üretilemedi.",
      );
    } finally {
      setReportLoading(false);
      done();
    }
  }, [
    threadId,
    reportLoading,
    begin,
    windowTokens,
    messages,
    topic,
    purpose,
    threads,
  ]);

  const ask = useCallback(
    async (text: string) => {
      if (text.length < 2 || running || reportLoading) return;

      let content = text;
      if (messages.length === 0) {
        if (!topic.trim() || !purpose.trim()) {
          setError("Konu ve amaç zorunlu.");
          return;
        }
        content =
          `Konu: ${topic.trim()}\nAmaç: ${purpose.trim()}\n\n${text}`.trim();
      }

      const nextUser: ChatMessage = { role: "user", content };
      const history = [...messages, nextUser];
      setMessages(history);
      setInput("");
      setError("");
      setRunning(true);
      // The rail reports the persona too, so a user who switches to the
      // generator can still see that this turn is running.
      const done = begin("Araştırıyor");

      const wire: DecisionTurn[] = history.map((m) => ({
        role: m.role,
        content: m.content,
      }));
      try {
        const res = await api.decisionChat(wire, threadId ?? undefined);
        setMessages((prev) => [
          ...prev,
          {
            role: "assistant",
            content: res.reply,
            sources: res.sources ?? [],
            research: res.research ?? [],
            model: res.model,
          },
        ]);

        // An empty conversation_id means the turn was answered but not
        // recorded. Keeping whatever id we already had is deliberate: adopting
        // "" would detach the rest of this conversation from a thread the
        // server does have, turning one lost turn into a lost thread.
        if (res.conversation_id) {
          setThreadId(res.conversation_id);
          // Refresh rather than patch locally: the row carries a title the
          // server derived and a verdict it parsed, and re-deriving either here
          // would be a second implementation of both.
          void loadHistory();
        }
      } catch (e) {
        setError(
          e instanceof ApiError
            ? e.message
            : "Persona yanıt veremedi. Çıkarım sunucusu kapalı olabilir.",
        );
      } finally {
        // No run to report: /decision/chat answers with a reply and its sources,
        // not with the timings the rail's resting readout is built from.
        done();
        setRunning(false);
      }
    },
    [
      messages,
      running,
      reportLoading,
      topic,
      purpose,
      begin,
      threadId,
      loadHistory,
    ],
  );

  // Open a stored thread. Refused mid-turn: the reply in flight belongs to the
  // conversation being left, and letting it land in another one would file
  // research under the wrong subject.
  const openThread = useCallback(
    async (id: string) => {
      if (running || reportLoading || id === threadId) return;
      setError("");
      setReport(null);
      setReportError("");
      setPanelOpen(false);
      try {
        const c = await api.conversation(id);
        setMessages(
          c.messages.map((m) =>
            m.role === "user"
              ? { role: "user", content: m.content }
              : {
                  role: "assistant",
                  content: m.content,
                  sources: m.sources ?? [],
                  research: m.research ?? [],
                  model: m.model,
                },
          ),
        );
        setThreadId(c.id);

        const firstUser = c.messages.find((m) => m.role === "user");
        if (firstUser) {
          const intake = parseIntake(firstUser.content);
          setTopic(intake.topic);
          setPurpose(intake.purpose);
        } else {
          setTopic("");
          setPurpose("");
        }

        const aid = c.assessment_id ?? null;
        setLinkedAssessmentId(aid);
        if (aid) {
          void hydrateReport(aid, c.id);
        }
      } catch (e) {
        setError(
          e instanceof ApiError ? e.message : "Konuşma açılamadı.",
        );
      }
    },
    [running, reportLoading, threadId, hydrateReport],
  );

  const newThread = useCallback(() => {
    if (running || reportLoading) return;
    setMessages([]);
    setThreadId(null);
    setError("");
    setInput("");
    setTopic("");
    setPurpose("");
    setPanelOpen(false);
    setReport(null);
    setReportLoading(false);
    setReportError("");
    setLinkedAssessmentId(null);
  }, [running, reportLoading]);

  const renameThread = useCallback(async (id: string, title: string) => {
    // Optimistic: a rename is the user's own text going into a field they can
    // see. Waiting for a round trip to redraw it would make the rail feel like
    // it is arguing with them.
    setThreads((prev) =>
      prev.map((t) => (t.id === id ? { ...t, title } : t)),
    );
    try {
      await api.renameConversation(id, title);
    } catch {
      void loadHistory();
    }
  }, [loadHistory]);

  const deleteThread = useCallback(
    async (id: string) => {
      setThreads((prev) => prev.filter((t) => t.id !== id));
      if (id === threadId) newThread();
      try {
        await api.deleteConversation(id);
      } catch {
        void loadHistory();
      }
    },
    [threadId, newThread, loadHistory],
  );

  const pickOpener = useCallback((text: string) => {
    // Openers fill topic + composer; purpose stays the operator's job so the
    // first turn is not sent without an explicit aim.
    setTopic(text);
    setInput(text);
    setError("");
  }, []);

  const send = useCallback(() => ask(input.trim()), [ask, input]);

  const onKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  };

  const historyItems: HistoryItem[] = threads.map((t) => ({
    id: t.id,
    title: t.title,
    // The badge is the decision, when there is one. A thread still asking
    // clarifying questions gets none rather than a placeholder — "henüz karar
    // yok" in every row would be noise in the column that exists to be scanned.
    badge: t.verdict
      ? {
          text:
            t.verdict_score !== null
              ? `${t.verdict} · ${t.verdict_score}`
              : t.verdict,
          tone: toneFor(t.verdict),
        }
      : null,
    timestamp: t.last_turn_at,
    // Exchanges, not rows: the table stores a user turn and a reply separately,
    // and "6 mesaj" for three questions reads as twice the work it was.
    detail: `${Math.ceil(t.turns / 2)} tur`,
  }));

  const lastAssistantIndex = (() => {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].role === "assistant") return i;
    }
    return -1;
  })();

  const intakeReady = topic.trim().length > 0 && purpose.trim().length > 0;
  const sendDisabled =
    running ||
    reportLoading ||
    input.trim().length < 2 ||
    (messages.length === 0 && !intakeReady);

  return (
    <div className="h-full flex min-h-0">
      <HistoryPanel
        items={historyItems}
        activeId={threadId}
        onSelect={openThread}
        onNew={newThread}
        onRename={renameThread}
        onDelete={deleteThread}
        onLoadMore={() => {
          setHistoryLoading(true);
          void loadHistory(cursor);
        }}
        hasMore={hasMore}
        loading={historyLoading}
        error={historyError}
        newLabel="+ Yeni"
        emptyText="Henüz değerlendirme yok. İlk konusunu yazdığında burada birikmeye başlar."
      />

      <div
        className={`h-full flex-1 min-w-0 flex flex-col w-full px-4 sm:px-5 ${
          panelOpen ? "" : "max-w-3xl mx-auto"
        }`}
      >
        {messages.length === 0 ? (
          <Intro
            onPick={pickOpener}
            disabled={running || reportLoading}
            intake={
              <IntakeFields
                topic={topic}
                purpose={purpose}
                onTopic={setTopic}
                onPurpose={setPurpose}
                disabled={running || reportLoading}
              />
            }
          />
        ) : (
          <div
            ref={scrollRef}
            className={`flex-1 min-h-0 overflow-y-auto scrollbar-thin py-6 space-y-6 ${
              panelOpen ? "max-w-3xl" : ""
            }`}
            aria-live="polite"
          >
            {(topic || purpose) && (
              <div className="flex flex-wrap gap-2">
                {topic && (
                  <span className="pill" title="Konu">
                    Konu · {topic}
                  </span>
                )}
                {purpose && (
                  <span className="pill" title="Amaç">
                    Amaç · {purpose}
                  </span>
                )}
              </div>
            )}
            {messages.map((m, i) =>
              m.role === "user" ? (
                <UserBubble key={i} text={m.content} />
              ) : (
                <PersonaBubble
                  key={i}
                  msg={m}
                  showReportCta={i === lastAssistantIndex}
                  linkedAssessmentId={linkedAssessmentId}
                  reportLoaded={report !== null}
                  reportLoading={reportLoading}
                  onProduceReport={() => void produceReport()}
                  onShowReport={() => setPanelOpen(true)}
                />
              ),
            )}
            {running && <Thinking />}
          </div>
        )}

        {error && (
          <div className="notice notice-bad mb-3 view-in">{error}</div>
        )}

        {/* ---- composer ----
            The field, the send control and the reset control are one surface
            rather than three loose boxes: they act on the same thing. */}
        <div className="pb-5 pt-2">
          <div
            className="card p-2 flex items-end gap-2"
            style={{ background: "var(--panel-2)" }}
          >
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={onKey}
              rows={2}
              disabled={running || reportLoading}
              aria-label="Değerlendirilecek konu"
              placeholder="Bir pazar, marka, ürün veya teknoloji yaz…"
              className="flex-1 resize-none bg-transparent border-0 px-2 py-1.5 text-sm outline-none min-w-0"
              style={{ color: "var(--text)" }}
            />
            <div className="flex items-center gap-1.5 shrink-0">
              {/* No longer "Sıfırla": the conversation is kept now, so clearing
                  the screen starts a second one rather than discarding the first.
                  The old label promised a destruction that no longer happens. */}
              {messages.length > 0 && (
                <button
                  className="btn btn-quiet btn-sm lg:hidden"
                  onClick={newThread}
                  disabled={running || reportLoading}
                  title="Bu değerlendirmeyi bırak ve yenisine başla"
                >
                  Yeni
                </button>
              )}
              <button
                className="btn btn-primary btn-sm"
                onClick={send}
                disabled={sendDisabled}
              >
                {running
                  ? "Araştırıyor…"
                  : reportLoading
                    ? "Rapor…"
                    : "Gönder"}
              </button>
            </div>
          </div>
          <p className="text-xs mt-2 px-1" style={{ color: "var(--text-faint)" }}>
            Persona her turda canlı araştırır; okumasını kaynaklara bağlar.{" "}
            <span className="mono">Enter</span> gönderir,{" "}
            <span className="mono">Shift+Enter</span> satır atlar.
          </p>
        </div>
      </div>

      <ReportPanel
        open={panelOpen}
        width={panelWidth}
        onWidthChange={(px) => {
          const c = clampReportPanelWidth(px, window.innerWidth);
          setPanelWidth(c);
          saveReportPanelWidth(c);
        }}
        onClose={() => setPanelOpen(false)}
        assessment={report}
        loading={reportLoading}
        error={reportError}
        onRetry={() => void produceReport()}
      />
    </div>
  );
}

function Intro({
  onPick,
  disabled,
  intake,
}: {
  onPick: (text: string) => void;
  disabled: boolean;
  intake: ReactNode;
}) {
  return (
    <div className="flex-1 grid place-items-center text-center px-2">
      <div className="max-w-md view-in w-full">
        <div
          className="mx-auto w-11 h-11 rounded-[var(--r-md)] grid place-items-center mb-4"
          style={{
            background: "var(--brand-wash)",
            border: "1px solid var(--brand-line)",
            color: "var(--brand)",
          }}
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 16 16"
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            strokeLinejoin="round"
            aria-hidden
          >
            <path d="M8 1.75 9.7 6.3 14.25 8 9.7 9.7 8 14.25 6.3 9.7 1.75 8 6.3 6.3z" />
          </svg>
        </div>
        <h2 className="font-display text-xl font-bold tracking-tight mb-2">
          Yatırım Personası
        </h2>
        <p
          className="text-sm leading-relaxed mb-5"
          style={{ color: "var(--text-dim)" }}
        >
          Değerlendirmek istediğin pazarı, markayı, ürünü veya teknolojiyi yaz.
          Persona canlı araştırma yapar, gerekirse tek bir soru sorar ve
          kaynaklarıyla birlikte bir ilk-geçiş okuması sunar — karar sende.
        </p>

        <CriterionContinuum count={10} mode="wave" className="justify-center mb-6 opacity-70" />

        <div className="text-left mb-5">{intake}</div>

        <div className="flex flex-wrap justify-center gap-2">
          {OPENERS.map((o, i) => (
            <button
              key={o}
              type="button"
              onClick={() => onPick(o)}
              disabled={disabled}
              className="item-in card card-action px-3 py-1.5 text-xs text-left"
              style={{
                color: "var(--text-dim)",
                ["--i" as string]: i + 1,
              }}
            >
              {o}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

function UserBubble({ text }: { text: string }) {
  return (
    <div className="flex justify-end item-in">
      <div
        className="max-w-[85%] rounded-[var(--r-md)] rounded-br-[var(--r-xs)] px-4 py-2.5 text-sm whitespace-pre-wrap leading-relaxed"
        style={{
          background: "linear-gradient(180deg, var(--brand-hi), var(--brand))",
          color: "var(--brand-ink)",
          boxShadow: "var(--bevel), var(--shadow-1)",
        }}
      >
        {text}
      </div>
    </div>
  );
}

function PersonaBubble({
  msg,
  showReportCta,
  linkedAssessmentId,
  reportLoaded,
  reportLoading,
  onProduceReport,
  onShowReport,
}: {
  msg: {
    content: string;
    sources: DecisionSource[];
    research: ResearchStep[];
    model: string;
  };
  showReportCta?: boolean;
  linkedAssessmentId?: string | null;
  reportLoaded?: boolean;
  reportLoading?: boolean;
  onProduceReport?: () => void;
  onShowReport?: () => void;
}) {
  // The source count is what decides whether the SKOR line is a measurement;
  // see lib/verdict.ts. The server applies the same rule to what it stores.
  const verdict = parseVerdict(msg.content, msg.sources.length);
  const tone = verdict ? toneFor(verdict.label) : null;
  const canShow =
    Boolean(linkedAssessmentId) && Boolean(reportLoaded) && !reportLoading;

  return (
    <div className="space-y-2.5 item-in">
      {verdict && tone && (
        <div
          className="inline-flex items-center gap-2.5 pl-3 pr-2.5 py-1.5 rounded-[var(--r-sm)]"
          style={{
            background: "var(--panel-2)",
            border: `1px solid ${tone}`,
            boxShadow: "var(--bevel)",
          }}
        >
          <span className="lamp" style={{ color: tone }} />
          <span
            className="text-sm font-semibold font-display tracking-tight"
            style={{ color: tone }}
          >
            {verdict.label}
          </span>
          {verdict.score !== null && (
            <span className="pill mono num">{verdict.score}/100</span>
          )}
        </div>
      )}

      <div className="well p-4">
        <RichText text={verdict ? stripVerdictLines(msg.content) : msg.content} />
      </div>

      {msg.sources.length > 0 && <SourceList sources={msg.sources} />}

      {showReportCta && verdict && (
        <div className="flex flex-wrap gap-2 pt-1">
          <button
            type="button"
            className="btn btn-primary btn-sm"
            onClick={onProduceReport}
            disabled={reportLoading}
          >
            {reportLoading ? "Rapor üretiliyor…" : "Rapor üret"}
          </button>
          {canShow && (
            <button
              type="button"
              className="btn btn-quiet btn-sm"
              onClick={onShowReport}
            >
              Raporu göster
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function SourceList({ sources }: { sources: DecisionSource[] }) {
  return (
    <div className="card overflow-hidden">
      <div
        className="px-4 py-2 eyebrow"
        style={{
          background: "var(--panel-2)",
          borderBottom: "1px solid var(--line)",
        }}
      >
        {sources.length} kaynak
      </div>
      {sources.map((s, i) => (
        <div
          key={s.n}
          className="px-4 py-2.5 flex gap-3 items-baseline"
          style={{ borderTop: i === 0 ? undefined : "1px solid var(--line)" }}
        >
          <span
            className="text-xs mono num shrink-0"
            style={{ color: "var(--text-faint)" }}
          >
            [{s.n}]
          </span>
          <div className="min-w-0">
            {s.url ? (
              <a
                href={s.url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-sm break-words hover:underline"
                style={{ color: "var(--text-dim)" }}
              >
                {s.title || s.url}
              </a>
            ) : (
              <span className="text-sm" style={{ color: "var(--text-dim)" }}>
                {s.title}
              </span>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

/**
 * The waiting state. Three stepping dots rather than a spinner, and a line that
 * names the phase — a turn here runs a web search and then the model, so it is
 * long enough that "working" needs to be visibly true, not just claimed.
 */
function Thinking() {
  return (
    <div className="flex items-center gap-3 item-in">
      <div
        className="w-8 h-8 rounded-[var(--r-sm)] grid place-items-center shrink-0"
        style={{
          background: "var(--brand-wash)",
          border: "1px solid var(--brand-line)",
          color: "var(--brand)",
        }}
      >
        <div className="flex gap-[3px]">
          <span className="dot" />
          <span className="dot" />
          <span className="dot" />
        </div>
      </div>
      <span className="text-sm" style={{ color: "var(--text-dim)" }}>
        Canlı araştırıyor ve ilk-geçiş okumasını hazırlıyor…
      </span>
    </div>
  );
}
