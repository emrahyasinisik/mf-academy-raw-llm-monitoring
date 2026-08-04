// Assembles the analysis/run `subject` from persona intake + chat transcript.
//
// Konu, Amaç and Kaynaklar are never dropped — only the middle chat summary
// shrinks when the rubric's character budget is exceeded. The backend scores
// against this text; truncating the fixed headers would hide what the operator
// asked for.

/** Restores intake from a first user turn that primed Konu/Amaç lines. */
export function parseIntake(content: string): {
  topic: string;
  purpose: string;
  rest: string;
} {
  const lines = content.split("\n");
  let i = 0;
  let topic = "";
  let purpose = "";

  if (lines[i]?.startsWith("Konu:")) {
    topic = lines[i].slice("Konu:".length).trim();
    i += 1;
  }
  if (lines[i]?.startsWith("Amaç:")) {
    purpose = lines[i].slice("Amaç:".length).trim();
    i += 1;
  }
  // Primed turns put a blank line between headers and the free-text ask.
  if (lines[i] === "") i += 1;

  return { topic, purpose, rest: lines.slice(i).join("\n") };
}

export type PersonaCaseInput = {
  topic: string;
  purpose: string;
  userReplies: string[];
  /** stripVerdictLines already applied by caller */
  lastAssistantBody: string;
  sources: { title: string; url: string }[];
  budgetChars: number;
};

function formatSources(sources: { title: string; url: string }[]): string {
  if (sources.length === 0) return "";
  return sources
    .map((s, i) => `${i + 1}. ${s.title} — ${s.url}`)
    .join("\n");
}

function buildChatBody(userReplies: string[], assistant: string): string {
  const parts = userReplies.filter((r) => r.length > 0);
  if (assistant.length > 0) parts.push(assistant);
  return parts.join("\n\n");
}

function assembleSubject(
  topic: string,
  purpose: string,
  chatBody: string,
  sources: { title: string; url: string }[],
): string {
  const sections = [
    `## Konu\n${topic}`,
    `## Amaç\n${purpose}`,
    `## Sohbet özeti\n${chatBody}`,
  ];
  const kaynaklar = formatSources(sources);
  if (kaynaklar) sections.push(`## Kaynaklar\n${kaynaklar}`);
  return sections.join("\n\n");
}

function truncateChatBody(
  chatBody: string,
  budgetChars: number,
  topic: string,
  purpose: string,
  sources: { title: string; url: string }[],
): string {
  const shell = assembleSubject(topic, purpose, "", sources);
  const maxChat = budgetChars - shell.length;
  if (maxChat <= 0) return "";
  if (maxChat === 1) return "…";
  if (chatBody.length <= maxChat) return chatBody;
  return chatBody.slice(0, maxChat - 1) + "…";
}

export function assemblePersonaCase(input: PersonaCaseInput): {
  subject_title: string;
  subject: string;
} {
  const { topic, purpose, userReplies, lastAssistantBody, sources, budgetChars } =
    input;

  const replies = [...userReplies];
  let assistant = lastAssistantBody;

  const build = () =>
    assembleSubject(topic, purpose, buildChatBody(replies, assistant), sources);

  let subject = build();

  while (subject.length > budgetChars && replies.length > 0) {
    replies.shift();
    subject = build();
  }

  while (subject.length > budgetChars && assistant.length > 0) {
    assistant = assistant.slice(0, -1);
    subject = build();
  }

  if (subject.length > budgetChars) {
    const chatBody = truncateChatBody(
      buildChatBody(replies, assistant),
      budgetChars,
      topic,
      purpose,
      sources,
    );
    subject = assembleSubject(topic, purpose, chatBody, sources);
  }

  return { subject_title: topic, subject };
}
