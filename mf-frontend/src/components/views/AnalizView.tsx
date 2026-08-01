"use client";

// Rubrik analizi: bir vaka girer, rubrik-puanlı bir rapor çıkar.
//
// Ürünün kendisi bu ekran. Motor uzun süredir çalışıyordu ama yalnızca API ve
// MCP üzerinden ulaşılabiliyordu, yani alıcıya açıp gösterilebilecek bir yeri
// yoktu — satılabilir ama gösterilemez bir durum.
//
// Neden bir sohbet değil de form: rubrik doldurmak çıkarım işidir, konuşma
// değil. Aynı vaka aynı okumayı vermeli, ve ürünün satıldığı şey tam olarak o
// tutarlılık. Sıcaklık da bu yüzden backend'de sabitlenmiş (analysisTemperature
// 0.1), operatörün sohbet için ayarladığı bir kaydırıcıya bırakılmamış.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { isRedacted, reportTitle } from "@/lib/report";
import { breakdown, caseBudgetChars, estimateTokens } from "@/lib/rubric";
import { useMachine } from "@/store/machine";
import type {
  AnalysisDomain,
  AppLimits,
  Assessment,
  AssessmentSummary,
} from "@/lib/types";

/** Backend'in asgari vaka uzunluğu (internal/analysis/handler.go:63). */
const MIN_SUBJECT_CHARS = 40;

