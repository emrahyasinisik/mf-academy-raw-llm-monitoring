"use client";

// Master view: Auth. Contains two subviews — Login and Register — swapped
// client-side without navigation.
//
// Written in Turkish like every other screen. It used to be the one view in
// English, which meant the first thing a visitor read was in a different
// language from everything they read after signing in.

import { useState } from "react";
import { useAuth } from "@/store/auth";
import { ApiError } from "@/lib/api";
import { CriterionContinuum } from "@/components/ui/CriterionContinuum";
import { GizlilikView } from "./GizlilikView";
import { KosullarView } from "./KosullarView";

type SubView = "login" | "register";

/** Which standalone document is showing over the auth form, if any. */
type DocView = "gizlilik" | "kosullar" | null;

const COPY: Record<SubView, { tab: string; heading: string; blurb: string; cta: string }> =
  {
    login: {
      tab: "Giriş",
      heading: "Tekrar hoş geldin",
      blurb: "Hesabınla oturum aç, persona ile ilk geçişi başlat.",
      cta: "Giriş yap",
    },
    register: {
      tab: "Kayıt",
      heading: "Hesap oluştur",
      blurb: "Hesap aç; canlı araştırmayla ilk-geçiş okumasına başla.",
      cta: "Hesabı oluştur",
    },
  };

