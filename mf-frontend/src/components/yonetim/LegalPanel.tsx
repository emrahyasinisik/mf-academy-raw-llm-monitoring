"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { RichText } from "@/components/ui/RichText";
import type { LegalListItem, LegalSlugDetail } from "@/lib/types";

const SLUGS = ["kosullar", "gizlilik"] as const;

export function LegalPanel() {
  const [list, setList] = useState<LegalListItem[]>([]);
  const [slug, setSlug] = useState<(typeof SLUGS)[number]>("kosullar");
  const [detail, setDetail] = useState<LegalSlugDetail | null>(null);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [reconsent, setReconsent] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);

  const loadList = useCallback(async () => {
    const res = await api.admin.legal.list();
    setList(res.documents);
  }, []);

  const loadSlug = useCallback(async (s: string) => {
    const d = await api.admin.legal.get(s);
    setDetail(d);
    if (d.draft) {
      setTitle(d.draft.title);
      setBody(d.draft.body);
    } else if (d.history[0]) {
      setTitle(d.history[0].title);
      setBody(d.history[0].body);
    } else {
      setTitle("");
      setBody("");
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        await loadList();
        if (!cancelled) await loadSlug(slug);
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof ApiError ? e.message : "Belgeler yüklenemedi.");
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [slug, loadList, loadSlug]);

  async function saveDraft() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api.admin.legal.saveDraft(slug, title, body);
      setNotice("Taslak kaydedildi.");
      await loadList();
      await loadSlug(slug);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Taslak kaydedilemedi.");
    } finally {
      setBusy(false);
    }
  }

  async function publish() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      // Yayın taslaktan okunur — kaydetmeden basmak eski taslağı yayınlar.
      await api.admin.legal.saveDraft(slug, title, body);
      const pub = await api.admin.legal.publish(slug, reconsent);
      setNotice(
        reconsent
          ? `Yayınlandı (versiyon ${pub.version}) — herkes yeniden onaylayacak.`
          : `Yayınlandı (versiyon ${pub.version}).`,
      );
      setReconsent(false);
      await loadList();
      await loadSlug(slug);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Yayınlanamadı.");
    } finally {
      setBusy(false);
    }
  }

  async function discardDraft() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api.admin.legal.deleteDraft(slug);
      setNotice("Taslak silindi.");
      await loadList();
      await loadSlug(slug);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Taslak silinemedi.");
    } finally {
      setBusy(false);
    }
  }

  const summary = list.find((d) => d.slug === slug);

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg">Belgeler</h1>
        <p className="mt-1 text-sm" style={{ color: "var(--muted)" }}>
          Gizlilik ve kullanım koşulları. Yayın append-only; yeniden onay
          istenirse versiyon artar.
        </p>
      </div>

      <div className="flex flex-wrap gap-2">
        {SLUGS.map((s) => (
          <button
            key={s}
            type="button"
            className="btn"
            aria-pressed={slug === s}
            onClick={() => setSlug(s)}
          >
            {s === "kosullar" ? "Koşullar" : "Gizlilik"}
            {list.find((d) => d.slug === s)?.has_draft ? " · taslak" : ""}
          </button>
        ))}
      </div>

      {summary && (
        <p className="text-sm" style={{ color: "var(--muted)" }}>
          Son yayın: {summary.version || "—"}
          {summary.published_at
            ? ` · ${new Date(summary.published_at).toLocaleString("tr-TR")}`
            : ""}
        </p>
      )}

      {error && (
        <p className="notice text-sm" role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className="text-sm" role="status">
          {notice}
        </p>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-3">
          <label className="block text-sm">
            Başlık
            <input
              className="input mt-1 w-full"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          </label>
          <label className="block text-sm">
            Gövde (Markdown)
            <textarea
              className="input mt-1 w-full min-h-[28rem] font-mono text-xs"
              value={body}
              onChange={(e) => setBody(e.target.value)}
            />
          </label>
          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              checked={reconsent}
              onChange={(e) => setReconsent(e.target.checked)}
            />
            <span>
              Yeniden onay iste — versiyon artar; herkes onay kapısına döner.
              Yazım düzeltmesi için işaretlemeyin.
            </span>
          </label>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              className="btn"
              disabled={busy}
              onClick={() => void saveDraft()}
            >
              Taslağı kaydet
            </button>
            <button
              type="button"
              className="btn"
              disabled={busy}
              onClick={() => void publish()}
            >
              Yayınla
            </button>
            {detail?.draft && (
              <button
                type="button"
                className="btn"
                disabled={busy}
                onClick={() => void discardDraft()}
              >
                Taslağı at
              </button>
            )}
          </div>
        </div>

        <div>
          <p className="eyebrow">Önizleme</p>
          <div
            className="mt-2 p-4 text-sm"
            style={{ border: "1px solid var(--line)" }}
          >
            <h2 className="text-lg">{title || "—"}</h2>
            <div className="mt-3">
              <RichText text={body} />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
