"use client";

// The screen a non-admin gets where an admin would get a panel.
//
// Shared by the two views that need it so they cannot drift into explaining the
// same rule two different ways. It is a courtesy, not a boundary: the server
// checks the role again on every request, and this exists only so the interface
// says why a section is closed instead of hiding it and leaving the reader to
// guess.

import type { ReactNode } from "react";

export function RoleGate({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <div className="max-w-2xl mx-auto p-5">
      <div className="card p-6 view-in" style={{ borderLeft: "3px solid var(--warn)" }}>
        <div className="flex items-center gap-2.5 mb-2.5">
          <span className="lamp" style={{ color: "var(--warn)" }} />
          <h2 className="font-display font-semibold">{title}</h2>
        </div>
        <p className="text-sm leading-relaxed" style={{ color: "var(--text-dim)" }}>
          {children}
        </p>
      </div>
    </div>
  );
}
