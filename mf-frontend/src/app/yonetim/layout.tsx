import type { Metadata } from "next";
import { PanelGate } from "@/components/yonetim/PanelGate";

// Sekme başlığı ürününkinden ayrı: operatörün açık duran iki sekmesi
// birbirinden ayırt edilebilmeli.
export const metadata: Metadata = {
  title: "Yönetim — MasterFabric",
};

export default function YonetimLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <PanelGate>{children}</PanelGate>;
}
