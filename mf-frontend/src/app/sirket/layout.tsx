import type { Metadata } from "next";
import { OrgGate } from "@/components/sirket/OrgGate";

// Sekme başlığı ürününkinden ve yönetimden ayrı: operatörün açık duran
// sekmeleri birbirinden ayırt edilebilmeli.
export const metadata: Metadata = {
  title: "Şirket — MasterFabric",
  robots: { index: false, follow: false },
};

export default function SirketLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <OrgGate>{children}</OrgGate>;
}