export function AnalizView() {
  const { begin } = useMachine();

  const [domains, setDomains] = useState<AnalysisDomain[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [running, setRunning] = useState(false);
  const [runError, setRunError] = useState<string | null>(null);
  const [assessment, setAssessment] = useState<Assessment | null>(null);

  const [slug, setSlug] = useState("");
  const [title, setTitle] = useState("");
  const [subject, setSubject] = useState("");

  // Pencere dağıtıma göre değişiyor, o yüzden sunucudan geliyor. Ölçülen
  // karakter/token oranları lib/rubric.ts'te sabit — onlar dile ve tokenizer'a
  // ait, bu dağıtıma değil.
  const [windowTokens, setWindowTokens] = useState(1200);
  // Ölçüm, ait olduğu rubrikle birlikte saklanıyor. Ayrı bir sayı tutup rubrik
  // değişince effect içinden sıfırlamak, bir kare boyunca önceki rubriğin
  // bütçesini gösterme ihtimali bırakıyordu — ve yanlış bütçe, bütçesizlikten
  // kötü. Böyle tutulunca eskimiş ölçüm render'da elenmiş oluyor.
  const [prompt, setPrompt] = useState<{ slug: string; chars: number } | null>(null);

  const [history, setHistory] = useState<AssessmentSummary[]>([]);
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);

  const loadDomains = useCallback(() => {
    api
      .analysisDomains()
      .then((d) => {
        setDomains(d.domains);
        setLoadError(null);
        // Varsayılan rubrik yatırılabilirlik: beachhead ICP hızlandırma
        // programları ve melek ağları.
        const first =
          d.domains.find((x) => x.slug === "startup-investability") ?? d.domains[0];
        if (first) setSlug((s) => s || first.slug);
      })
      .catch((e: unknown) =>
        setLoadError(e instanceof ApiError ? e.message : "Rubrikler yüklenemedi."),
      );
  }, []);

  useEffect(loadDomains, [loadDomains]);

  const refreshHistory = useCallback(
    () =>
      api
        .analysisList(20)
        .then((r) => setHistory(r.assessments))
        .catch(() => {
          /* Geçmiş ikincil: alınamaması ekranın geri kalanını durdurmaz. */
        }),
    [],
  );

  useEffect(() => {
    void refreshHistory();
  }, [refreshHistory]);

  // Pencere: sunucunun bildirdiği sayı, alınamazsa backend'in kendi varsayılanı.
  useEffect(() => {
    api
      .config()
      .then((c) => {
        const limits = (c as { limits?: Partial<AppLimits> }).limits;
        if (limits?.max_prompt_tokens) setWindowTokens(limits.max_prompt_tokens);
      })
      .catch(() => {
        /* Varsayılan yeterli: bütçe biraz yanılır, gönderim engellenmez. */
      });
  }, []);

  // Seçili rubriğin sistem prompt'u ne kadar yer yiyor. İki rubrik arasında
  // 565 karakter fark var, yani bu rubrik başına okunmak zorunda.
  useEffect(() => {
    if (!slug) return;
    let cancelled = false;
    api
      .analysisPrompt(slug)
      .then((p) => {
        if (!cancelled) setPrompt({ slug, chars: p.system_prompt.length });
      })
      .catch(() => {
        if (!cancelled) setPrompt(null);
      });
    return () => {
      cancelled = true;
    };
  }, [slug]);

  // Başka bir rubriğe ait ölçüm burada eleniyor.
  const systemChars = prompt && prompt.slug === slug ? prompt.chars : null;

  // Ölçüm henüz gelmediyse bütçe gösterilmiyor: yanlış bir sayı göstermek,
  // sayı göstermemekten kötü.
  const budget = systemChars === null ? null : caseBudgetChars(windowTokens, systemChars);
  const over = budget !== null && subject.length > budget;
  // Backend 40 **bayttan** kısa vakayı reddediyor (handler.go:63). Karakter
  // olarak saymak Türkçe'de daima güvenli tarafta kalıyor: çok baytlı harfler
  // yüzünden 40 karakter her zaman en az 40 bayt eder.
  const tooShort = subject.trim().length < MIN_SUBJECT_CHARS;
  const canRun = slug !== "" && !tooShort && !over;

  const run = useCallback(async () => {
    if (running) return;
    setRunning(true);
    setRunError(null);
    // Durum çubuğu geçen süreyi sayabilsin diye. Bitirici `Run` bekliyor,
    // `Assessment` o değil — null geçiliyor, yani çubuk süreyi gösterir ama
    // son koşu özetini doldurmaz. Store'u genişletmenin bedeli bu ekran için
    // faydasından büyük.
    const done = begin("analiz");
    try {
      const result = await api.analysisRun({
        domain: slug,
        subject,
        subject_title: title.trim() || "Adsız vaka",
      });
      setAssessment(result);
      // Yeni rapor listenin başına geçsin.
      void refreshHistory();
    } catch (e: unknown) {
      setRunError(describeRunError(e));
    } finally {
      done(null);
      setRunning(false);
    }
  }, [begin, running, slug, subject, title, refreshHistory]);

  // İki adımlı onay, tarayıcının confirm() kutusu değil: bu arayüz koyu tema ve
  // kendi diliyle konuşuyor, ve confirm() ikisini de terk ediyor.
  //
  // Silinen satır listeden çıkarılmıyor, redakte işaretleniyor. Neyin silindiğini
  // göstermek, satırı yok etmekten dürüst — ve zaten sunucuda da olan bu.
  const remove = useCallback(async (id: string) => {
    setPendingDelete(null);
    try {
      await api.analysisDelete(id);
    } catch {
      setRunError("Rapor silinemedi.");
      return;
    }
    const stamp = new Date().toISOString();
    setHistory((prev) =>
      prev.map((h) =>
        h.id === id ? { ...h, redacted_at: stamp, subject_title: "" } : h,
      ),
    );
    setAssessment((cur) =>
      cur && cur.id === id
        ? { ...cur, redacted_at: stamp, subject_title: "", subject: "", findings: [] }
        : cur,
    );
  }, []);

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-6xl px-4 sm:px-5 py-6">
        <h1 className="font-display text-lg font-semibold">Analiz</h1>
        <p className="text-sm mt-1" style={{ color: "var(--text-dim)" }}>
          Bir vaka girin, rubrik-puanlı bir rapor alın.
        </p>

        {loadError && (
          <div className="notice notice-bad mt-4" role="alert">
            {loadError}
          </div>
        )}

        {!loadError && domains.length === 0 && (
          <p className="mono text-xs mt-4" style={{ color: "var(--text-faint)" }}>
            rubrikler yükleniyor…
          </p>
        )}

        {domains.length > 0 && (
          <div className="card mt-5 p-4 space-y-4">
            <div>
              <label className="label" htmlFor="analiz-rubrik">
                Rubrik
              </label>
              <select
                id="analiz-rubrik"
                className="input"
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
              >
                {domains.map((d) => (
                  <option key={d.slug} value={d.slug}>
                    {d.name} · v{d.version} · {d.criteria.length} kriter
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label className="label" htmlFor="analiz-baslik">
                Vaka başlığı
              </label>
              <input
                id="analiz-baslik"
                className="input"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="Şirket veya vaka adı"
              />
            </div>

            <div>
              <div className="flex items-baseline justify-between gap-3">
                <label className="label" htmlFor="analiz-vaka">
                  Vaka metni
                </label>
                <span
                  className="mono text-xs num"
                  style={{ color: over ? "var(--bad)" : "var(--text-faint)" }}
                >
                  {subject.length}
                  {budget !== null && ` / ${budget}`} karakter
                  {budget !== null && ` · ~${estimateTokens(subject)} token`}
                </span>
              </div>
              <textarea
                id="analiz-vaka"
                className="input"
                rows={10}
                value={subject}
                onChange={(e) => setSubject(e.target.value)}
                placeholder="Değerlendirilecek metni buraya yapıştırın."
                aria-invalid={over}
                aria-describedby={
                  over || (tooShort && subject.trim() !== "")
                    ? "analiz-vaka-uyari"
                    : undefined
                }
              />
            </div>

            {over && budget !== null && (
              <div className="notice notice-warn" id="analiz-vaka-uyari" role="alert">
                Metin bu rubriğin bıraktığı yerden {subject.length - budget} karakter
                uzun. Rubriğin kendisi de modele gönderiliyor ve pencereden yer
                kaplıyor; bu sınırın üstünde istek değerlendirilmeden reddedilir.
              </div>
            )}

            {!over && tooShort && subject.trim() !== "" && (
              <div className="notice" id="analiz-vaka-uyari" role="status">
                Değerlendirme için en az {MIN_SUBJECT_CHARS} karakter gerekiyor.
              </div>
            )}

            <div className="flex items-center gap-3">
              <button
                className="btn btn-primary"
                disabled={!canRun || running}
                onClick={run}
              >
                {running ? "Analiz ediliyor…" : "Analiz et"}
              </button>
              {running && (
                <span className="mono text-xs" style={{ color: "var(--text-faint)" }}>
                  model tüm rubriği tek seferde dolduruyor; bu bir dakikayı bulabilir
                </span>
              )}
            </div>
          </div>
        )}

        {runError && (
          <div className="notice notice-bad mt-4" role="alert">
            {runError}
          </div>
        )}

        {assessment && <Rapor assessment={assessment} />}

        {history.length > 0 && (
          <section className="mt-8 gecmis">
            <h2 className="eyebrow">Önceki raporlar</h2>
            <p className="text-xs mt-1" style={{ color: "var(--text-faint)" }}>
              Rapor içerikleri 30 gün sonra otomatik silinir. Puan kaydı kalır.
            </p>
            <div className="mt-2 space-y-1">
              {history.map((h) => (
                <div
                  key={h.id}
                  className="card w-full p-3 flex flex-wrap items-center gap-x-4 gap-y-1"
                >
                  <button
                    className="card-action text-left flex-1 min-w-0 truncate text-sm"
                    onClick={() =>
                      api
                        .analysisGet(h.id)
                        .then(setAssessment)
                        .catch(() => setRunError("Rapor açılamadı."))
                    }
                  >
                    {reportTitle(h)}
                  </button>
                  <span className="mono text-xs" style={{ color: "var(--text-faint)" }}>
                    {h.domain_name}
                  </span>
                  {/* Kapsam listede de puanın yanında: aynı sayı farklı
                      kapsamlarda farklı bulgu, ve unutulduğu yer tam burası. */}
                  <span className="mono text-xs num" style={{ color: "var(--text-dim)" }}>
                    {h.overall_score === null ? "—" : h.overall_score.toFixed(1)} ·{" "}
                    {pct(h.coverage)}
                  </span>
                  <span className="mono text-xs" style={{ color: "var(--text-faint)" }}>
                    {new Date(h.created_at).toLocaleDateString("tr-TR")}
                  </span>

                  {!isRedacted(h) && (
                    pendingDelete === h.id ? (
                      <span className="flex items-center gap-2">
                        <button className="btn btn-danger btn-sm" onClick={() => remove(h.id)}>
                          Silinsin
                        </button>
                        <button
                          className="btn btn-ghost btn-sm"
                          onClick={() => setPendingDelete(null)}
                        >
                          Vazgeç
                        </button>
                      </span>
                    ) : (
                      <button
                        className="btn btn-ghost btn-sm"
                        onClick={() => setPendingDelete(h.id)}
                      >
                        Sil
                      </button>
                    )
                  )}
                </div>
              ))}
            </div>
          </section>
        )}
      </div>
    </div>
  );
}

// Hataları operatörün okuyabileceği cümlelere çevirir.
//
// Ham gövdeyi ekrana basmak burada işe yaramıyor: bu yolun iki tipik hatası da
// başka bir katmandan geliyor ve o katmanın diliyle konuşuyor. 503 çıkarım
// makinesinin kapalı olması demek — desteklenen bir hal, arıza değil: API
// ayakta kalır ve tarayıcı tarafı etkilenmez. 400 ise neredeyse her zaman
// metnin pencereye sığmaması, ve sayaç bunu zaten önceden söylüyor olmalıydı.
function describeRunError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 503) {
      return "Çıkarım makinesi şu anda ulaşılamıyor. Analiz için gerekli, ama diğer ekranlar çalışmaya devam eder.";
    }
    if (e.status === 400) {
      // Yön varsayılmıyor. Bu yolda 400'ün dört sebebi var — konu çok kısa,
      // çok uzun, rubrik seçilmemiş, ya da motorun penceresine sığmamış — ve
      // "kısaltın" demek, çok kısa yazmış birine verilecek en kötü öğüt.
      // Sunucunun kendi cümlesi hangisi olduğunu söylüyor.
      return `Vaka değerlendirilmeden reddedildi: ${e.message}`;
    }
    if (e.status === 504 || e.status === 408) {
      return "Analiz zaman aşımına uğradı. Tekrar denemek isteğin sırasını ikiye katlar; önce bir süre bekleyin.";
    }
    return e.message;
  }
  return "Analiz tamamlanamadı.";
}

