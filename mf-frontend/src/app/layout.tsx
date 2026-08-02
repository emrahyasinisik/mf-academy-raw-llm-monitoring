import type { Metadata, Viewport } from "next";
import { IBM_Plex_Sans, JetBrains_Mono, Syne } from "next/font/google";
import "./globals.css";
import { Providers } from "./providers";

// Three faces, three jobs, all self-hosted by next/font — no request leaves for
// Google at runtime, and the metric overrides mean no layout shift when they
// swap in.
//
// latin-ext is not optional here. The interface is Turkish, and ğ, ı and ş live
// in that subset while ç, ö and ü do not — omit it and every other word renders
// half in the webfont and half in a fallback.

// Display: geometric, slightly condensed — the ledger's headline voice.
// Chosen over a generic grotesque so product headings carry presence without
// borrowing Inter's default SaaS look.
const syne = Syne({
  subsets: ["latin", "latin-ext"],
  variable: "--font-syne",
  display: "swap",
});

// Body and UI: IBM Plex — professional without being the Inter default every
// dark dashboard ships with.
const plex = IBM_Plex_Sans({
  subsets: ["latin", "latin-ext"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-plex",
  display: "swap",
});

// Data: every latency, token count and model id in the app. Tabular by default,
// with a slashed zero — this is a screen where 0 and O appear in the same string.
const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin", "latin-ext"],
  variable: "--font-jetbrains-mono",
  display: "swap",
});

export const metadata: Metadata = {
  title: "MasterFabric — Rubrik Analiz Konsolu",
  description:
    "Vakayı girin, tanımlı rubriğe göre kriter kriter puanlanmış ve kanıtı gösterilen bir rapor alın. İlk geçiş taramasında tutarlılık — karar sizin.",
};

export const viewport: Viewport = {
  themeColor: "#070b10",
  colorScheme: "dark",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html
      lang="tr"
      className={`h-full ${plex.variable} ${syne.variable} ${jetbrainsMono.variable}`}
    >
      <body className="min-h-full">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
