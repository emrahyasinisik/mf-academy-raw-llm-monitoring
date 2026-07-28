"use client";

// The investment persona: one screen, one conversation. The user names a market,
// brand, product or technology; the persona researches it live and works toward
// an investability verdict, asking a clarifying question when it must. There is
// no separate "analysis" and no separate "knowledge base" here on purpose —
// research and decision are the same agent, and this screen is its whole surface.

import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { DecisionSource, DecisionTurn, ResearchStep } from "@/lib/types";
import { useMachine } from "@/store/machine";
import { RichText } from "@/components/ui/RichText";

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

// A verdict the persona committed to, parsed out of its reply so it can be shown
// as a badge rather than buried in a paragraph. Absent while it is still
// researching or asking questions.
type Verdict = { label: string; score: number | null };

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

function parseVerdict(text: string): Verdict | null {
  const m = text.match(/KARAR:\s*([^\n]+)/i);
  if (!m) return null;
  const label = m[1].trim();
  const s = text.match(/SKOR:\s*(\d{1,3})/i);
  return { label, score: s ? Math.min(100, parseInt(s[1], 10)) : null };
}

/**
 * Removes the machine-readable verdict lines from the prose.
 *
 * They are a protocol between the persona and this screen, not part of what it
 * wrote: once parsed into the badge, leaving them in the body renders the
 * decision twice, and the second copy is worse — the two lines collapse into one
 * run-on paragraph ("KARAR: Temkinli yatırılabilir SKOR: 64") because nothing in
 * markdown makes them separate blocks.
 *
 * Only applied when a verdict was actually parsed, so a reply that mentions
 * these words without committing to a decision keeps every word it wrote.
 */
function stripVerdictLines(text: string): string {
  return text
    .split("\n")
    .filter((line) => !/^\s*(KARAR|SKOR)\s*:/i.test(line))
    .join("\n")
    .trimEnd();
}

function toneFor(label: string): string {
  const key = label.toLowerCase();
  for (const k of Object.keys(VERDICT_TONE)) {
    if (key.includes(k)) return VERDICT_TONE[k];
  }
  return "var(--brand)";
}

export function PersonaView() {
  const { begin } = useMachine();
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);

  // Keep the newest message in view as the conversation grows.
  useEffect(() => {
    scrollRef.current?.scrollTo({
      top: scrollRef.current.scrollHeight,
      behavior: "smooth",
    });
  }, [messages, running]);

  const ask = useCallback(
    async (text: string) => {
      if (text.length < 2 || running) return;

      const nextUser: ChatMessage = { role: "user", content: text };
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
        const res = await api.decisionChat(wire);
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
    [messages, running, begin],
  );

  const send = useCallback(() => ask(input.trim()), [ask, input]);

  const onKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  };

  const reset = () => {
    setMessages([]);
    setError("");
    setInput("");
  };

  return (
    <div className="h-full flex flex-col max-w-3xl mx-auto w-full px-4 sm:px-5">
      {messages.length === 0 ? (
        <Intro onPick={ask} disabled={running} />
      ) : (
        <div
          ref={scrollRef}
          className="flex-1 min-h-0 overflow-y-auto scrollbar-thin py-6 space-y-6"
          aria-live="polite"
        >
          {messages.map((m, i) =>
            m.role === "user" ? (
              <UserBubble key={i} text={m.content} />
            ) : (
              <PersonaBubble key={i} msg={m} />
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
            disabled={running}
            aria-label="Değerlendirilecek konu"
            placeholder="Bir pazar, marka, ürün veya teknoloji yaz…"
            className="flex-1 resize-none bg-transparent border-0 px-2 py-1.5 text-sm outline-none min-w-0"
            style={{ color: "var(--text)" }}
          />
          <div className="flex items-center gap-1.5 shrink-0">
            {messages.length > 0 && (
              <button
                className="btn btn-quiet btn-sm"
                onClick={reset}
                disabled={running}
                title="Konuşmayı temizle ve yeni bir değerlendirmeye başla"
              >
                Sıfırla
              </button>
            )}
            <button
              className="btn btn-primary btn-sm"
              onClick={send}
              disabled={running || input.trim().length < 2}
            >
              {running ? "Araştırıyor…" : "Gönder"}
            </button>
          </div>
        </div>
        <p className="text-xs mt-2 px-1" style={{ color: "var(--text-faint)" }}>
          Persona her turda canlı web ve DeepKwiki üzerinden araştırır, kararını
          kaynaklara bağlar. <span className="mono">Enter</span> gönderir,{" "}
          <span className="mono">Shift+Enter</span> satır atlar.
        </p>
      </div>
    </div>
  );
}

function Intro({
  onPick,
  disabled,
}: {
  onPick: (text: string) => void;
  disabled: boolean;
}) {
  return (
    <div className="flex-1 grid place-items-center text-center px-2">
      <div className="max-w-md view-in">
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
        <h2 className="font-display text-xl font-semibold tracking-tight mb-2">
          Yatırım Personası
        </h2>
        <p
          className="text-sm leading-relaxed mb-6"
          style={{ color: "var(--text-dim)" }}
        >
          Değerlendirmek istediğin pazarı, markayı, ürünü veya teknolojiyi yaz.
          Persona canlı araştırma yapar, gerekirse tek bir soru sorar ve
          yatırılabilirlik kararını kaynaklarıyla verir.
        </p>

        <div className="flex flex-wrap justify-center gap-2">
          {OPENERS.map((o, i) => (
            <button
              key={o}
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
}: {
  msg: {
    content: string;
    sources: DecisionSource[];
    research: ResearchStep[];
    model: string;
  };
}) {
  const verdict = parseVerdict(msg.content);
  const tone = verdict ? toneFor(verdict.label) : null;

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

      <div className="card p-4">
        <RichText text={verdict ? stripVerdictLines(msg.content) : msg.content} />
      </div>

      {msg.research.length > 0 && <ResearchTrail steps={msg.research} />}
      {msg.sources.length > 0 && <SourceList sources={msg.sources} />}
    </div>
  );
}

function ResearchTrail({ steps }: { steps: ResearchStep[] }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {steps.map((s, i) => (
        <span key={i} className="pill mono" title={s.query}>
          <svg
            width="10"
            height="10"
            viewBox="0 0 16 16"
            fill="none"
            stroke="currentColor"
            strokeWidth={2}
            strokeLinecap="round"
            aria-hidden
          >
            <circle cx="7" cy="7" r="4.5" />
            <path d="m10.5 10.5 3 3" />
          </svg>
          {s.tool === "web_research" ? "web" : "DeepKwiki"} · {s.results}
        </span>
      ))}
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
            <span className={`pill mr-2 ${s.kind === "web" ? "" : "pill-ok"}`}>
              {s.kind === "web" ? "web" : "DeepKwiki"}
            </span>
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
        Canlı araştırıyor ve kararını oluşturuyor…
      </span>
    </div>
  );
}