/** Bir oranı yüzdeye çevirir. */
const pct = (x: number) => `%${Math.round(x * 100)}`;

/**
 * Tamamlanmış bir raporun görünümü.
 *
 * İki kural pazarlık konusu değil ve ikisi de types.ts'te yazılı:
 * `score: null` sıfır değildir, ve `overall_score` kapsamsız gösterilmez.
 * Birincisi sessizliği başarısızlık diye puanlar, ikincisi 0.9 kapsamdaki 68
 * ile 0.3 kapsamdaki 68'i aynı şey gibi gösterir.
 */
function Rapor({ assessment }: { assessment: Assessment }) {
  if (isRedacted(assessment)) {
    return (
      <article className="card mt-5 p-4">
        <h2 className="eyebrow">Rapor</h2>
        <p className="mt-2 text-sm">
          Bu raporun içeriği silindi. Puan ve kapsam kaydı duruyor, vaka metni ve
          kanıt alıntıları kaldırıldı.
        </p>
      </article>
    );
  }

  const b = breakdown(assessment.criteria_snapshot, assessment.findings);

  return (
    <article className="card mt-5 p-4 rapor">
      <header
        className="flex flex-wrap items-start justify-between gap-4 pb-4"
        style={{ borderBottom: "1px solid var(--line)" }}
      >
        <div className="min-w-0">
          <h2 className="font-display text-base font-semibold truncate">
            {assessment.subject_title}
          </h2>
          <p className="mono text-xs mt-1" style={{ color: "var(--text-faint)" }}>
            {assessment.domain_name} · v{assessment.domain_version} ·{" "}
            {new Date(assessment.created_at).toLocaleString("tr-TR")}
          </p>
        </div>

        <div className="flex items-baseline gap-4 shrink-0">
          <div>
            <div className="eyebrow">Toplam</div>
            <div className="num font-display text-2xl">
              {b.overall === null ? "—" : b.overall.toFixed(1)}
            </div>
          </div>
          {/* Kapsam puanın yanından ayrılmaz: aynı sayı farklı kapsamlarda
              farklı bulgudur. */}
          <div>
            <div className="eyebrow">Kapsam</div>
            <div className="num font-display text-2xl">{pct(b.coverage)}</div>
          </div>
        </div>
      </header>

      {b.overall === null && (
        <div className="notice notice-warn mt-4">
          Bu vakada değerlendirilebilecek hiçbir kriter bulunamadı. Bu, sıfır puan
          değildir — metin rubriğin sorduğu şeylere dair kanıt taşımıyor.
        </div>
      )}

      <div className="mt-4 space-y-2">
        {b.rows.map((row) => (
          <details key={row.criterion.key} className="well p-3" open>
            <summary className="flex flex-wrap items-center gap-x-3 gap-y-1 cursor-pointer">
              <span className="font-medium text-sm flex-1 min-w-0">
                {row.criterion.label}
              </span>

              <span className="mono text-xs num" style={{ color: "var(--text-faint)" }}>
                ağırlık {row.criterion.weight.toFixed(2)}
              </span>

              {row.scored && row.points !== null ? (
                <>
                  <span className="pill pill-brand num">
                    {row.clamped} /{" "}
                    {row.criterion.scale_max > 0 ? row.criterion.scale_max : 5}
                  </span>
                  {/* Katkı kolonu: bu sayıların toplamı yukarıdaki toplam
                      puana eşittir. Raporun savunulabilir olmasının sebebi. */}
                  <span className="mono text-xs num" style={{ color: "var(--text-dim)" }}>
                    +{row.points.toFixed(1)}
                  </span>
                </>
              ) : (
                <span className="pill pill-warn">kanıt yok</span>
              )}
            </summary>

            <div className="mt-3 space-y-2">
              {row.criterion.description && (
                <p className="text-xs" style={{ color: "var(--text-faint)" }}>
                  {row.criterion.description}
                </p>
              )}

              {row.finding?.rationale && (
                <p className="text-sm" style={{ color: "var(--text-dim)" }}>
                  {row.finding.rationale}
                </p>
              )}

              {row.finding && row.finding.evidence.length > 0 ? (
                <ul className="space-y-1.5">
                  {row.finding.evidence.map((q, i) => (
                    // Birebir alıntı, parafraz değil: parafraz kaynağa karşı
                    // doğrulanamaz, ve doğrulanamayan bir atıf sağlam göründüğü
                    // için hiç atıf olmamasından kötüdür.
                    <li
                      key={i}
                      className="text-sm pl-3"
                      style={{
                        borderLeft: "2px solid var(--brand-line)",
                        color: "var(--text)",
                      }}
                    >
                      &ldquo;{q}&rdquo;
                    </li>
                  ))}
                </ul>
              ) : (
                !row.scored && (
                  <p className="text-xs" style={{ color: "var(--text-faint)" }}>
                    Metinde bu kritere dair bir ifade bulunamadı. Kanıtsız kriterler
                    toplamdan düşer ve kapsam sayısında raporlanır — düşük puan
                    olarak sayılmaz.
                  </p>
                )
              )}
            </div>
          </details>
        ))}
      </div>

      <footer
        className="mt-4 pt-3 flex flex-wrap gap-x-4 gap-y-1 mono text-xs"
        style={{ borderTop: "1px solid var(--line)", color: "var(--text-faint)" }}
      >
        <span>model {assessment.model}</span>
        <span className="num">{(assessment.latency_ms / 1000).toFixed(1)} sn</span>
        <span className="num">
          {assessment.prompt_tokens} + {assessment.completion_tokens} token
        </span>
        {/* Gizlenmiyor: şemayı tutturamamış bir cevap operatörün bilmesi
            gereken bir şey, ve rapor yine de okunabilir kalıyor. */}
        {!assessment.schema_valid && <span style={{ color: "var(--warn)" }}>şema onarıldı</span>}
      </footer>
    </article>
  );
}
