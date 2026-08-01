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
      blurb: "Konsoluna giriş yap.",
      cta: "Giriş yap",
    },
    register: {
      tab: "Kayıt",
      heading: "Hesap oluştur",
      blurb: "Birkaç saniyede başla.",
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
  // form first. One state rather than two independent booleans — two booleans
  // would leave undefined behaviour when both are set, and only one document
  // is ever on screen at a time.
  const [showDoc, setShowDoc] = useState<DocView>(() => {
    if (typeof window === "undefined") return null;
    if (window.location.hash === "#gizlilik") return "gizlilik";
    if (window.location.hash === "#kosullar") return "kosullar";
    return null;
  });

  const copy = COPY[sub];

  if (showDoc) {
    return (
      // Kabı yok: GizlilikView ve KosullarView artık kendi kenar boşluklarını
      // taşıyor, ve buradaki p-6 onların üstüne binip çift boşluk yapıyordu.
      // Kalan tek iş, "Geri"yi metnin soluyla aynı hizaya oturtmak — o yüzden
      // düğme aynı ölçüleri tekrarlayan kendi kabında.
      <div>
        <div className="mx-auto max-w-2xl px-4 sm:px-5 pt-6">
          <button
            className="btn btn-ghost btn-sm"
            onClick={() => {
              setShowDoc(null);
              // Hash de temizleniyor, yoksa "#gizlilik" ya da "#kosullar" adres
              // çubuğunda kalıyor ve giriş başarılı olduğu anda AppShell'in
              // initialRoute'u onu okuyup kullanıcıyı Analiz yerine o belgeye
              // indiriyor. replaceState kullanılıyor: assign("#") geçmişe bir
              // adım daha ekler ve geri tuşu aynı yere geri döndürür.
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
      // On success the AppShell re-renders into the app automatically.
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Bir şeyler ters gitti.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="min-h-screen grid lg:grid-cols-[1.1fr_1fr]">
      {/* ---- brand panel ----
          Hidden below lg: on a phone it would push the form off the fold, and
          the form is the only thing anyone came here to use. */}
      <div
        className="hidden lg:flex flex-col justify-between p-12 relative overflow-hidden"
        style={{ borderRight: "1px solid var(--line)" }}
      >
        {/* Soft brand wash under the grid — breathes so the left panel isn't
            a static void while the form does the work. */}
        <div
          className="absolute inset-0 pointer-events-none auth-wash-breathe"
          aria-hidden
          style={{
            background:
              "radial-gradient(520px 380px at 18% 42%, var(--brand-wash), transparent 70%)",
          }}
        />

        {/* A hairline grid, at the same weight as the panel borders throughout
            the app. It is the one decorative surface here and it is doing the
            job the product's own panels do: making the field read as machined
            rather than empty. Slow drift keeps it from reading as wallpaper. */}
        <div
          className="absolute inset-0 pointer-events-none auth-grid-drift"
          aria-hidden
          style={{
            backgroundImage:
              "linear-gradient(var(--line) 1px, transparent 1px), linear-gradient(90deg, var(--line) 1px, transparent 1px)",
            backgroundSize: "56px 56px",
            opacity: 0.35,
            maskImage:
              "radial-gradient(680px 480px at 22% 34%, #000 0%, transparent 78%)",
            WebkitMaskImage:
              "radial-gradient(680px 480px at 22% 34%, #000 0%, transparent 78%)",
          }}
        />

        <div className="flex items-center gap-2.5 relative item-in" style={{ ["--i" as string]: 0 }}>
          <span
            className="grid place-items-center w-8 h-8 rounded-[var(--r-xs)] font-bold text-xs mono"
            style={{
              background: "linear-gradient(180deg, var(--brand-hi), var(--brand-lo))",
              color: "var(--brand-ink)",
              boxShadow: "var(--bevel), var(--shadow-1)",
            }}
          >
            MF
          </span>
          <span className="font-display font-semibold">MasterFabric Academy</span>
        </div>

        <div className="max-w-lg relative view-in">
          <h1 className="font-display text-[2.75rem] font-bold leading-[1.08] tracking-tight">
            Üreteç ve
            <br />
            Karar Konsolu
          </h1>
          <p
            className="mt-5 text-[0.95rem] leading-7"
            style={{ color: "var(--text-dim)" }}
          >
            Kendi barındırdığın modeli çalıştır, ürettiği ekranları projenin kod
            standardına göre denetle, çıkarım sunucusunun telemetrisini tek yerden
            izle.
          </p>
          <div className="mt-8 flex flex-wrap gap-2">
            {[
              "Kendi barındırılan çıkarım",
              "Go + Postgres API",
              "Canlı telemetri",
            ].map((t, i) => (
              <span
                key={t}
                className="pill item-in"
                style={{ ["--i" as string]: i + 2 }}
              >
                {t}
              </span>
            ))}
          </div>
        </div>

        <div
          className="text-xs mono relative item-in"
          style={{ color: "var(--text-faint)", ["--i" as string]: 5 }}
        >
          Next.js SPA → Vercel · Go → Render
        </div>
      </div>

      {/* ---- form panel ---- */}
      <div className="flex items-center justify-center p-6">
        <div className="w-full max-w-sm view-in">
          {/* A segmented control, not two links: these are two modes of one
              form, and the switch should not look like navigation. */}
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

          {/* key={sub}: Giriş ↔ Kayıt swap re-triggers the entrance so the
              mode change is felt, not just text replacement. */}
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
