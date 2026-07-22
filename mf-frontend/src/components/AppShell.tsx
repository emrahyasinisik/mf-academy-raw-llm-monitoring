"use client";

// AppShell is the client-side router for the SPA. It owns the two-level
// navigation state — master view plus the active subview — and mirrors it into
// the URL hash as `#master/subview`, so any view is shareable and the browser's
// back button works, with no full-page reload.

import { useCallback, useEffect, useState } from "react";
import { useAuth } from "@/store/auth";
import { AuthView } from "./views/AuthView";
import { PlaygroundView } from "./views/PlaygroundView";
import { DashboardView } from "./views/DashboardView";
import { ScoringView } from "./views/ScoringView";

export type MasterView = "playground" | "dashboard" | "scoring";

const NAV: { id: MasterView; label: string; icon: string }[] = [
  { id: "playground", label: "LLM Playground", icon: "◑" },
  { id: "dashboard", label: "Monitoring", icon: "▤" },
  { id: "scoring", label: "Decision Scoring", icon: "◆" },
];

const isMaster = (v: string): v is MasterView => NAV.some((n) => n.id === v);

// An unknown or missing subview is left empty on purpose — each master view
// falls back to its own default, so the router never needs to know their names.
function parseHash(): { view: MasterView; sub: string } | null {
  const [v, s] = window.location.hash.replace(/^#/, "").split("/");
  return isMaster(v) ? { view: v, sub: s ?? "" } : null;
}

// Reads the hash during the initial render rather than in an effect, so a deep
// link paints the right view immediately instead of flashing the default one.
// Safe under SSR: the shell renders its loading state until auth resolves, so
// the server and the client agree on the first paint.
function initialRoute(): { view: MasterView; sub: string } {
  const parsed = typeof window === "undefined" ? null : parseHash();
  return parsed ?? { view: "playground", sub: "" };
}

export function AppShell() {
  const { user, loading, logout } = useAuth();
  const [view, setView] = useState<MasterView>(() => initialRoute().view);
  const [sub, setSub] = useState(() => initialRoute().sub);

  // The hash is the single source of truth: navigation handlers only write to
  // it, and this listener is what actually moves the app — which is also what
  // makes the browser's back button work for free.
  useEffect(() => {
    const sync = () => {
      const parsed = parseHash();
      if (parsed) {
        setView(parsed.view);
        setSub(parsed.sub);
      }
    };
    window.addEventListener("hashchange", sync);
    return () => window.removeEventListener("hashchange", sync);
  }, []);

  const go = (v: MasterView) => {
    window.location.assign(`#${v}`);
  };

  // Subviews are addressable too, so a deep link lands on the right tab.
  const goSub = useCallback(
    (s: string) => {
      window.location.assign(`#${view}/${s}`);
    },
    [view],
  );

  if (loading) {
    return (
      <div className="min-h-screen grid place-items-center">
        <div
          className="mono text-sm animate-pulse-soft"
          style={{ color: "var(--text-dim)" }}
        >
          loading session…
        </div>
      </div>
    );
  }

  // Auth master view (with its own login/register subviews) — shown logged out.
  if (!user) return <AuthView />;

  return (
    <div className="min-h-screen flex flex-col">
      <header
        className="flex items-center justify-between px-5 h-14 border-b"
        style={{ borderColor: "var(--border)", background: "var(--bg-elev)" }}
      >
        <div className="flex items-center gap-6">
          <div className="flex items-center gap-2">
            <span
              className="grid place-items-center w-7 h-7 rounded-md font-bold text-sm"
              style={{ background: "var(--accent)", color: "#06122b" }}
            >
              MF
            </span>
            <span className="font-semibold text-sm hidden sm:block">
              Raw LLM Monitoring
            </span>
          </div>
          <nav className="flex items-center gap-1">
            {NAV.map((n) => (
              <button
                key={n.id}
                onClick={() => go(n.id)}
                className="px-3 py-1.5 rounded-lg text-sm font-medium transition-colors"
                style={{
                  background:
                    view === n.id ? "var(--accent-soft)" : "transparent",
                  color: view === n.id ? "var(--text)" : "var(--text-dim)",
                }}
              >
                <span className="mr-1.5 opacity-70">{n.icon}</span>
                <span className="hidden sm:inline">{n.label}</span>
              </button>
            ))}
          </nav>
        </div>
        <div className="flex items-center gap-3">
          <span
            className="text-xs mono hidden md:block"
            style={{ color: "var(--text-faint)" }}
          >
            {user.email}
          </span>
          <button className="btn btn-ghost !py-1.5 !px-3" onClick={logout}>
            Sign out
          </button>
        </div>
      </header>

      <main className="flex-1 min-h-0">
        {view === "playground" && <PlaygroundView sub={sub} onSub={goSub} />}
        {view === "dashboard" && <DashboardView sub={sub} onSub={goSub} />}
        {view === "scoring" && <ScoringView sub={sub} onSub={goSub} />}
      </main>
    </div>
  );
}
