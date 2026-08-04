"use client";

// Panelin kabuğu: solda sabit bölüm listesi, üstte ince bir şerit, ortada
// bölümün kendisi.
//
// Ürün ekranlarıyla bilerek zıt. Orada yerleşim az ve nefes alan, çünkü
// okunan şey bir rapor; burada yoğun ve liste ağırlıklı, çünkü okunan şey bir
// sistemin durumu. Aynı token setini paylaşıyorlar (globals.css) — panel için
// ayrı tema açmak iki bakım yüzeyi demek olurdu ve panelin ürüne ait olduğu
// görünmeli.
//
// Ürün kabuğundaki header, StatusRail ve alt bilgi burada yok: StatusRail
// çıkarım makinesini anlatıyor ve bu ekranların hiçbiri ona bakmıyor.

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useAuth } from "@/store/auth";
import { PANEL_SECTIONS, sectionFromPath } from "@/lib/adminNav";

export function PanelShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const active = sectionFromPath(pathname);
  const { user, logout } = useAuth();
  const label = PANEL_SECTIONS.find((s) => s.id === active)?.label ?? "Genel";

  return (
    <div className="min-h-screen grid md:grid-cols-[220px_1fr]">
      <a href="#panel-main" className="skip-link">
        İçeriğe geç
      </a>

      <aside
        className="hidden md:flex flex-col gap-1 p-3"
        style={{
          borderRight: "1px solid var(--line)",
          background: "var(--bg-sunk)",
        }}
      >
        <Link href="/" className="flex items-center gap-2.5 px-2 py-2 mb-2">
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
          <span className="font-display text-sm tracking-tight">Yönetim</span>
        </Link>

        <nav className="flex flex-col gap-0.5" aria-label="Panel bölümleri">
          {PANEL_SECTIONS.map((s) => {
            const on = s.id === active;
            return (
              <Link
                key={s.id}
                href={s.path}
                aria-current={on ? "page" : undefined}
                className="px-2.5 py-2 rounded-[var(--r-sm)] text-sm"
                style={{
                  color: on ? "var(--text)" : "var(--text-dim)",
                  background: on ? "var(--panel-2)" : "transparent",
                  borderLeft: on
                    ? "2px solid var(--brand)"
                    : "2px solid transparent",
                  transition: "color var(--dur-2) var(--ease), background var(--dur-2) var(--ease)",
                }}
              >
                {s.label}
              </Link>
            );
          })}
        </nav>

        <div className="mt-auto pt-3">
          <Link
            href="/"
            className="block px-2.5 py-2 rounded-[var(--r-sm)] text-xs"
            style={{ color: "var(--text-faint)" }}
          >
            ← Uygulamaya dön
          </Link>
        </div>
      </aside>

      <div className="flex flex-col min-w-0">
        <header
          className="sticky top-0 z-20 glass flex items-center justify-between gap-4 px-4 sm:px-5 h-12"
          style={{ borderBottom: "1px solid var(--line)" }}
        >
          {/* Kırıntı yolu, mobilde sidebar gizliyken bölümün adını taşıyan tek
              yer — bu yüzden dekorasyon değil. */}
          <nav aria-label="Konum" className="text-xs min-w-0 truncate">
            <span style={{ color: "var(--text-faint)" }}>Yönetim</span>
            <span aria-hidden style={{ color: "var(--text-faint)" }}>
              {" / "}
            </span>
            <span style={{ color: "var(--text)" }}>{label}</span>
          </nav>

          <div className="flex items-center gap-2.5 shrink-0">
            <span
              className="text-xs mono hidden sm:block max-w-[180px] truncate"
              style={{ color: "var(--text-faint)" }}
              title={user?.email}
            >
              {user?.email}
            </span>
            <button className="btn btn-ghost btn-sm" onClick={logout}>
              Çıkış
            </button>
          </div>
        </header>

        {/* Mobilde sidebar yok; bölümler yatay bir şeride iniyor. Ayrı bir
            menü bileşeni açmıyoruz — dört bölüm bir satıra sığıyor. */}
        <nav
          className="md:hidden flex gap-1 overflow-x-auto scrollbar-thin px-3 py-2"
          style={{ borderBottom: "1px solid var(--line)" }}
          aria-label="Panel bölümleri"
        >
          {PANEL_SECTIONS.map((s) => (
            <Link
              key={s.id}
              href={s.path}
              aria-current={s.id === active ? "page" : undefined}
              className="px-2.5 py-1 rounded-[var(--r-xs)] text-xs whitespace-nowrap"
              style={{
                color: s.id === active ? "var(--text)" : "var(--text-dim)",
                background: s.id === active ? "var(--panel-2)" : "transparent",
              }}
            >
              {s.label}
            </Link>
          ))}
        </nav>

        <main id="panel-main" className="flex-1 min-h-0 p-4 sm:p-5">
          <h1 className="font-display text-xl font-semibold tracking-tight mb-4">
            {label}
          </h1>
          {children}
        </main>
      </div>
    </div>
  );
}
