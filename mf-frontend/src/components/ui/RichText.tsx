"use client";

// Rich Result: structured text rendered as structure.
//
// This is a small Markdown subset written by hand rather than a library, and
// the reason is specific rather than ideological. The text being rendered comes
// from two places — passages out of the knowledge base, and answers out of a 2B
// model — and neither is trusted input. A general Markdown renderer accepts raw
// HTML, link targets and image sources, all of which are injection surface for
// text that a model can be talked into producing. This renderer has no escape
// hatch: every branch below emits a React element with the text as a *child*,
// never as HTML, so there is no path from the corpus to the DOM as markup.
//
// What it supports is what the material actually contains: headings, lists,
// tables, code, bold, inline code, and blockquotes. Anything unrecognised is
// rendered as the paragraph it is, which is the correct failure — a stray `~~`
// shows up as two tildes rather than swallowing the line.

import { Fragment, type ReactNode } from "react";

/** Inline formatting: **bold**, `code`, *italic*. */
function inline(text: string, keyPrefix: string): ReactNode[] {
  // One pass, one regex, alternatives ordered longest-first so `**` is matched
  // before `*` — the other order makes every bold run render as two italics
  // wrapped around nothing.
  const re = /(\*\*[^*]+\*\*|`[^`]+`|\*[^*]+\*)/g;
  const out: ReactNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  let i = 0;

  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push(text.slice(last, m.index));
    const tok = m[0];
    const k = `${keyPrefix}-i${i++}`;

    if (tok.startsWith("**")) {
      out.push(<strong key={k}>{tok.slice(2, -2)}</strong>);
    } else if (tok.startsWith("`")) {
      out.push(
        <code
          key={k}
          className="px-1 py-0.5 rounded text-[0.9em]"
          style={{ background: "var(--panel-2)", fontFamily: "var(--font-mono, monospace)" }}
        >
          {tok.slice(1, -1)}
        </code>,
      );
    } else {
      out.push(<em key={k}>{tok.slice(1, -1)}</em>);
    }
    last = m.index + tok.length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

