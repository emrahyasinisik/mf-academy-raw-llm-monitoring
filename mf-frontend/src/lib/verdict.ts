// The verdict lines the persona closes a final answer with, read out of its
// reply so the screen can show a badge instead of a paragraph.
//
// This is the browser half of a contract whose other half is
// `mf-backend/internal/decision/verdict.go`. The server parses the same reply to
// store one label per thread — the conversation list renders from a summary
// query that deliberately carries no message bodies, so the badge in the list
// and the badge in the transcript come from two different readers of one
// format. They are kept identical in shape on purpose: a change to the prompt
// contract must be one grep away from both.
//
// It lives in lib/ rather than beside the view because the rule below is worth
// testing, and the test runner reads src/lib/*.test.ts.

/** A decision the persona committed to. Absent while it researches or asks. */
export type Verdict = { label: string; score: number | null };

const VERDICT_RE = /KARAR:\s*([^\n]+)/i;
const SCORE_RE = /SKOR:\s*(\d{1,3})/i;

/**
 * Reads the badge out of a reply.
 *
 * @param text the persona's message
 * @param sourceCount how many pieces of evidence the turn gathered — which
 *   decides whether the number on the SKOR line means anything.
 *
 * A turn that researched nothing still produces a KARAR/SKOR block: the model is
 * asked for one, and asking a 2B model *not* to answer is a request rather than
 * a constraint. Rendering that "SKOR: 0" as 0/100 would put a measured-looking
 * number on a thread that read nothing — the same mistake the analysis path is
 * built to avoid, where a criterion with no evidence scores null and never zero.
 * So with no sources the score is dropped and only the label is shown; the empty
 * research trail beside it says why.
 */
/** Investability labels the report CTA cares about — not free-form "Karar: …". */
const INVEST_LABEL =
  /yatırılabilir|temkinli|yatırılamaz/i;

export function parseVerdict(text: string, sourceCount: number): Verdict | null {
  const m = text.match(VERDICT_RE);
  if (!m) return null;

  const label = m[1].trim();
  // No evidence → no badge. Greeting turns used to invent "Karar: şarkı…" and
  // the report button appeared on an empty thread.
  if (sourceCount <= 0) return null;
  if (!INVEST_LABEL.test(label)) return null;

  const s = text.match(SCORE_RE);
  return { label, score: s ? Math.min(100, parseInt(s[1], 10)) : null };
}

/**
 * Removes the machine-readable verdict lines from the prose.
 *
 * They are a protocol between the persona and this screen, not part of what it
 * wrote: once parsed into the badge, leaving them in the body renders the
 * decision twice, and the second copy is worse — the two lines collapse into one
 * run-on paragraph ("KARAR: Temkinli yatırılabilir SKOR: 64") because nothing in
 * markdown makes them separate blocks.
 *
 * Only applied when a verdict was actually parsed, so a reply that mentions
 * these words without committing to a decision keeps every word it wrote.
 */
export function stripVerdictLines(text: string): string {
  return text
    .split("\n")
    .filter((line) => !/^\s*(KARAR|SKOR)\s*:/i.test(line))
    .join("\n")
    .trimEnd();
}
