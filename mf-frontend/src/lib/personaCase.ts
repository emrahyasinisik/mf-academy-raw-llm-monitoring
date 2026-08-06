// Assembles the analysis/run `subject` from the persona chat transcript.
//
// The UI no longer collects Konu/Amaç as separate fields — the first user
// bubble *is* the case. Older threads may still carry primed "Konu:" / "Amaç:"
// lines; parseIntake strips those so report assembly stays readable.
// Kaynaklar is never dropped — only the middle chat summary shrinks when the
// rubric's character budget is exceeded.

/** Restores optional intake headers from an older primed first turn. */
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
  if (lines[i] === "") i += 1;

  return { topic, purpose, rest: lines.slice(i).join("\n") };
}

export type PersonaCaseInput = {
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

/** Title for the assessment: old Konu line, else the first line of the ask. */
function caseTitle(userReplies: string[]): string {
  const first = userReplies[0] ?? "";
  const { topic, rest } = parseIntake(first);
  if (topic) return topic;
  const line = (rest || first).split("\n")[0]?.trim() ?? "";
  if (!line) return "Vaka";
  return line.length > 80 ? line.slice(0, 79) + "…" : line;
}

function normalizeReplies(userReplies: string[]): string[] {
  return userReplies.map((r, i) => {
    if (i !== 0) return r;
    const { rest } = parseIntake(r);
    return rest || r;
  });
}

function assembleSubject(
  title: string,
  chatBody: string,
  sources: { title: string; url: string }[],
): string {
  const sections = [`## Konu\n${title}`, `## Sohbet özeti\n${chatBody}`];
  const kaynaklar = formatSources(sources);
  if (kaynaklar) sections.push(`## Kaynaklar\n${kaynaklar}`);
  return sections.join("\n\n");
}

function truncateChatBody(
  chatBody: string,
  budgetChars: number,
  title: string,
  sources: { title: string; url: string }[],
): string {
  const shell = assembleSubject(title, "", sources);
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
  const { lastAssistantBody, sources, budgetChars } = input;
  const title = caseTitle(input.userReplies);
  const replies = normalizeReplies(input.userReplies);
  let assistant = lastAssistantBody;

  const build = () =>
    assembleSubject(title, buildChatBody(replies, assistant), sources);

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
      title,
      sources,
    );
    subject = assembleSubject(title, chatBody, sources);
  }

  return { subject_title: title, subject };
}
