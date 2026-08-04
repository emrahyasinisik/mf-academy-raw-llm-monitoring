// Şirket panelinin rota tablosu — tek doğruluk kaynağı.
//
// Sidebar bunu okuyup çiziyor, sayfa başlığı bunu okuyup yazıyor. İkisi ayrı
// yerlerde tutulsaydı bir bölüm eklemek iki dosyayı düzenlemek olurdu ve biri
// her seferinde unutulurdu.
//
// JSX'ten uzak tutuluyor çünkü bu projedeki test koşucusu yalnızca
// src/lib/*.test.ts dosyalarını çalıştırıyor: burada durması, rota mantığının
// test edilebilir tek biçimi.

export type OrgSection = "ozet" | "ekip" | "kullanim" | "aktivite";

export const ORG_SECTIONS: readonly {
  id: OrgSection;
  label: string;
  path: string;
}[] = [
  { id: "ozet", label: "Özet", path: "/sirket" },
  { id: "ekip", label: "Ekip", path: "/sirket/ekip" },
  { id: "kullanim", label: "Kullanım", path: "/sirket/kullanim" },
  { id: "aktivite", label: "Aktivite", path: "/sirket/aktivite" },
];

/** Adres çubuğundaki yol → hangi bölüm açık. Tanınmayan yol Özet'e düşer. */
export function sectionFromPath(pathname: string): OrgSection {
  const clean = pathname.replace(/\/+$/, "");
  const found = ORG_SECTIONS.find((s) => s.path === clean);
  return found ? found.id : "ozet";
}
