"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { RichText } from "@/components/ui/RichText";
import type { LegalDocument } from "@/lib/types";

// Metin artık repoda değil: GET /legal/gizlilik. Seed deploy'da yoksa boş
// sayfa — o yüzden hata görünür olmalı.
export function GizlilikView() {
  return <LegalPublicView slug="gizlilik" />;
}

export function KosullarView() {
  return <LegalPublicView slug="kosullar" />;
}

function LegalPublicView({ slug }: { slug: string }) {
  const [doc, setDoc] = useState<LegalDocument | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const d = await api.legal.get(slug);
        if (!cancelled) {
          setDoc(d);
          setError(null);
        }
      } catch (e) {
        if (!cancelled) {
          setDoc(null);
          setError(
            e instanceof ApiError
              ? e.message
              : "Belge yüklenemedi.",
          );
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [slug]);

  return (
    <div className="mx-auto max-w-2xl px-4 sm:px-5 py-6">
      {error && (
        <p className="notice text-sm" role="alert">
          {error}
        </p>
      )}
      {!error && !doc && (
        <p className="text-sm" style={{ color: "var(--muted)" }}>
          Yükleniyor…
        </p>
      )}
      {doc && (
        <>
          <h1 className="text-lg">{doc.title}</h1>
          <div className="mt-4 text-sm">
            <RichText text={doc.body.replace(/^#\s.+\n+/, "")} />
          </div>
        </>
      )}
    </div>
  );
}
