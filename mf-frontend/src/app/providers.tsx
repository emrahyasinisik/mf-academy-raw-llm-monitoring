"use client";

// Client-side provider tree. Kept out of the server layout so the layout can
// stay a Server Component while context (which needs the client) lives here.
//
// Machine sits inside Auth because every call it makes is authenticated: probing
// the model list before a session exists would only ever return 401 and paint
// the host as offline.

import { AuthProvider } from "@/store/auth";
import { MachineProvider } from "@/store/machine";
import type { ReactNode } from "react";

export function Providers({ children }: { children: ReactNode }) {
  return (
    <AuthProvider>
      <MachineProvider>{children}</MachineProvider>
    </AuthProvider>
  );
}
