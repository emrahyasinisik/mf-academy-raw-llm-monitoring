"use client";

// Client-side provider tree. Kept out of the server layout so the layout can
// stay a Server Component while context (which needs the client) lives here.

import { AuthProvider } from "@/store/auth";
import type { ReactNode } from "react";

export function Providers({ children }: { children: ReactNode }) {
  return <AuthProvider>{children}</AuthProvider>;
}
