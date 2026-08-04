// Panelin rota tablosu — tek doğruluk kaynağı.
//
// Sidebar bunu okuyup çiziyor, sayfa başlığı bunu okuyup yazıyor, ve eski
// hash rotalarının yönlendirmesi de burada. Üçü ayrı yerlerde tutulsaydı bir
// bölüm eklemek üç dosyayı düzenlemek olurdu ve biri her seferinde unutulurdu.
//
// JSX'ten uzak tutuluyor çünkü bu projedeki test koşucusu yalnızca
// src/lib/*.test.ts dosyalarını çalıştırıyor: burada durması, rota mantığının
// test edilebilir tek biçimi.

export type PanelSection =
  | "genel"
  | "hesaplar"
  | "belgeler"
  | "denetim"
  | "model"
  | "mcp"
  | "metrikler"
  | "loglar";

export const PANEL_SECTIONS: readonly {
  id: PanelSection;
  label: string;
  path: string;
}[] = [
  { id: "genel", label: "Genel", path: "/yonetim" },
  { id: "hesaplar", label: "Hesaplar", path: "/yonetim/hesaplar" },
  { id: "belgeler", label: "Belgeler", path: "/yonetim/belgeler" },
  { id: "denetim", label: "Denetim", path: "/yonetim/denetim" },
  { id: "model", label: "Model & Ayarlar", path: "/yonetim/model" },
  { id: "mcp", label: "MCP Sunucuları", path: "/yonetim/mcp" },
  { id: "metrikler", label: "Metrikler", path: "/yonetim/metrikler" },
  { id: "loglar", label: "Log Monitörü", path: "/yonetim/loglar" },
];

/** Adres çubuğundaki yol → hangi bölüm açık. Tanınmayan yol Genel'e düşer. */
export function sectionFromPath(pathname: string): PanelSection {
  const clean = pathname.replace(/\/+$/, "");
  const found = PANEL_SECTIONS.find((s) => s.path === clean);
  return found ? found.id : "genel";
}

// Panel `#admin` hash'inde yaşarken paylaşılmış bağlantılar var. Silmek yerine
// eşliyoruz — bu reponun kuralı: nav'dan inen rota adreslenebilir kalır.
// `#metrics` de panele taşındı; eski ürün-nav bağlantısı aynı kuralı izler.
const LEGACY_TABS: Record<string, string> = {
  "": "/yonetim",
  overview: "/yonetim",
  model: "/yonetim/model",
  mcp: "/yonetim/mcp",
  logs: "/yonetim/loglar",
};

/** Eski hash → yeni yol. Panel dışı / bilinmeyen hash için null. */
export function legacyHashToPath(hash: string): string | null {
  const [view, tab = ""] = hash.replace(/^#/, "").split("/");
  if (view === "metrics") return "/yonetim/metrikler";
  if (view !== "admin") return null;
  return Object.hasOwn(LEGACY_TABS, tab) ? LEGACY_TABS[tab] : "/yonetim";
}
