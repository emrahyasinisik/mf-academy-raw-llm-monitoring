"use client";

// AppShell is the client-side router for the SPA. It owns the active master
// view and mirrors it into the URL hash, so any view is shareable and the
// browser's back button works, with no full-page reload.
//
// A view that has been opened stays mounted and is hidden rather than removed.
// That is not a performance tweak: generation on the box takes tens of seconds,
// and swapping the view out unmounted the component holding the request, so its
// result had nowhere to land. The work itself never stopped — the run was
// still recorded — but from the chair it looked exactly like leaving the tab
// had killed the job. The persona conversation was lost the same way.

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/store/auth";
import { needsTermsGate } from "@/lib/terms";
import { needsPasswordGate } from "@/lib/passwordGate";
import { legacyHashToPath } from "@/lib/adminNav";
import { AnalizView } from "./views/AnalizView";
import { AuthView } from "./views/AuthView";
import { OnayView } from "./views/OnayView";
import { ParolaView } from "./views/ParolaView";
import { CodegenView } from "./views/CodegenView";
import { PersonaView } from "./views/PersonaView";
import { MetricsView } from "./views/MetricsView";
import { GizlilikView } from "./views/GizlilikView";
import { KosullarView } from "./views/KosullarView";
import { StatusRail } from "./ui/StatusRail";

export type MasterView =
  | "analiz" | "codegen" | "persona" | "metrics" | "gizlilik" | "kosullar";

// Analiz leads because it is the product: a case goes in, a rubric-scored and
// auditable report comes out. The order used to be the generator's, and the
// reasoning was "it is what the box serves" — a sort by whatever weights were
// loaded. That put the product second, and which model is loaded is not a
// question nav order should be answering.
//
// The persona stays: it is a working surface, and it fails legibly (it reports
// the inference host, it does not crash) when the machine is off.
//
// Yönetim artık burada değil: kendi rotasında, kendi kabuğunda (/yonetim).
// Nav'dan çıkması bir gizleme değil, bir ayrım — ürün ekranları bir vakayı
// değerlendirmek için, panel sistemi işletmek için, ve ikisi aynı başlık
// çubuğunu paylaşınca hangisinde olduğun kayboluyordu. Panele giden bağlantı
// header'da, yalnızca yönetici rolüne görünür.
// Metrics stays in the product nav and remains listed for everyone, with the
// view explaining the role it needs. It is separate from the /yonetim panel
// because it reads from the metrics store rather than the database — when the
// inference box is off, this goes quiet while the admin panel keeps working,
// and a tab that empties for reasons the panel does not share belongs here.
const NAV: { id: MasterView; label: string; Icon: () => React.ReactElement }[] = [
  { id: "analiz", label: "Analiz", Icon: IconRubric },
  { id: "persona", label: "Persona", Icon: IconSpark },
  { id: "metrics", label: "Metrikler", Icon: IconChart },
];

// Nav'da olmayan ama adreslenebilen rotalar. isMaster bugüne kadar NAV
// üyeliğine bakıyordu; gizlilik bir çalışma aracı değil, bir belge — nav'a
// girmesi orayı sulandırır, ama derin bağlantının çalışması gerekiyor.
//
// `codegen` buraya 2 Ağu 2026'da indi, silinerek değil. Flutter ekran üreteci
// çalışıyor ve bozulmadı; kaldırılma sebebi ürün odağı. Kod üretiminde rakip
// büyük modeller, ve 6 GB kartta 4B modelle o kavga kaybediliyor — CLAUDE.md
// "best model" iddiasını zaten yasaklıyor. Yatırım tarafının eksenleri (rubrik
// şeffaflığı, veri egemenliği, tutarlılık) ise rakibin model boyutuyla
// erimiyor. Ölçüm de bunu destekliyordu: adapter'ın kazancı ev stilinde
// (clean 81 → 95.2%), kanıt okuma iddiası doğrulanmamıştı.
//
// Rota adreslenebilir kaldı — #codegen hâlâ açılıyor — çünkü geri getirmenin
// maliyeti bu dizide bir satır olsun istiyoruz, ve gömülü bağlantısı olan
// kimse 404 görmesin.
const OFF_NAV: MasterView[] = ["gizlilik", "kosullar", "codegen"];

