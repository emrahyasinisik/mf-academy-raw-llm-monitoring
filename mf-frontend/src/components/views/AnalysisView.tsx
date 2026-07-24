"use client";

// Case in, report out. The product's main screen.

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { api, ApiError } from "@/lib/api";
import {
  localMcpClient,
  McpToolError,
  webMcpAvailable,
  setWebMcpHandler,
  subscribeWebMcp,
  getWebMcpSnapshot,
  getWebMcpServerSnapshot,
} from "@/lib/mcp";
import type { AnalysisDomain, Assessment, AssessmentSummary } from "@/lib/types";
import { ReportCard } from "@/components/ui/ReportCard";

const MIN_SUBJECT = 40;
const MAX_SUBJECT = 32000;

/** How the request is sent. Both paths hit the same engine and must agree. */
type Transport = "rest" | "mcp";

export function AnalysisView() {
  const [domains, setDomains] = useState<AnalysisDomain[]>([]);
  const [domain, setDomain] = useState("");
  const [title, setTitle] = useState("");
  const [subject, setSubject] = useState("");
  const [transport, setTransport] = useState<Transport>("rest");

  const [running, setRunning] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [error, setError] = useState("");
  const [report, setReport] = useState<Assessment | null>(null);
  const [history, setHistory] = useState<AssessmentSummary[]>([]);

  const abortRef = useRef(false);

  useEffect(() => {
    api
      .analysisDomains()
      .then((r) => {
        setDomains(r.domains);
        if (r.domains.length) setDomain((d) => d || r.domains[0].slug);
      })
      .catch(() => setError("Rubrikler yüklenemedi."));
    api
      .analysisList(10)
      .then((r) => setHistory(r.assessments))
      .catch(() => {});
  }, []);

  const run = useCallback(
    async (d: string, s: string): Promise<Assessment> => {
      if (transport === "mcp") {
        return localMcpClient().analyzeCase(d, s, title);
      }
      return api.analysisRun({ domain: d, subject: s, subject_title: title });
    },
    [transport, title],
  );

  // Offer the same operation to an in-browser agent. A no-op in every browser
  // shipping today; wired anyway so the surface is real and testable, and the
  // UI reports its absence honestly rather than showing a dead control.
  //
  // The handler is set separately from the subscription so the tool is
  // registered exactly once. Re-registering whenever `run` changes identity
  // would leave duplicate entries in an agent's tool list.
  setWebMcpHandler(run);
  const webmcp = useSyncExternalStore(
    subscribeWebMcp,
    getWebMcpSnapshot,
    getWebMcpServerSnapshot,
  );

  // A visible timer, not a spinner. Generation takes tens of seconds on one
  // consumer GPU behind a tunnel, and an indeterminate spinner at that length
  // reads as "stuck" — people reload, which queues a second job on a card that
  // serves one at a time and makes the wait worse.
  useEffect(() => {
    if (!running) return;
    const started = Date.now();
    const t = setInterval(() => setElapsed(Math.floor((Date.now() - started) / 1000)), 500);
    return () => clearInterval(t);
  }, [running]);

  async function submit() {
    const body = subject.trim();
    if (!domain) return setError("Bir rubrik seç.");
    if (body.length < MIN_SUBJECT)
      return setError(`Vaka metni en az ${MIN_SUBJECT} karakter olmalı.`);
    if (body.length > MAX_SUBJECT)
      return setError(`Vaka metni ${MAX_SUBJECT} karakteri aşamaz.`);

    setError("");
    setReport(null);
    setRunning(true);
    setElapsed(0);
    abortRef.current = false;

    try {
      const result = await run(domain, body);
      if (abortRef.current) return;
      setReport(result);
      api
        .analysisList(10)
        .then((r) => setHistory(r.assessments))
        .catch(() => {});
    } catch (e) {
      if (abortRef.current) return;
      // Three failure shapes, three different things for the reader to do, so
      // they are not collapsed into one "something went wrong".
      if (e instanceof McpToolError) {
        setError(e.message);
      } else if (e instanceof ApiError) {
        setError(
          e.status === 503
            ? "Çıkarım sunucusu şu an kapalı. Bu bir hata değil — model ev bilgisayarında koşuyor."
            : e.message,
        );
      } else {
        setError("Analiz tamamlanamadı. Sunucu hâlâ çalışıyor olabilir; geçmişi kontrol et.");
      }
    } finally {
      setRunning(false);
    }
  }

  const selected = domains.find((d) => d.slug === domain);
  const chars = subject.trim().length;

  return (
    <div className="max-w-6xl mx-auto p-5 space-y-5">
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="space-y-4">
          <div className="card p-4 space-y-3">
            <div className="flex flex-wrap gap-3">
              <label className="flex-1 min-w-[200px]">
                <span className="text-xs block mb-1" style={{ color: "var(--text-faint)" }}>
                  Rubrik
                </span>
                <select
                  className="input w-full"
                  value={domain}
                  onChange={(e) => setDomain(e.target.value)}
                  disabled={running}
                >
                  {domains.map((d) => (
                    <option key={d.slug} value={d.slug}>
                      {d.name} ({d.criteria.length} kriter)
                    </option>
                  ))}
                </select>
              </label>

              <label className="flex-1 min-w-[200px]">
                <span className="text-xs block mb-1" style={{ color: "var(--text-faint)" }}>
                  Başlık
                </span>
                <input
                  className="input w-full"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  placeholder="örn. FiloTakip — Seri A"
                  disabled={running}
                />
              </label>
            </div>

            {selected && (
              <p className="text-xs leading-relaxed" style={{ color: "var(--text-dim)" }}>
                {selected.description}
              </p>
            )}

            <label className="block">
              <span className="text-xs block mb-1" style={{ color: "var(--text-faint)" }}>
                Vaka metni
              </span>
              <textarea
                className="input w-full font-mono text-xs"
                rows={14}
                value={subject}
                onChange={(e) => setSubject(e.target.value)}
                placeholder="Pitch deck metnini buraya yapıştır…"
                disabled={running}
              />
            </label>

            <div className="flex flex-wrap items-center justify-between gap-3">
              <span className="text-xs tabular-nums" style={{ color: "var(--text-faint)" }}>
                {chars.toLocaleString("tr-TR")} karakter
                {chars > 0 && chars < MIN_SUBJECT && ` (en az ${MIN_SUBJECT})`}
              </span>

              <div className="flex items-center gap-3">
                <label className="text-xs flex items-center gap-1.5" style={{ color: "var(--text-faint)" }}>
                  yol
                  <select
                    className="input !py-1 !px-2 text-xs"
                    value={transport}
                    onChange={(e) => setTransport(e.target.value as Transport)}
                    disabled={running}
                  >
                    <option value="rest">REST</option>
                    <option value="mcp">MCP</option>
                  </select>
                </label>

                <button
                  className="btn btn-primary"
                  onClick={submit}
                  disabled={running || chars < MIN_SUBJECT || !domain}
                >
                  {running ? `Analiz ediliyor… ${elapsed}s` : "Analiz et"}
                </button>
              </div>
            </div>

            {running && (
              <p className="text-xs" style={{ color: "var(--text-dim)" }}>
                Model tek bir tüketici kartında koşuyor; bu genellikle 30-60 saniye sürer.
                Sayfayı yenileme — ikinci bir istek aynı kartın kuyruğuna girer ve beklemeyi uzatır.
              </p>
            )}

            {error && (
              <p className="text-xs p-2.5 rounded" style={{ background: "var(--accent-soft)", color: "var(--bad)" }}>
                {error}
              </p>
            )}
          </div>

          {report && <ReportCard report={report} />}
        </div>

        <aside className="space-y-4">
          <div className="card p-4">
            <h3 className="text-sm font-semibold mb-2">Nasıl okunur</h3>
            <ul className="text-xs space-y-2 leading-relaxed" style={{ color: "var(--text-dim)" }}>
              <li>
                <strong style={{ color: "var(--text)" }}>Puanı kapsamsız okuma.</strong> %90
                kapsamda 75 ile %30 kapsamda 75 aynı sayı, farklı sonuçtur.
              </li>
              <li>
                <strong style={{ color: "var(--text)" }}>Boş kriter kötü kriter değildir.</strong>{" "}
                Vakada geçmeyen bir kriter puana hiç katılmaz, sıfır almaz.
              </li>
              <li>
                <strong style={{ color: "var(--text)" }}>Puanı model vermez.</strong> Model
                kriterleri kanıtla doldurur; ağırlıklı toplamı sunucu hesaplar.
              </li>
            </ul>
          </div>

          <div className="card p-4">
            <h3 className="text-sm font-semibold mb-2">Son raporlar</h3>
            {history.length === 0 ? (
              <p className="text-xs" style={{ color: "var(--text-faint)" }}>
                Henüz rapor yok.
              </p>
            ) : (
              <ul className="space-y-1.5">
                {history.map((h) => (
                  <li key={h.id}>
                    <button
                      className="w-full text-left text-xs py-1.5 hover:opacity-80"
                      onClick={() =>
                        api
                          .analysisGet(h.id)
                          .then(setReport)
                          .catch(() => setError("Rapor açılamadı."))
                      }
                    >
                      <span className="flex items-center justify-between gap-2">
                        <span className="truncate">{h.subject_title || "(başlıksız)"}</span>
                        <span className="tabular-nums shrink-0" style={{ color: "var(--text-faint)" }}>
                          {h.overall_score === null ? "—" : h.overall_score.toFixed(0)}
                          {" · %"}
                          {Math.round(h.coverage * 100)}
                        </span>
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="card p-4">
            <h3 className="text-sm font-semibold mb-1.5">WebMCP</h3>
            <p className="text-xs leading-relaxed" style={{ color: "var(--text-dim)" }}>
              {webmcp
                ? "Bu sayfanın araçları tarayıcıdaki ajana açıldı."
                : webMcpAvailable()
                  ? "navigator.modelContext var ama araçlar kaydedilemedi."
                  : "Bu tarayıcı navigator.modelContext desteklemiyor. WebMCP hâlâ bir taslak; analiz REST ve MCP yollarından çalışmaya devam ediyor."}
            </p>
          </div>
        </aside>
      </div>
    </div>
  );
}
