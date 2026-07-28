import type { Metadata, Viewport } from "next";
import { Inter, JetBrains_Mono, Space_Grotesk } from "next/font/google";
import "./globals.css";
import { Providers } from "./providers";

// Three faces, three jobs, all self-hosted by next/font — no request leaves for
// Google at runtime, and the metric overrides mean no layout shift when they
// swap in.
//
// latin-ext is not optional here. The interface is Turkish, and ğ, ı and ş live
// in that subset while ç, ö and ü do not — omit it and every other word renders
// half in the webfont and half in a fallback.

// Display: mechanical, slightly engineered letterforms. Chosen over another
// grotesque because the product is an instrument panel, and its headings should
// not look like the body text set larger.
const spaceGrotesk = Space_Grotesk({
  subsets: ["latin", "latin-ext"],
  variable: "--font-space-grotesk",
  display: "swap",
});

// Body and UI: neutral on purpose, so the display face and the numbers are the
// only things with a voice.
const inter = Inter({
  subsets: ["latin", "latin-ext"],
  variable: "--font-inter",
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
  title: "MasterFabric — Üreteç ve Karar Konsolu",
  description:
    "Kendi barındırdığın modeli çalıştır, ürettiği ekranları standarda göre denetle, çıkarım sunucusunun telemetrisini izle.",
};

export const viewport: Viewport = {
  // Matches --bg, so the mobile browser chrome continues the canvas instead of
  // capping it with a white bar.
  themeColor: "#0a0e13",
  colorScheme: "dark",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html
      lang="tr"
      className={`h-full ${inter.variable} ${spaceGrotesk.variable} ${jetbrainsMono.variable}`}
    >
      <body className="min-h-full">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
