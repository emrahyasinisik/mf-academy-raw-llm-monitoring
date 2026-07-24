"use client";

// DeepKwiki: search the knowledge base, or ask it a question.
//
// Two things happen on this screen and they are kept visually separate on
// purpose. Search returns passages that are definitionally true — they are the
// corpus. Asking returns a 2B model's summary of those passages, which is
// useful and is not the same kind of object. Presenting them in one undifferen-
// tiated block is how a generated sentence ends up quoted as a source.
//
// The rules this view enforces, mirroring the report screen's:
//
//   1. An answer never renders without its sources. They are one component.
//   2. An ungrounded answer is labelled as such, loudly. It is the model
//      talking about the world, which is what this feature exists to avoid.
//   3. "Not in the knowledge base" is presented as a legitimate answer, not as
//      an error — the alternative teaches people that an empty corpus and a
//      broken server look the same.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { localMcpClient, McpToolError } from "@/lib/mcp";
import type { WikiAnswer, WikiDocument, WikiHit, WikiMatch } from "@/lib/types";
import { RichText } from "@/components/ui/RichText";
import { SubNav } from "@/components/ui/SubNav";

type Tab = "ara" | "belgeler";
type Transport = "rest" | "mcp";

const TABS = [
  { id: "ara" as const, label: "Ara & Sor" },
  { id: "belgeler" as const, label: "Belgeler" },
];

/**
 * How a passage was found, in the reader's terms. "fuzzy" gets a warning tone
 * because those results routinely have nothing to do with the question — the
 * label is the only thing stopping someone from treating one as an answer.
 */
const MATCH_LABEL: Record<WikiMatch, { text: string; color: string }> = {
  all: { text: "tam eşleşme", color: "var(--good)" },
  any: { text: "kısmi eşleşme", color: "var(--warn)" },
  fuzzy: { text: "yakın benzerlik", color: "var(--bad)" },
};

/**
 * Truncate on a line boundary. Cutting mid-line through a Markdown table leaves
 * a half row that renders as a broken table rather than as clipped text.
 */
function clip(text: string, max: number): string {
  if (text.length <= max) return text;
  const cut = text.lastIndexOf("\n", max);
  return text.slice(0, cut > max / 2 ? cut : max) + "\n\n…";
}

/** Renders « » highlight markers as marks. The markers come from ts_headline. */
function Snippet({ text }: { text: string }) {
  const parts = text.split(/(«[^»]*»)/g);
  return (
    <>
      {parts.map((p, i) =>
        p.startsWith("«") ? (
          <mark
            key={i}
            style={{ background: "var(--accent-soft)", color: "var(--text)" }}
            className="px-0.5 rounded"
          >
            {p.slice(1, -1)}
          </mark>
        ) : (
          <span key={i}>{p}</span>
        ),
      )}
    </>
  );
}

