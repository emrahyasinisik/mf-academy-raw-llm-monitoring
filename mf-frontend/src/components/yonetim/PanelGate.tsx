"use client";

// panelGate'in kararını ekrana çeviren yer. Karar mantığı burada değil
// src/lib/adminAccess.ts'te — orası test edilebilir, burası değil.

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/store/auth";
import { panelGate } from "@/lib/adminAccess";
import { PanelShell } from "./PanelShell";
import { PanelLogin } from "./PanelLogin";

export function PanelGate({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();
  const router = useRouter();
  const state = panelGate({ loading, user });

  // Yönlendirme render sırasında değil bir efektte: render sırasında router'a
  // dokunmak React'in aynı geçişte başka bir bileşeni güncellemesi demek.
  useEffect(() => {
    if (state === "redirect") router.replace("/");
  }, [state, router]);

  if (state === "booting" || state === "redirect") {
    return (
      <div className="min-h-screen grid place-items-center">
        <span
          className="mono text-xs tracking-wider uppercase"
          style={{ color: "var(--text-faint)" }}
        >
          oturum çözümleniyor
        </span>
      </div>
    );
  }

  if (state === "login") return <PanelLogin />;

  return <PanelShell>{children}</PanelShell>;
}