export function AuthView() {
  const { login, register } = useAuth();
  const [sub, setSub] = useState<SubView>("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [accepted, setAccepted] = useState(false);
  // AuthView renders in place of AppShell's hash router while there is no
  // signed-in user, so a signed-out visitor following `#gizlilik` or
  // `#kosullar` never reaches that router. This local state is the same deep
  // links handled here instead: read once on mount so a direct load of either
  // hash lands on that document immediately rather than flashing the login
  // form first.
  const [showDoc, setShowDoc] = useState<DocView>(() => {
    if (typeof window === "undefined") return null;
    if (window.location.hash === "#gizlilik") return "gizlilik";
    if (window.location.hash === "#kosullar") return "kosullar";
    return null;
  });

  const copy = COPY[sub];

  if (showDoc) {
    return (
      <div>
        <div className="mx-auto max-w-2xl px-4 sm:px-5 pt-6">
          <button
            className="btn btn-ghost btn-sm"
            onClick={() => {
              setShowDoc(null);
              window.history.replaceState(
                null,
                "",
                window.location.pathname + window.location.search,
              );
            }}
          >
            ← Geri
          </button>
        </div>
        {showDoc === "kosullar" ? <KosullarView /> : <GizlilikView />}
      </div>
    );
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      if (sub === "login") await login(email, password);
      else await register(email, password, name, accepted);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Bir şeyler ters gitti.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="min-h-screen grid lg:grid-cols-[1.15fr_1fr]">
      {/* Brand panel — one composition: brand, one thesis, one supporting line,
          signature continuum. Hidden below lg so the form stays above the fold. */}
      <div
        className="hidden lg:flex flex-col justify-between p-12 xl:p-16 relative overflow-hidden"
        style={{ borderRight: "1px solid var(--line)" }}
      >
        <div
          className="absolute inset-0 pointer-events-none auth-wash-breathe"
          aria-hidden
          style={{
            background:
              "radial-gradient(620px 440px at 22% 38%, var(--brand-wash), transparent 72%), radial-gradient(480px 360px at 78% 72%, var(--steel-wash), transparent 70%)",
          }}
        />

        <div
          className="absolute inset-0 pointer-events-none auth-grid-drift"
          aria-hidden
          style={{
            backgroundImage:
              "linear-gradient(var(--line) 1px, transparent 1px), linear-gradient(90deg, var(--line) 1px, transparent 1px)",
            backgroundSize: "64px 64px",
            opacity: 0.4,
            maskImage:
              "radial-gradient(720px 520px at 28% 36%, #000 0%, transparent 78%)",
            WebkitMaskImage:
              "radial-gradient(720px 520px at 28% 36%, #000 0%, transparent 78%)",
          }}
        />

        <div
          className="flex items-center gap-3.5 relative reveal-up"
          style={{ ["--i" as string]: 0 }}
        >
          <span
            className="grid place-items-center w-11 h-11 rounded-[var(--r-sm)] font-bold text-sm mono"
            style={{
              background: "linear-gradient(180deg, var(--brand-hi), var(--brand-lo))",
              color: "var(--brand-ink)",
              boxShadow: "var(--bevel), var(--shadow-2)",
            }}
          >
            MF
          </span>
          <span className="font-display text-2xl tracking-tight">MasterFabric</span>
        </div>

        <div className="max-w-xl relative">
          <p
            className="eyebrow reveal-up mb-4"
            style={{ ["--i" as string]: 1 }}
          >
            Yatırım personası
          </p>
          <h1
            className="font-display text-[3.1rem] xl:text-[3.5rem] font-bold leading-[1.02] tracking-tight reveal-up"
            style={{ ["--i" as string]: 2 }}
          >
            İlk geçişte
            <br />
            <span style={{ color: "var(--brand)" }}>kaynaklı okuma.</span>
          </h1>
          <p
            className="mt-6 text-[1.02rem] leading-7 max-w-md reveal-up"
            style={{ color: "var(--text-dim)", ["--i" as string]: 3 }}
          >
            Konuyu verirsiniz; persona canlı araştırma yapar, gerekirse sorar
            ve kaynaklarıyla birlikte bir ilk-geçiş okuması sunar. Karar sizde
            kalır.
          </p>

          <div
            className="mt-10 reveal-up"
            style={{ ["--i" as string]: 4 }}
          >
            <CriterionContinuum count={12} mode="wave" />
            <p
              className="mono text-[0.65rem] mt-3 tracking-wider uppercase"
              style={{ color: "var(--text-faint)" }}
            >
              araştırma · kaynak · karar sende
            </p>
          </div>
        </div>

        <ul
          className="relative reveal-up flex flex-col gap-2.5 mono text-[0.7rem] tracking-wider uppercase"
          style={{ ["--i" as string]: 5, color: "var(--text-faint)" }}
        >
          {["Canlı araştırma", "Kaynak gösterilir", "Veri sizde kalır"].map(
            (t) => (
              <li key={t} className="flex items-center gap-2.5">
                <span
                  aria-hidden
                  className="block w-3 h-px shrink-0"
                  style={{ background: "var(--brand)" }}
                />
                {t}
              </li>
            ),
          )}
        </ul>
      </div>

      {/* Form panel */}
      <div className="flex items-center justify-center p-6 sm:p-10 relative">
        <div
          className="absolute inset-0 pointer-events-none lg:hidden"
          aria-hidden
          style={{
            background:
              "radial-gradient(420px 280px at 50% 0%, var(--brand-wash), transparent 70%)",
          }}
        />

        <div className="w-full max-w-sm view-in relative">
          <div className="flex items-center gap-2.5 mb-3 lg:hidden">
            <span
              className="grid place-items-center w-8 h-8 rounded-[var(--r-xs)] font-bold text-[0.7rem] mono"
              style={{
                background: "linear-gradient(180deg, var(--brand-hi), var(--brand-lo))",
                color: "var(--brand-ink)",
                boxShadow: "var(--bevel), var(--shadow-1)",
              }}
            >
              MF
            </span>
            <span className="font-display font-semibold">MasterFabric</span>
          </div>
          <p
            className="lg:hidden font-display text-xl font-bold tracking-tight mb-7 leading-snug"
          >
            İlk geçişte{" "}
            <span style={{ color: "var(--brand)" }}>aynı ölçüt.</span>
          </p>

          <div
            className="inline-flex p-1 rounded-[var(--r-sm)] mb-7"
            style={{
              background: "var(--bg-sunk)",
              border: "1px solid var(--line)",
              boxShadow: "inset 0 1px 2px rgba(0,0,0,.4)",
            }}
          >
            {(Object.keys(COPY) as SubView[]).map((s) => {
              const on = sub === s;
              return (
                <button
                  key={s}
                  type="button"
                  onClick={() => {
                    setSub(s);
                    setError(null);
                  }}
                  aria-pressed={on}
                  className="px-4 py-1.5 rounded-[var(--r-xs)] text-sm font-semibold"
                  style={{
                    background: on
                      ? "linear-gradient(180deg, var(--brand-hi), var(--brand))"
                      : "transparent",
                    color: on ? "var(--brand-ink)" : "var(--text-dim)",
                    boxShadow: on ? "var(--bevel), var(--shadow-1)" : undefined,
                    transition:
                      "background var(--dur-2) var(--ease), color var(--dur-2) var(--ease)",
                  }}
                >
                  {COPY[s].tab}
                </button>
              );
            })}
          </div>

          <div key={sub} className="view-in">
            <h2 className="font-display text-2xl font-bold tracking-tight mb-1.5">
              {copy.heading}
            </h2>
            <p className="text-sm mb-7" style={{ color: "var(--text-dim)" }}>
              {copy.blurb}
            </p>
          </div>

          <form onSubmit={submit} className="space-y-4">
            {sub === "register" && (
              <div className="view-in">
                <label className="label" htmlFor="auth-name">
                  Ad
                </label>
                <input
                  id="auth-name"
                  className="input"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="Ada Lovelace"
                  autoComplete="name"
                />
              </div>
            )}
            <div>
              <label className="label" htmlFor="auth-email">
                E-posta
              </label>
              <input
                id="auth-email"
                className="input"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="sen@masterfabric.co"
                autoComplete="email"
                required
              />
            </div>
            <div>
              <label className="label" htmlFor="auth-password">
                Parola
              </label>
              <input
                id="auth-password"
                className="input"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="En az 8 karakter"
                autoComplete={sub === "login" ? "current-password" : "new-password"}
                minLength={8}
                required
              />
            </div>

            {sub === "register" && (
              <label
                className="flex items-start gap-2 text-xs view-in"
                style={{ color: "var(--text-faint)" }}
              >
                <input
                  type="checkbox"
                  checked={accepted}
                  onChange={(e) => setAccepted(e.target.checked)}
                  className="mt-0.5"
                />
                <span>
                  <a
                    href="#kosullar"
                    onClick={() => setShowDoc("kosullar")}
                    style={{ color: "var(--brand)", textDecoration: "underline" }}
                  >
                    Kullanım koşullarını
                  </a>{" "}
                  kabul ediyorum ve{" "}
                  <a
                    href="#gizlilik"
                    onClick={() => setShowDoc("gizlilik")}
                    style={{ color: "var(--brand)", textDecoration: "underline" }}
                  >
                    aydınlatma metnini
                  </a>{" "}
                  okudum.
                </span>
              </label>
            )}

            {error && (
              <div className="notice notice-bad view-in" role="alert">
                {error}
              </div>
            )}

            <button
              type="submit"
              className="btn btn-primary w-full"
              disabled={busy || (sub === "register" && !accepted)}
            >
              {busy ? "Bekle…" : copy.cta}
            </button>
          </form>

          <p className="mt-4 text-xs" style={{ color: "var(--text-faint)" }}>
            <a href="#gizlilik" onClick={() => setShowDoc("gizlilik")}>
              Verilerinizi nasıl sakladığımızı okuyun.
            </a>
          </p>
        </div>
      </div>
    </div>
  );
}