function HitRow({ hit }: { hit: WikiHit }) {
  const [open, setOpen] = useState(false);
  const m = MATCH_LABEL[hit.matched] ?? MATCH_LABEL.fuzzy;

  return (
    <div style={{ borderTop: "1px solid var(--border)" }}>
      <button
        onClick={() => setOpen((v) => !v)}
        className="w-full text-left px-4 py-3 hover:opacity-90"
        aria-expanded={open}
      >
        <span className="flex items-center justify-between gap-3 mb-1">
          <span className="text-sm font-medium truncate">
            {hit.title}
            {hit.heading && (
              <span style={{ color: "var(--text-faint)" }}> — {hit.heading}</span>
            )}
          </span>
          <span className="flex items-center gap-2 shrink-0">
            <span className="pill text-xs" style={{ color: m.color, borderColor: "var(--border)" }}>
              {m.text}
            </span>
            <span className="text-xs" style={{ color: "var(--text-faint)" }}>
              {open ? "▲" : "▼"}
            </span>
          </span>
        </span>
        <span className="text-xs leading-relaxed block" style={{ color: "var(--text-dim)" }}>
          <Snippet text={hit.snippet} />
        </span>
      </button>

      {open && (
        <div className="px-4 pb-4">
          {/* The verbatim passage, not the highlighted snippet. Everything the
              product claims rests on this being checkable, so it is shown as
              the corpus holds it. */}
          <div
            className="text-xs leading-relaxed p-3 rounded"
            style={{ background: "var(--bg-elev-2)" }}
          >
            <RichText text={hit.body} />
          </div>
          <div className="text-xs mt-2" style={{ color: "var(--text-faint)" }}>
            {hit.document_slug} · bölüm {hit.ordinal}
            {hit.source_url && (
              <>
                {" · "}
                <a
                  href={hit.source_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  style={{ color: "var(--accent)" }}
                >
                  kaynak
                </a>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function AnswerCard({ answer }: { answer: WikiAnswer }) {
  return (
    <div className="space-y-3">
      <div className="card p-4">
        <div className="flex items-center justify-between gap-3 mb-2">
          <h3 className="text-sm font-semibold">Yanıt</h3>
          {answer.no_results ? (
            <span className="pill text-xs" style={{ color: "var(--text-faint)", borderColor: "var(--border)" }}>
              kapsam dışı
            </span>
          ) : answer.grounded ? (
            <span className="pill text-xs" style={{ color: "var(--good)", borderColor: "var(--border)" }}>
              kaynaklara dayalı
            </span>
          ) : (
            <span className="pill text-xs" style={{ color: "var(--bad)", borderColor: "var(--border)" }}>
              kaynaksız
            </span>
          )}
        </div>

        <RichText text={answer.text} />

        {/* Loud, and above the sources rather than below them. An ungrounded
            answer that looks like the grounded ones is the single most
            misleading thing this screen can show. */}
        {!answer.grounded && !answer.no_results && (
          <p
            className="text-xs mt-3 p-2.5 rounded leading-relaxed"
            style={{ background: "var(--accent-soft)", color: "var(--bad)" }}
          >
            Bu yanıt getirilen pasajların hiçbirine atıf yapmadı. Bilgi tabanının cevabı
            gibi aktarma — modelin kendi genel bilgisinden gelmiş olabilir. Aşağıdaki
            pasajları kendin oku.
          </p>
        )}

        {answer.no_results && (
          <p className="text-xs mt-3 leading-relaxed" style={{ color: "var(--text-dim)" }}>
            Bu bir hata değil. Bilgi tabanı bu konuyu kapsamıyor, ve model bu soru için
            hiç çağrılmadı.
          </p>
        )}

        {answer.model && (
          <div className="text-xs mt-3 flex flex-wrap gap-x-4" style={{ color: "var(--text-faint)" }}>
            <span>model: {answer.model}</span>
            <span>gecikme: {(answer.latency_ms / 1000).toFixed(1)}s</span>
          </div>
        )}
      </div>

      {answer.sources.length > 0 && (
        <div className="card overflow-hidden">
          <div className="px-4 py-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold">Kaynaklar</h3>
            <span className="text-xs" style={{ color: "var(--text-faint)" }}>
              {answer.sources.filter((s) => s.cited).length} / {answer.sources.length} atıf aldı
            </span>
          </div>
          {answer.sources.map((s) => (
            <div key={s.n} className="px-4 py-3" style={{ borderTop: "1px solid var(--border)" }}>
              <div className="flex items-center gap-2 mb-1.5">
                <span
                  className="pill text-xs tabular-nums"
                  style={{
                    color: s.cited ? "var(--accent)" : "var(--text-faint)",
                    borderColor: "var(--border)",
                  }}
                >
                  [{s.n}]
                </span>
                <span className="text-xs truncate">
                  {s.title}
                  {s.heading && <span style={{ color: "var(--text-faint)" }}> — {s.heading}</span>}
                </span>
                {!s.cited && (
                  <span className="text-xs ml-auto shrink-0" style={{ color: "var(--text-faint)" }}>
                    kullanılmadı
                  </span>
                )}
              </div>
              {/* Rendered, not printed raw. The corpus is Markdown, so an
                  unrendered passage shows its ** and | as literal characters —
                  which makes the one thing the reader is here to check the
                  hardest thing on the screen to read. Truncated on a line
                  boundary so a cut never lands inside a table. */}
              <div
                className="text-xs leading-relaxed pl-3"
                style={{ borderLeft: "2px solid var(--border)", color: "var(--text-dim)" }}
              >
                <RichText text={clip(s.body, 700)} />
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export function WikiView({ sub, onSub }: { sub: string; onSub: (s: string) => void }) {
  const tab: Tab = sub === "belgeler" ? "belgeler" : "ara";

  const [query, setQuery] = useState("");
  const [transport, setTransport] = useState<Transport>("rest");
  const [hits, setHits] = useState<WikiHit[] | null>(null);
  const [answer, setAnswer] = useState<WikiAnswer | null>(null);
  const [searching, setSearching] = useState(false);
  const [asking, setAsking] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [error, setError] = useState("");

  const [docs, setDocs] = useState<WikiDocument[]>([]);

  useEffect(() => {
    api
      .wikiDocuments()
      .then((r) => setDocs(r.documents))
      .catch(() => {});
  }, []);

  // Same counting timer as the analysis screen, for the same reason: an
  // indeterminate spinner at 30+ seconds reads as "stuck" and people reload,
  // which queues a second job on a card that serves one at a time.
  useEffect(() => {
    if (!asking) return;
    const started = Date.now();
    const t = setInterval(() => setElapsed(Math.floor((Date.now() - started) / 1000)), 500);
    return () => clearInterval(t);
  }, [asking]);

  const search = useCallback(async () => {
    const q = query.trim();
    if (q.length < 2) return setError("En az 2 karakter yaz.");
    setError("");
    setSearching(true);
    setAnswer(null);
    try {
      if (transport === "mcp") {
        const r = await localMcpClient().searchWiki(q, 8);
        // The MCP tool returns passages without the display snippet, so one is
        // synthesised here rather than leaving the row blank.
        setHits(
          r.passages.map((p) => ({
            document_slug: p.document,
            title: p.title,
            source_url: "",
            ordinal: p.ordinal,
            heading: p.heading,
            body: p.body,
            snippet: p.body.slice(0, 240),
            rank: p.rank,
            matched: p.matched,
          })),
        );
      } else {
        setHits((await api.wikiSearch(q, 8)).hits);
      }
    } catch (e) {
      setHits(null);
      setError(e instanceof McpToolError || e instanceof ApiError ? e.message : "Arama başarısız.");
    } finally {
      setSearching(false);
    }
  }, [query, transport]);

  const ask = useCallback(async () => {
    const q = query.trim();
    if (q.length < 2) return setError("En az 2 karakter yaz.");
    setError("");
    setAsking(true);
    setElapsed(0);
    try {
      const a =
        transport === "mcp"
          ? await localMcpClient().askWiki(q)
          : await api.wikiAsk(q);
      setAnswer(a);
      // Search runs alongside so the passages are visible next to the answer.
      // It is a database query and costs nothing next to the generation.
      api
        .wikiSearch(q, 8)
        .then((r) => setHits(r.hits))
        .catch(() => {});
    } catch (e) {
      setAnswer(null);
      if (e instanceof McpToolError) setError(e.message);
      else if (e instanceof ApiError)
        setError(
          e.status === 503
            ? "Çıkarım sunucusu kapalı. Arama çalışmaya devam ediyor — 'Ara'ya bas."
            : e.message,
        );
      else setError("Yanıt alınamadı.");
    } finally {
      setAsking(false);
    }
  }, [query, transport]);

  return (
    <div className="max-w-5xl mx-auto p-5 space-y-4">
      <SubNav items={TABS} active={tab} onSelect={onSub} />

      {tab === "ara" ? (
        <>
          <div className="card p-4 space-y-3">
            <label className="block">
              <span className="text-xs block mb-1" style={{ color: "var(--text-faint)" }}>
                Soru veya arama ifadesi
              </span>
              <input
                className="input w-full"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && !asking && !searching && search()}
                placeholder="örn. LoRA rank ve alpha nasıl seçilir"
                disabled={asking}
              />
            </label>

            <div className="flex flex-wrap items-center justify-between gap-3">
              <span className="text-xs" style={{ color: "var(--text-faint)" }}>
                {docs.length} belge · {docs.reduce((n, d) => n + d.chunks, 0)} pasaj
              </span>
              <div className="flex items-center gap-3">
                <label className="text-xs flex items-center gap-1.5" style={{ color: "var(--text-faint)" }}>
                  yol
                  <select
                    className="input !py-1 !px-2 text-xs"
                    value={transport}
                    onChange={(e) => setTransport(e.target.value as Transport)}
                    disabled={asking || searching}
                  >
                    <option value="rest">REST</option>
                    <option value="mcp">MCP</option>
                  </select>
                </label>
                <button className="btn" onClick={search} disabled={searching || asking}>
                  {searching ? "Aranıyor…" : "Ara"}
                </button>
                <button className="btn btn-primary" onClick={ask} disabled={asking || searching}>
                  {asking ? `Yanıtlanıyor… ${elapsed}s` : "Sor"}
                </button>
              </div>
            </div>

            {/* Stated up front, because the difference decides which button to
                press and how much to trust what comes back. */}
            <p className="text-xs leading-relaxed" style={{ color: "var(--text-dim)" }}>
              <strong style={{ color: "var(--text)" }}>Ara</strong> belgelerdeki pasajları
              birebir getirir; anında döner ve kesin doğrudur — korpusun kendisidir.{" "}
              <strong style={{ color: "var(--text)" }}>Sor</strong> aynı pasajları küçük
              modele özetletir; 30-60 saniye sürer ve yalnızca bir özettir.
            </p>

            {error && (
              <p className="text-xs p-2.5 rounded" style={{ background: "var(--accent-soft)", color: "var(--bad)" }}>
                {error}
              </p>
            )}
          </div>

          {answer && <AnswerCard answer={answer} />}

          {hits && (
            <div className="card overflow-hidden">
              <div className="px-4 py-3 flex items-center justify-between">
                <h3 className="text-sm font-semibold">Pasajlar</h3>
                <span className="text-xs" style={{ color: "var(--text-faint)" }}>
                  {hits.length} sonuç
                </span>
              </div>
              {hits.length === 0 ? (
                <p
                  className="px-4 pb-4 text-xs leading-relaxed"
                  style={{ color: "var(--text-dim)", borderTop: "1px solid var(--border)", paddingTop: "0.75rem" }}
                >
                  Bilgi tabanında bu sorguyla eşleşen pasaj yok. Bu bir hata değil — aranan
                  konu henüz eklenmemiş demektir.
                </p>
              ) : (
                hits.map((h) => <HitRow key={`${h.document_slug}-${h.ordinal}`} hit={h} />)
              )}
            </div>
          )}
        </>
      ) : (
        <div className="card overflow-hidden">
          <div className="px-4 py-3">
            <h3 className="text-sm font-semibold">Bilgi tabanındaki belgeler</h3>
          </div>
          {docs.length === 0 ? (
            <p className="px-4 pb-4 text-xs" style={{ color: "var(--text-faint)" }}>
              Henüz belge yok. Belge eklemek yönetici yetkisi ister.
            </p>
          ) : (
            docs.map((d) => (
              <div key={d.id} className="px-4 py-3" style={{ borderTop: "1px solid var(--border)" }}>
                <div className="flex items-center justify-between gap-3">
                  <span className="text-sm truncate">{d.title}</span>
                  <span className="text-xs tabular-nums shrink-0" style={{ color: "var(--text-faint)" }}>
                    {d.chunks} pasaj
                  </span>
                </div>
                <div className="text-xs mt-0.5" style={{ color: "var(--text-faint)" }}>
                  {d.slug}
                  {d.tags.length > 0 && ` · ${d.tags.join(", ")}`}
                </div>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}
