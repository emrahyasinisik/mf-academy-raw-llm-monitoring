"use client";

import { useState } from "react";
import { useAuth } from "@/store/auth";
import { GizlilikView } from "./GizlilikView";
import { KosullarView } from "./KosullarView";

// Kabul etmemis mevcut kullanicilar icin kapi.
//
// Cikis yolu var ve olmak zorunda: tek cikisi kabul etmek olan bir ekran,
// kabul degildir.
export function OnayView() {
  const { acceptTerms, logout } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

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

      <div className="flex-1 mx-auto w-full max-w-2xl px-4 sm:px-5 py-8 view-in">
        <p className="eyebrow">Kabul</p>
        <h1 className="font-display text-2xl sm:text-3xl font-bold tracking-tight mt-2">
          Devam etmeden önce
        </h1>
        <p className="mt-3 text-sm leading-relaxed" style={{ color: "var(--text-dim)" }}>
          Bu hesap, kullanım koşulları yayımlanmadan önce açılmış. Devam etmek
          için koşulları kabul etmeniz ve aydınlatma metnini okumanız gerekiyor.
        </p>

        <div
          className="mt-7 p-5 sm:p-6 space-y-8 [&>div]:mx-0 [&>div]:max-w-none [&>div]:px-0 [&>div]:py-0"
          style={{
            background: "var(--panel)",
            border: "1px solid var(--line)",
            borderRadius: "var(--r-lg)",
            boxShadow: "var(--bevel), var(--shadow-1)",
          }}
        >
          <KosullarView />
          <div style={{ borderTop: "1px solid var(--line)" }} />
          <GizlilikView />
        </div>

        {error && (
          <div className="notice notice-bad mt-4" role="alert">
            {error}
          </div>
        )}
      </div>

      <div
        className="sticky bottom-0 shrink-0 px-4 sm:px-5 py-3.5 flex flex-wrap items-center gap-3 glass"
        style={{ borderTop: "1px solid var(--line)" }}
      >
        <div className="mx-auto w-full max-w-2xl flex flex-wrap items-center gap-3">
          <button
            type="button"
            className="btn btn-primary"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              setError("");
              try {
                await acceptTerms();
              } catch {
                setError("Kabul kaydedilemedi. Tekrar deneyin.");
                setBusy(false);
              }
            }}
          >
            {busy ? "Kaydediliyor…" : "Kabul ediyorum"}
          </button>
          <p className="text-xs" style={{ color: "var(--text-faint)" }}>
            Kabul etmeden uygulamaya giremezsiniz; çıkış her zaman açık.
          </p>
        </div>
      </div>
    </div>
  );
}
