"use client";

// orgGate'in kararını ekrana çeviren yer. Karar mantığı burada değil
// src/lib/orgAccess.ts'te — orası test edilebilir, burası değil.
//
// Yetkisiz kullanıcıya "yetkin yok" mesajı yok: sessizce /'ye yönlenir.
// PanelGate ile aynı kalıp, ayrı dosya — yönetim bileşenlerini import etmiyoruz.

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/store/auth";
import { orgGate } from "@/lib/orgAccess";
import { OrgShell } from "./OrgShell";
import { OrgLogin } from "./OrgLogin";

export function OrgGate({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();
  const router = useRouter();
  const state = orgGate({ loading, user });

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

  if (state === "login") return <OrgLogin />;

  return <OrgShell>{children}</OrgShell>;
}