const isMaster = (v: string): v is MasterView =>
  NAV.some((n) => n.id === v) || (OFF_NAV as string[]).includes(v);

/** True when the reader has asked for less animation. Safe before hydration. */
function reducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

// Pane holds a view that has been opened, whether or not it is the one on
// screen. `hidden` is display:none, which takes the subtree out of layout and
// out of the accessibility tree while React keeps its state — the point being
// that a request in flight still has a component to resolve into.
//
// The entrance is played through the Web Animations API rather than a CSS class,
// and that is forced by the line above: a keyframe class only runs on mount, and
// anything that would re-trigger it on re-show (a changing `key`, a remount)
// would throw away the state this component exists to preserve.
function Pane({ active, children }: { active: boolean; children: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!active || !ref.current || reducedMotion()) return;
    ref.current.animate(
      [
        { opacity: 0, transform: "translateY(12px)", filter: "blur(3px)" },
        { opacity: 1, transform: "none", filter: "blur(0)" },
      ],
      { duration: 380, easing: "cubic-bezier(.16,1,.3,1)" },
    );
  }, [active]);

  return (
    <div ref={ref} className={active ? "h-full" : "hidden"}>
      {children}
    </div>
  );
}

// Any extra hash segment is ignored: AppShell owns only the master view now.
function parseHash(): MasterView | null {
  const [v] = window.location.hash.replace(/^#/, "").split("/");
  return isMaster(v) ? v : null;
}

// Reads the hash during the initial render rather than in an effect, so a deep
// link paints the right view immediately instead of flashing the default one.
// Safe under SSR: the shell renders its loading state until auth resolves, so
// the server and the client agree on the first paint.
function initialRoute(): MasterView {
  const parsed = typeof window === "undefined" ? null : parseHash();
  return parsed ?? "analiz";
}

export function AppShell() {
  const { user, loading, logout } = useAuth();
  const router = useRouter();

  // Where we are, and everywhere we have been — one piece of state, because
  // they change together and only ever from one place.
  //
  // `opened` is the set of working views that have been shown at least once.
  // Mounting them all up front would be simpler, but each has work to do on
  // mount, and a visitor who only ever opens the generator should not pay for
  // the persona. Opened once, they stay — that is what keeps a generation
  // started before the switch alive to return to.
  //
  // It lives here rather than in its own state updated by an effect. "Set state
  // when `view` changed" is an effect that exists only to run a second render
  // pass after the first one already knew the answer; folding it into the
  // transition that moves the route computes it once, in the same update.
  const [route, setRoute] = useState<{
    view: MasterView;
    opened: ReadonlySet<MasterView>;
  }>(() => {
    const first = initialRoute();
    return { view: first, opened: new Set([first]) };
  });
  const { view, opened } = route;

  // #admin ölmedi, taşındı. Bağlantısı paylaşılmış olan kimse 404 görmesin —
  // bu reponun kuralı: nav'dan inen rota adreslenebilir kalır. Yönlendirme
  // hem ilk yüklemede hem sonraki hash değişimlerinde çalışıyor, çünkü ikisi
  // de aynı bağlantıya tıklamanın sonucu olabiliyor.
  useEffect(() => {
    const jump = () => {
      const target = legacyHashToPath(window.location.hash);
      if (target) router.replace(target);
    };
    jump();
    window.addEventListener("hashchange", jump);
    return () => window.removeEventListener("hashchange", jump);
  }, [router]);

  // The hash is the single source of truth: navigation handlers only write to
  // it, and this listener is what actually moves the app — which is also what
  // makes the browser's back button work for free.
  useEffect(() => {
    const sync = () => {
      const parsed = parseHash();
      if (!parsed) return;
      setRoute((prev) => ({
        view: parsed,
        // A new Set only when it would actually differ, so navigating back to a
        // view already open does not hand every consumer a fresh reference.
        opened: prev.opened.has(parsed)
          ? prev.opened
          : new Set(prev.opened).add(parsed),
      }));
    };
    window.addEventListener("hashchange", sync);
    return () => window.removeEventListener("hashchange", sync);
  }, []);

  const go = (v: MasterView) => {
    window.location.assign(`#${v}`);
  };

  if (loading) return <Booting />;

  // Auth master view (with its own login/register subviews) — shown logged out.
  if (!user) return <AuthView />;

  // Geçici parola ile ürün de koşullar da açılmaz: kullanıcı önce kalıcı parolayı
  // seçer, sonra diğer kapılar aynı sunucu kullanıcısı üzerinden değerlendirilir.
  if (needsPasswordGate(user)) return <ParolaView />;

  // Oturum var ama kabul yok: uygulamayi degil kapiyi goster. AuthView ile ayni
  // dallanma sekli, ayni yerde, cunku ikisi de "henuz uygulamaya giremez"in
  // farkli sebepleri.
  if (needsTermsGate(user)) return <OnayView />;

  return (
    <div className="min-h-screen flex flex-col">
      <a href="#main" className="skip-link">
        İçeriğe geç
      </a>

      <header
        className="sticky top-0 z-20 glass"
        style={{ borderBottom: "1px solid var(--line)" }}
      >
        <div className="flex items-center justify-between gap-4 px-4 sm:px-5 h-14">
          <div className="flex items-center gap-5 min-w-0">
            <Wordmark />
            <MasterNav active={view} onSelect={go} />
          </div>

          <div className="flex items-center gap-2.5 shrink-0">
            {user.role === "admin" && (
              <a
                href="/yonetim"
                className="btn btn-ghost btn-sm"
                title="Yönetim paneli"
              >
                Yönetim
              </a>
            )}
            <span
              className="text-xs mono hidden md:block max-w-[180px] truncate"
              style={{ color: "var(--text-faint)" }}
              title={user.email}
            >
              {user.email}
            </span>
            <button className="btn btn-ghost btn-sm" onClick={logout}>
              Çıkış
            </button>
          </div>
        </div>
      </header>

      {/* The one instrument that is true on every screen. Directly under the
          header because it describes the machine the whole app drives, not the
          view that happens to be open. */}
      <StatusRail />

      {/* main is a column with two rows: the view, and the footer under it.
          Before this it was one box with the footer as the last child, and that
          worked only for the views that flow — Metrikler, Yönetim, Gizlilik.
          Analiz, Üreteç and Persona all open with `h-full`, which fills main
          exactly, so the footer was laid out after a child that had already
          consumed the entire height and landed below the fold with nothing to
          scroll it into view. The link in it is the only route to the privacy
          page for a signed-in user, and it was missing on the product screen.

          The view row owns the scroll now. That is what lets the footer stay a
          sibling: the flowing views scroll inside the row instead of
          overflowing main, and the `h-full` views still measure against a
          definite height because `flex-1 min-h-0` gives the row one. Their own
          inner scrollers sit exactly at that height and never overflow it, so
          no second scrollbar appears. */}
      <main id="main" className="flex-1 min-h-0 flex flex-col">
        <div className="flex-1 min-h-0 overflow-y-auto">
          {/* `hidden` and not a conditional render: React keeps the subtree and
              its state alive, so an in-flight generation still has a component
              to return to, and each view keeps its scroll position. The inactive
              ones are display:none, so they cost nothing to lay out. */}
          {/* Kalıcı mount edilen grupta, Üreteç ve Persona ile birlikte: bir
              analiz tünelin ardındaki makinede onlarca saniye sürüyor, ve view
              söküldüğünde isteği tutan bileşen de gidiyor — iş durmuyor, kayıt
              yine yazılıyor, ama sonucun ineceği yer kalmıyor. */}
          {opened.has("analiz") && (
            <Pane active={view === "analiz"}>
              <AnalizView />
            </Pane>
          )}
          {opened.has("codegen") && (
            <Pane active={view === "codegen"}>
              <CodegenView />
            </Pane>
          )}
          {opened.has("persona") && (
            <Pane active={view === "persona"}>
              <PersonaView />
            </Pane>
          )}
          {/* The two read-only screens are still torn down and rebuilt, and that
              is the right default: they start no work that can be interrupted,
              and every one of their numbers ages. Kept mounted, a chart opened
              this morning would still be on screen tonight, silently. */}
          {view === "metrics" && <MetricsView />}
          {view === "gizlilik" && <GizlilikView />}
          {view === "kosullar" && <KosullarView />}
        </div>

        <footer
          className="shrink-0 px-4 sm:px-5 py-2.5 text-xs flex flex-wrap gap-x-1"
          style={{ color: "var(--text-faint)", borderTop: "1px solid var(--line)" }}
        >
          <a
            href="#gizlilik"
            className="hover:text-[var(--text-dim)] transition-colors"
            style={{ transitionDuration: "var(--dur-2)" }}
          >
            Verileriniz ve gizlilik
          </a>
          <span aria-hidden> · </span>
          <a
            href="#kosullar"
            className="hover:text-[var(--text-dim)] transition-colors"
            style={{ transitionDuration: "var(--dur-2)" }}
          >
            Kullanım koşulları
          </a>
        </footer>
      </main>
    </div>
  );
}

/**
 * The master nav, with a single indicator that travels between items rather than
 * one indicator per item fading in and out. The moving element is what makes the
 * master navigation legible: it shows that these items are one control.
 *
 * The position is measured rather than computed from index, because the labels
 * are different widths and they change width again when the webfont swaps in.
 */
function MasterNav({
  active,
  onSelect,
}: {
  active: MasterView;
  onSelect: (v: MasterView) => void;
}) {
  const wrap = useRef<HTMLDivElement>(null);
  const items = useRef(new Map<MasterView, HTMLButtonElement>());
  const [box, setBox] = useState<{ left: number; width: number } | null>(null);

  useLayoutEffect(() => {
    const measure = () => {
      const el = items.current.get(active);
      const parent = wrap.current;
      if (!el || !parent) return;
      setBox({
        left: el.offsetLeft,
        width: el.offsetWidth,
      });
    };
    measure();

    // Re-measure when the labels reflow: a webfont swap, a viewport change, or
    // the labels appearing at the sm breakpoint all move the target.
    const ro = new ResizeObserver(measure);
    if (wrap.current) ro.observe(wrap.current);
    document.fonts?.ready.then(measure).catch(() => {});
    return () => ro.disconnect();
  }, [active]);

  return (
    <nav ref={wrap} className="relative flex items-center gap-0.5">
      {NAV.map(({ id, label, Icon }) => {
        const on = id === active;
        return (
          <button
            key={id}
            ref={(el) => {
              if (el) items.current.set(id, el);
              else items.current.delete(id);
            }}
            onClick={() => onSelect(id)}
            aria-current={on ? "page" : undefined}
            className="relative flex items-center gap-2 px-2.5 sm:px-3 h-9 rounded-[var(--r-sm)] text-sm font-medium"
            style={{
              color: on ? "var(--text)" : "var(--text-dim)",
              transition:
                "color var(--dur-2) var(--ease), background var(--dur-2) var(--ease), transform var(--dur-1) var(--ease)",
              background: on ? "var(--panel-2)" : "transparent",
            }}
            onMouseEnter={(e) => {
              if (!on) e.currentTarget.style.background = "var(--panel-2)";
            }}
            onMouseLeave={(e) => {
              if (!on) e.currentTarget.style.background = "transparent";
            }}
          >
            <span
              style={{
                color: on ? "var(--brand)" : "inherit",
                display: "flex",
                transition: "color var(--dur-2) var(--ease), transform var(--dur-2) var(--ease)",
                transform: on ? "scale(1.05)" : undefined,
              }}
            >
              <Icon />
            </span>
            <span className="hidden sm:inline">{label}</span>
          </button>
        );
      })}

      {/* Travelling indicator — copper underline that slides between items. */}
      {box && (
        <span
          aria-hidden
          className="absolute -bottom-[7px] h-0.5 rounded-full"
          style={{
            left: 0,
            width: box.width,
            background: "linear-gradient(90deg, var(--brand-lo), var(--brand-hi))",
            boxShadow: "0 0 8px color-mix(in srgb, var(--brand) 28%, transparent)",
            transform: `translateX(${box.left}px)`,
            transition:
              "transform var(--dur-3) var(--ease-out), width var(--dur-3) var(--ease-out)",
          }}
        />
      )}
    </nav>
  );
}

function Wordmark() {
  return (
    <div className="flex items-center gap-2.5 shrink-0">
      <span
        className="grid place-items-center w-7 h-7 rounded-[var(--r-xs)] font-bold text-[0.7rem] mono"
        style={{
          background: "linear-gradient(180deg, var(--brand-hi), var(--brand-lo))",
          color: "var(--brand-ink)",
          boxShadow: "var(--bevel), var(--shadow-1)",
        }}
      >
        MF
      </span>
      <span className="font-display text-sm hidden lg:block tracking-tight">
        MasterFabric
      </span>
    </div>
  );
}

/** The pre-session state. A rail of its own, so the first paint is not blank. */
function Booting() {
  return (
    <div className="min-h-screen grid place-items-center">
      <div className="flex flex-col items-center gap-4 view-in">
        <span className="relative">
          <span className="boot-ring" aria-hidden />
          <span
            className="grid place-items-center w-10 h-10 rounded-[var(--r-sm)] font-bold text-xs mono"
            style={{
              background: "linear-gradient(180deg, var(--brand-hi), var(--brand-lo))",
              color: "var(--brand-ink)",
              boxShadow: "var(--bevel), var(--shadow-2)",
            }}
          >
            MF
          </span>
        </span>
        <span className="mono text-xs tracking-wider uppercase" style={{ color: "var(--text-faint)" }}>
          oturum çözümleniyor
        </span>
      </div>
    </div>
  );
}

/* Icons ----------------------------------------------------------------
   Inline and hand-drawn on a 16px grid rather than pulled from a set: four
   glyphs do not justify a dependency, and these are stroked to match the
   1px hairlines the panels are built from. */

const SVG = {
  width: 16,
  height: 16,
  viewBox: "0 0 16 16",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.5,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

function IconRubric() {
  return (
    <svg {...SVG} aria-hidden>
      <path d="M2.5 3.5h11v9h-11z" />
      <path d="M5 6.5h6M5 9.5h4" />
    </svg>
  );
}

// Üreteç nav'dan çıktığı için çağıranı kalmadı. Silinmedi: rota OFF_NAV'da
// adreslenebilir duruyor ve geri gelmesi NAV dizisine bir satır olsun istiyoruz
// — ikonu da silmek o satırı yeniden çizmek demek olurdu.
// eslint-disable-next-line @typescript-eslint/no-unused-vars
function IconCode() {
  return (
    <svg {...SVG} aria-hidden>
      <path d="M5.5 4.5 2 8l3.5 3.5M10.5 4.5 14 8l-3.5 3.5" />
    </svg>
  );
}

function IconSpark() {
  return (
    <svg {...SVG} aria-hidden>
      <path d="M8 1.75 9.7 6.3 14.25 8 9.7 9.7 8 14.25 6.3 9.7 1.75 8 6.3 6.3z" />
    </svg>
  );
}

function IconChart() {
  return (
    <svg {...SVG} aria-hidden>
      <path d="M2 13V3M2 13h12" />
      <path d="m4.5 10.5 3-3.5 2.5 2 3.5-4.5" />
    </svg>
  );
}

