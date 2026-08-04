"use client";

// The operator's cockpit. Four modules over the control-plane API.
//
// Gated twice, and the two gates do different jobs. The role check here decides
// what to render; the backend's RequireRole decides what is allowed. Only the
// second one is a security boundary — this one exists so a non-admin is not
// shown controls that would only ever return 403. Never rely on it for access:
// anything the browser knows, the browser's user can change.

import { useAuth } from "@/store/auth";
import { Segmented } from "@/components/ui/Segmented";
import { RoleGate } from "@/components/ui/RoleGate";
import { OverviewPanel } from "@/components/yonetim/OverviewPanel";
import { ModelPanel } from "@/components/yonetim/ModelPanel";
import { MCPPanel } from "@/components/yonetim/MCPPanel";
import { LogsPanel } from "@/components/yonetim/LogsPanel";

type Tab = "overview" | "model" | "mcp" | "logs";

const TABS: { id: Tab; label: string }[] = [
  { id: "overview", label: "Genel" },
  { id: "model", label: "Model & Ayarlar" },
  { id: "mcp", label: "MCP Sunucuları" },
  { id: "logs", label: "Log Monitörü" },
];

export function AdminView({
  sub,
  onNavigate,
}: {
  sub: string;
  onNavigate: (s: string) => void;
}) {
  const { user } = useAuth();
  const tab = (TABS.find((t) => t.id === sub)?.id ?? "overview") as Tab;

  if (user?.role !== "admin") {
    return (
      <RoleGate title="Bu bölüm yönetici hesabı gerektiriyor">
        Yönetici rolü API üzerinden verilmiyor. Sunucunun{" "}
        <code className="mono" style={{ color: "var(--text)" }}>
          ADMIN_EMAIL
        </code>{" "}
        değişkeninde adı geçen hesap, servis yeniden başladığında yükseltilir. Rol
        veren bir endpoint olsaydı, ileride çıkacak herhangi bir kimlik doğrulama
        açığı doğrudan tam yetkiye dönüşürdü.
      </RoleGate>
    );
  }

  return (
    <div className="max-w-6xl mx-auto p-4 sm:p-5 space-y-5">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h2 className="font-display text-xl font-semibold tracking-tight">Yönetim</h2>
        <Segmented
          items={TABS}
          active={tab}
          onSelect={(t) => onNavigate(t)}
          label="Yönetim bölümü"
          size="sm"
        />
      </div>

      {/* Keyed so switching tabs replays the entrance: these panels are torn
          down and rebuilt anyway, and without the key the new one would appear
          instantly while every other transition in the app animates. */}
      <div key={tab} className="view-in">
        {tab === "overview" && <OverviewPanel />}
        {tab === "model" && <ModelPanel />}
        {tab === "mcp" && <MCPPanel />}
        {tab === "logs" && <LogsPanel />}
      </div>
    </div>
  );
}
