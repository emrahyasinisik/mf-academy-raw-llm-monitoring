"use client";

// The investment persona: one screen, one conversation. The user names a market,
// brand, product or technology; the persona researches it live and works toward
// an investability verdict, asking a clarifying question when it must. There is
// no separate "analysis" and no separate "knowledge base" here on purpose —
// research and decision are the same agent, and this screen is its whole surface.

import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { DecisionSource, DecisionTurn, ResearchStep } from "@/lib/types";
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
  yatırılabilir: "var(--good)",
  temkinli: "var(--warn)",
  yatırılamaz: "var(--bad)",
};

function parseVerdict(text: string): Verdict | null {
  const m = text.match(/KARAR:\s*([^\n]+)/i);
  if (!m) return null;
  const label = m[1].trim();
  const s = text.match(/SKOR:\s*(\d{1,3})/i);
  return { label, score: s ? Math.min(100, parseInt(s[1], 10)) : null };
}

function toneFor(label: string): string {
  const key = label.toLowerCase();
  for (const k of Object.keys(VERDICT_TONE)) {
    if (key.includes(k)) return VERDICT_TONE[k];
  }
  return "var(--accent)";
}

export function PersonaView() {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);

  // Keep the newest message in view as the conversation grows.
  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: "smooth" });
  }, [messages, running]);

  const send = useCallback(async () => {
    const text = input.trim();
    if (text.length < 2 || running) return;

    const nextUser: ChatMessage = { role: "user", content: text };
    const history = [...messages, nextUser];
    setMessages(history);
    setInput("");
    setError("");
    setRunning(true);

    const wire: DecisionTurn[] = history.map((m) => ({ role: m.role, content: m.content }));
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
      const msg =
        e instanceof ApiError
          ? e.message
          : "Persona şu an yanıt veremedi. Çıkarım sunucusu kapalı olabilir.";
      setError(msg);
    } finally {
      setRunning(false);
    }
  }, [input, messages, running]);

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
    <div className="h-full flex flex-col max-w-3xl mx-auto w-full px-4">
      {messages.length === 0 ? (
        <Intro />
      ) : (
        <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto py-6 space-y-6">
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
        <div
          className="mb-3 px-4 py-3 rounded-lg text-sm"
          style={{ background: "var(--accent-soft)", color: "var(--bad)" }}
        >
          {error}
        </div>
      )}

      <div className="pb-5 pt-2" style={{ borderTop: messages.length ? "1px solid var(--border)" : "none" }}>
        <div className="flex items-end gap-2">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={onKey}
            rows={2}
            disabled={running}
            placeholder="Bir pazar, marka, ürün veya teknoloji yaz… (ör. 'Acme AI, seed aşaması B2B SaaS')"
            className="flex-1 resize-none rounded-lg px-3 py-2 text-sm outline-none"
            style={{
              background: "var(--bg-elev-2)",
              border: "1px solid var(--border)",
              color: "var(--text)",
            }}
          />
          <button className="btn" onClick={send} disabled={running || input.trim().length < 2}>
            {running ? "Araştırıyor…" : "Gönder"}
          </button>
          {messages.length > 0 && (
            <button className="btn btn-ghost" onClick={reset} disabled={running} title="Yeni değerlendirme">
              Sıfırla
            </button>
          )}
        </div>
        <p className="text-xs mt-2" style={{ color: "var(--text-faint)" }}>
          Persona her turda canlı web + DeepKwiki üzerinden araştırır ve kararını kaynaklara bağlar.
        </p>
      </div>
    </div>
  );
}

function Intro() {
  return (
    <div className="flex-1 grid place-items-center text-center px-6">
      <div className="max-w-md">
        <div
          className="mx-auto w-12 h-12 rounded-xl grid place-items-center text-xl mb-4"
          style={{ background: "var(--accent-soft)", color: "var(--accent)" }}
        >
          ◈
        </div>
        <h2 className="text-lg font-semibold mb-2">Yatırım Personası</h2>
        <p className="text-sm leading-relaxed" style={{ color: "var(--text-dim)" }}>
          Değerlendirmek istediğin pazarı, markayı, ürünü veya teknolojiyi yaz. Persona canlı
          araştırma yapar, gerekirse tek bir soru sorar ve yatırılabilirlik kararını —
          kaynaklarıyla — verir.
        </p>
      </div>
    </div>
  );
}

function UserBubble({ text }: { text: string }) {
  return (
    <div className="flex justify-end">
      <div
        className="max-w-[85%] rounded-2xl rounded-br-sm px-4 py-2.5 text-sm whitespace-pre-wrap"
        style={{ background: "var(--accent)", color: "#06122b" }}
      >
        {text}
      </div>
    </div>
  );
}

function PersonaBubble({
  msg,
}: {
  msg: { content: string; sources: DecisionSource[]; research: ResearchStep[]; model: string };
}) {
  const verdict = parseVerdict(msg.content);
  return (
    <div className="space-y-3">
      {verdict && (
        <div
          className="inline-flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-semibold"
          style={{ background: "var(--accent-soft)", color: toneFor(verdict.label) }}
        >
          <span>KARAR: {verdict.label}</span>
          {verdict.score !== null && (
            <span className="pill text-xs" style={{ borderColor: "var(--border)" }}>
              {verdict.score}/100
            </span>
          )}
        </div>
      )}

      <div className="card p-4">
        <RichText text={msg.content} />
      </div>

      {msg.research.length > 0 && <ResearchTrail steps={msg.research} />}
      {msg.sources.length > 0 && <SourceList sources={msg.sources} />}
    </div>
  );
}

function ResearchTrail({ steps }: { steps: ResearchStep[] }) {
  return (
    <div className="flex flex-wrap gap-2 text-xs" style={{ color: "var(--text-faint)" }}>
      {steps.map((s, i) => (
        <span
          key={i}
          className="pill"
          style={{ borderColor: "var(--border)" }}
          title={s.query}
        >
          🔎 {s.tool === "web_research" ? "web" : "DeepKwiki"} · {s.results} sonuç
        </span>
      ))}
    </div>
  );
}

function SourceList({ sources }: { sources: DecisionSource[] }) {
  return (
    <div className="card overflow-hidden">
      <div className="px-4 py-2 text-xs" style={{ color: "var(--text-faint)", background: "var(--bg-elev-2)" }}>
        {sources.length} kaynak
      </div>
      {sources.map((s) => (
        <div key={s.n} className="px-4 py-2.5 flex gap-3" style={{ borderTop: "1px solid var(--border)" }}>
          <span className="text-xs mono shrink-0" style={{ color: "var(--text-faint)" }}>
            [{s.n}]
          </span>
          <div className="min-w-0">
            <span
              className="pill text-xs mr-2"
              style={{ color: s.kind === "web" ? "var(--accent)" : "var(--good)", borderColor: "var(--border)" }}
            >
              {s.kind === "web" ? "web" : "DeepKwiki"}
            </span>
            {s.url ? (
              <a
                href={s.url}
                target="_blank"
                rel="noreferrer"
                className="text-sm hover:underline break-words"
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

function Thinking() {
  return (
    <div className="flex items-center gap-2 text-sm animate-pulse-soft" style={{ color: "var(--text-dim)" }}>
      <span
        className="w-6 h-6 rounded-lg grid place-items-center text-xs"
        style={{ background: "var(--accent-soft)", color: "var(--accent)" }}
      >
        ◈
      </span>
      Canlı araştırıyor ve kararını oluşturuyor…
    </div>
  );
}
