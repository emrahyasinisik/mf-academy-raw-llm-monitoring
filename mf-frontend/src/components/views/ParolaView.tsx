"use client";

import { useState } from "react";
import { useAuth } from "@/store/auth";
import { ApiError } from "@/lib/api";

// İlk giriş kapısı: backend zaten ürünü 403 ile kapatıyor; bu ekran kullanıcıya
// aynı kuralı açıkça gösterip yeni token çiftini aldırır.
export function ParolaView() {
  const { user, changePassword, logout } = useAuth();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await changePassword(current, next);
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : "Parola değiştirilemedi. Tekrar deneyin.",
      );
      setBusy(false);
    }
  }

  return (
    <div className="min-h-screen flex flex-col">
      <header
        className="shrink-0 px-4 sm:px-5 h-14 flex items-center justify-between gap-3"
        style={{ borderBottom: "1px solid var(--line)" }}
      >
        <div className="flex items-center gap-2.5">
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
          <span className="font-display text-sm tracking-tight">MasterFabric</span>
        </div>
        <button type="button" className="btn btn-ghost btn-sm" onClick={() => logout()}>
          Çıkış yap
        </button>
      </header>

      <main className="flex-1 grid place-items-center px-4 sm:px-5 py-8">
        <div className="w-full max-w-sm view-in">
          <p className="eyebrow">Parola</p>
          <h1 className="font-display text-2xl sm:text-3xl font-bold tracking-tight mt-2">
            İlk girişte parolanızı değiştirmeniz gerekiyor.
          </h1>
          <p className="mt-3 text-sm leading-relaxed" style={{ color: "var(--text-dim)" }}>
            Yönetici tarafından verilen geçici parolayla oturum açtınız. Devam
            etmek için yalnızca sizin bildiğiniz yeni bir parola belirleyin.
          </p>

          <form
            onSubmit={submit}
            className="mt-7 p-5 sm:p-6 space-y-4"
            style={{
              background: "var(--panel)",
              border: "1px solid var(--line)",
              borderRadius: "var(--r-lg)",
              boxShadow: "var(--bevel), var(--shadow-1)",
            }}
          >
            <div>
              <label className="label" htmlFor="password-email">
                E-posta
              </label>
              <input
                id="password-email"
                className="input"
                type="email"
                value={user?.email ?? ""}
                readOnly
                autoComplete="username"
              />
            </div>

            <div>
              <label className="label" htmlFor="password-current">
                Mevcut parola
              </label>
              <input
                id="password-current"
                className="input"
                type="password"
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
                placeholder="Geçici parolanız"
                autoComplete="current-password"
                required
              />
            </div>

            <div>
              <label className="label" htmlFor="password-next">
                Yeni parola
              </label>
              <input
                id="password-next"
                className="input"
                type="password"
                value={next}
                onChange={(e) => setNext(e.target.value)}
                placeholder="En az 8 karakter"
                autoComplete="new-password"
                minLength={8}
                required
              />
            </div>

            {error && (
              <div className="notice notice-bad" role="alert">
                {error}
              </div>
            )}

            <button type="submit" className="btn btn-primary w-full" disabled={busy}>
              {busy ? "Kaydediliyor…" : "Parolayı değiştir"}
            </button>
          </form>
        </div>
      </main>
    </div>
  );
}
