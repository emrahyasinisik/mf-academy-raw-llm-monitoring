"use client";

// AppShell is the client-side router for the SPA. It holds the active master
// view in state, syncs it to the URL hash (so views are shareable/back-button
// friendly) and swaps views in place with no full-page reload.

import { useEffect, useState } from "react";
import { useAuth } from "@/store/auth";
import { AuthView } from "./views/AuthView";
import { PlaygroundView } from "./views/PlaygroundView";
import { DashboardView } from "./views/DashboardView";

export type MasterView = "playground" | "dashboard";

const NAV: { id: MasterView; label: string; icon: string }[] = [
  { id: "playground", label: "LLM Playground", icon: "◑" },
  { id: "dashboard", label: "Monitoring", icon: "▤" },
];

export function AppShell() {
  const { user, loading, logout } = useAuth();
  const [view, setView] = useState<MasterView>("playground");

  // Sync the active view <-> URL hash (#dashboard) for client-side routing.
  useEffect(() => {
    const fromHash = () => {
      const h = window.location.hash.replace("#", "");
      if (h === "playground" || h === "dashboard") setView(h);
    };
    fromHash();
    window.addEventListener("hashchange", fromHash);
    return () => window.removeEventListener("hashchange", fromHash);
  }, []);

  const go = (v: MasterView) => {
    setView(v);
    window.location.hash = v;
  };

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

  // Master view 1: Auth (with login/register subviews) — shown when logged out.
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
                {n.label}
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
        {view === "playground" ? <PlaygroundView /> : <DashboardView />}
      </main>
    </div>
  );
}
