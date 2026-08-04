import assert from "node:assert/strict";
import { test } from "node:test";
import { assemblePersonaCase } from "./personaCase.ts";

test("puts topic in subject_title and keeps purpose section", () => {
  const { subject_title, subject } = assemblePersonaCase({
    topic: "Acme AI",
    purpose: "seed değerlendirme",
    userReplies: ["B2B SaaS"],
    lastAssistantBody: "Pazar büyüyor.",
    sources: [{ title: "Haber", url: "https://example.com" }],
    budgetChars: 10_000,
  });
  assert.equal(subject_title, "Acme AI");
  assert.match(subject, /## Konu\nAcme AI/);
  assert.match(subject, /## Amaç\nseed değerlendirme/);
  assert.match(subject, /## Kaynaklar/);
});

test("truncates middle chat before dropping konu/amaç/kaynaklar", () => {
  const long = "x".repeat(500);
  const { subject } = assemblePersonaCase({
    topic: "T",
    purpose: "P",
    userReplies: [long, long, long],
    lastAssistantBody: long,
    sources: [{ title: "S", url: "https://s.test" }],
    budgetChars: 400,
  });
  assert.ok(subject.length <= 400);
  assert.match(subject, /## Konu\nT/);
  assert.match(subject, /## Amaç\nP/);
  assert.match(subject, /## Kaynaklar/);
});
