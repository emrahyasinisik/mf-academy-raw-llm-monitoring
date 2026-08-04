"use client";

// Panelin kendi kapısı.
//
// Ayrı olan ekran, oturum değil: token seti ürünle aynı (mf_access /
// mf_refresh). İki ayrı set iki ayrı yenileme döngüsü demek olurdu ve 401
// sonrası hangisinin yenileneceği belirsizleşirdi; bir sekmede yapılan çıkış
// diğerinde yarım oturum bırakırdı.
//
// Kayıt bağlantısı yok ve bilerek yok: bu ekranın işi hesap açmak değil.

import { useState } from "react";
import Link from "next/link";
import { useAuth } from "@/store/auth";
import { ApiError } from "@/lib/api";

export function PanelLogin() {
  const { login } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await login(email, password);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Bir şeyler ters gitti.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="min-h-screen grid place-items-center p-5">
      <div className="w-full max-w-sm">
        <div className="flex items-center gap-3 mb-6">
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
          <div>
            <div className="font-display text-lg tracking-tight">Yönetim</div>
            <div className="text-xs" style={{ color: "var(--text-faint)" }}>
              MasterFabric
            </div>
          </div>
        </div>

        <form onSubmit={submit} className="card p-5 space-y-3.5 view-in">
          <label className="block">
            <span className="label">E-posta</span>
            <input
              className="input"
              type="email"
              autoComplete="username"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </label>

          <label className="block">
            <span className="label">Parola</span>
            <input
              className="input"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </label>

          {error && <div className="notice notice-bad">{error}</div>}

          <button className="btn btn-primary w-full" disabled={busy} type="submit">
            {busy ? "Kontrol ediliyor…" : "Giriş yap"}
          </button>
        </form>

        <div className="mt-4 text-xs" style={{ color: "var(--text-faint)" }}>
          <Link href="/" className="hover:text-[var(--text-dim)]">
            ← Uygulamaya dön
          </Link>
        </div>
      </div>
    </div>
  );
}