/** A pipe-delimited table, with its separator row dropped. */
function Table({ rows, k }: { rows: string[]; k: string }) {
  const cells = rows.map((r) =>
    r.replace(/^\||\|$/g, "").split("|").map((c) => c.trim()),
  );
  // Markdown puts a |---|---| line under the header. It carries no content and
  // rendering it produces a row of dashes that reads like data.
  const isRule = (c: string[]) => c.every((x) => /^:?-{2,}:?$/.test(x));
  const body = cells.filter((c) => !isRule(c));
  if (body.length === 0) return null;

  const [head, ...rest] = body;
  return (
    // Tables are the one element here that can legitimately be wider than the
    // column. Scrolling it inside its own box keeps the page from scrolling
    // sideways, which on a phone makes everything else unreadable.
    <div key={k} className="overflow-x-auto my-3">
      <table className="text-sm border-collapse min-w-full">
        <thead>
          <tr>
            {head.map((c, i) => (
              <th
                key={i}
                className="text-left font-semibold px-3 py-1.5 whitespace-nowrap"
                style={{ borderBottom: "1px solid var(--line)", color: "var(--text)" }}
              >
                {inline(c, `${k}-h${i}`)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rest.map((row, r) => (
            <tr key={r}>
              {row.map((c, i) => (
                <td
                  key={i}
                  className="px-3 py-1.5 align-top"
                  style={{ borderBottom: "1px solid var(--line)", color: "var(--text-dim)" }}
                >
                  {inline(c, `${k}-r${r}c${i}`)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

const HEADING_SIZE = ["text-lg", "text-base", "text-sm", "text-sm", "text-sm", "text-sm"];

export function RichText({ text }: { text: string }) {
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  const out: ReactNode[] = [];

  let para: string[] = [];
  let list: { ordered: boolean; items: string[] } | null = null;
  let table: string[] = [];
  let fence: { lang: string; lines: string[] } | null = null;
  let quote: string[] = [];
  let k = 0;

  const flushPara = () => {
    if (!para.length) return;
    out.push(
      <p key={`p${k++}`} className="text-sm leading-relaxed my-2">
        {inline(para.join(" "), `p${k}`)}
      </p>,
    );
    para = [];
  };
  const flushList = () => {
    if (!list) return;
    const L = list.ordered ? "ol" : "ul";
    out.push(
      <L
        key={`l${k++}`}
        className={`text-sm leading-relaxed my-2 pl-5 ${list.ordered ? "list-decimal" : "list-disc"}`}
      >
        {list.items.map((it, i) => (
          <li key={i} className="my-0.5">
            {inline(it, `l${k}-${i}`)}
          </li>
        ))}
      </L>,
    );
    list = null;
  };
  const flushTable = () => {
    if (!table.length) return;
    out.push(<Table key={`t${k++}`} rows={table} k={`t${k}`} />);
    table = [];
  };
  const flushQuote = () => {
    if (!quote.length) return;
    out.push(
      <blockquote
        key={`q${k++}`}
        className="text-sm leading-relaxed my-2 pl-3 py-1"
        style={{ borderLeft: "2px solid var(--brand)", color: "var(--text-dim)" }}
      >
        {inline(quote.join(" "), `q${k}`)}
      </blockquote>,
    );
    quote = [];
  };
  const flushAll = () => {
    flushPara();
    flushList();
    flushTable();
    flushQuote();
  };

  for (const raw of lines) {
    const line = raw.trimEnd();

    // A fenced block swallows everything until it closes, including lines that
    // would otherwise look like headings or tables. Checked first for exactly
    // that reason — a `# comment` inside a code sample is not a heading.
    if (fence) {
      if (line.trimStart().startsWith("```")) {
        out.push(
          <pre
            key={`c${k++}`}
            className="text-xs my-3 p-3 rounded overflow-x-auto"
            style={{ background: "var(--panel-2)" }}
          >
            <code>{fence.lines.join("\n")}</code>
          </pre>,
        );
        fence = null;
      } else {
        fence.lines.push(raw);
      }
      continue;
    }
    if (line.trimStart().startsWith("```")) {
      flushAll();
      fence = { lang: line.trim().slice(3), lines: [] };
      continue;
    }

    if (!line.trim()) {
      flushAll();
      continue;
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    if (heading) {
      flushAll();
      const level = heading[1].length;
      const Tag = `h${Math.min(level + 1, 6)}` as "h2";
      out.push(
        <Tag key={`h${k++}`} className={`${HEADING_SIZE[level - 1]} font-semibold mt-4 mb-1.5`}>
          {inline(heading[2], `h${k}`)}
        </Tag>,
      );
      continue;
    }

    if (line.trimStart().startsWith("|") && line.includes("|", 1)) {
      flushPara();
      flushList();
      flushQuote();
      table.push(line.trim());
      continue;
    }
    flushTable();

    const quoted = /^>\s?(.*)$/.exec(line.trimStart());
    if (quoted) {
      flushPara();
      flushList();
      quote.push(quoted[1]);
      continue;
    }
    flushQuote();

    const bullet = /^\s*[-*•]\s+(.*)$/.exec(line);
    const numbered = /^\s*\d+[.)]\s+(.*)$/.exec(line);
    if (bullet || numbered) {
      flushPara();
      const ordered = !!numbered;
      // A change of list type ends the previous list. Without this, a bulleted
      // list following a numbered one continues the numbering.
      if (list && list.ordered !== ordered) flushList();
      if (!list) list = { ordered, items: [] };
      list.items.push((bullet ?? numbered)![1]);
      continue;
    }
    flushList();

    para.push(line.trim());
  }

  // An unterminated fence is common in truncated model output. Rendering what
  // was collected beats dropping it.
  if (fence) {
    out.push(
      <pre
        key={`c${k++}`}
        className="text-xs my-3 p-3 rounded overflow-x-auto"
        style={{ background: "var(--panel-2)" }}
      >
        <code>{fence.lines.join("\n")}</code>
      </pre>,
    );
  }
  flushAll();

  return <div className="rich">{out.map((n, i) => <Fragment key={i}>{n}</Fragment>)}</div>;
}
