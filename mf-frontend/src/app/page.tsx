import { AppShell } from "@/components/AppShell";

// The entire product is one page. AppShell is a client component that swaps
// master views in place (client-side routing, no full-page reloads) — the SPA
// requirement of the capstone.
export default function Home() {
  return <AppShell />;
}
