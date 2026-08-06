import assert from "node:assert/strict";
import { test } from "node:test";
import { assemblePersonaCase, parseIntake } from "./personaCase.ts";

test("parseIntake extracts Konu/Amaç and leaves the rest", () => {
  const { topic, purpose, rest } = parseIntake(
    "Konu: Acme AI\nAmaç: seed değerlendirme\n\nBüyüme nedir?",
  );
  assert.equal(topic, "Acme AI");
  assert.equal(purpose, "seed değerlendirme");
  assert.equal(rest, "Büyüme nedir?");
});

test("parseIntake returns empty intake when headers absent", () => {
  const { topic, purpose, rest } = parseIntake("Sadece soru");
  assert.equal(topic, "");
  assert.equal(purpose, "");
  assert.equal(rest, "Sadece soru");
});

test("titles the case from the first user bubble", () => {
  const { subject_title, subject } = assemblePersonaCase({
    userReplies: ["hepsiburada.com için hangi platformlarda reklam yapsak?"],
    lastAssistantBody: "Meta ve Google.",
    sources: [{ title: "Haber", url: "https://example.com" }],
    budgetChars: 10_000,
  });
  assert.equal(
    subject_title,
    "hepsiburada.com için hangi platformlarda reklam yapsak?",
  );
  assert.match(subject, /## Konu\nhepsiburada\.com/);
  assert.doesNotMatch(subject, /## Amaç/);
  assert.match(subject, /## Kaynaklar/);
});

test("truncates middle chat before dropping konu/kaynaklar", () => {
  const long = "x".repeat(500);
  const { subject } = assemblePersonaCase({
    userReplies: [long, long, long],
    lastAssistantBody: long,
    sources: [{ title: "S", url: "https://s.test" }],
    budgetChars: 400,
  });
  assert.ok(subject.length <= 400);
  assert.match(subject, /## Konu\n/);
  assert.match(subject, /## Kaynaklar/);
});
