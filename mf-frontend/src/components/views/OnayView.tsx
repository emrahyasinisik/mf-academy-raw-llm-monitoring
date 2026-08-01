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
    <div className="mx-auto max-w-2xl px-4 sm:px-5 py-6">
      <h1 className="text-lg">Devam etmeden önce</h1>
      <p className="mt-2 text-sm">
        Bu hesap, kullanım koşulları yayımlanmadan önce açılmış. Devam etmek
        için koşulları kabul etmeniz ve aydınlatma metnini okumanız gerekiyor.
      </p>

      <div className="mt-6 space-y-6">
        <KosullarView />
        <GizlilikView />
      </div>

      {error && <div className="notice mt-4" role="status">{error}</div>}

      <div className="flex items-center gap-3 mt-6">
        <button
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
          Kabul ediyorum
        </button>
        <button className="btn btn-ghost" onClick={() => logout()}>
          Çıkış yap
        </button>
      </div>
    </div>
  );
}
