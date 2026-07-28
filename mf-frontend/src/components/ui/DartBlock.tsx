"use client";

// A Dart source block: highlighted, numbered, copyable.
//
// Written by hand rather than pulling in a highlighter, for the same reason
// RichText is hand-written: this renders text a language model produced, and it
// must not be possible for that text to reach the DOM as markup. Every branch
// below emits a React element with the source as a *child* — there is no
// dangerouslySetInnerHTML and no path to one. A highlighter that returns an HTML
// string would hand that guarantee away for prettier keywords.
//
// It is also much less code than it looks: one tokenizer pass, one colour map.

import { useCallback, useState } from "react";

// Syntax colours are local rather than in globals.css because nothing else in
// the app highlights code, and a var(--syntax-keyword) that exists for one
// component is indirection without a payoff.
//
// They are also deliberately literals rather than the design system's status
// colours. The system reserves green, amber and red to mean healthy, caution and
// failed; borrowing --ok for string literals would put "healthy" green on every
// quoted word in a widget that the linter had just rejected. A highlighter needs
// six hues that separate well from each other, which is a different problem from
// the one the palette solves — so it gets its own, tuned to sit on --bg-sunk and
// to keep the brand orange in the family.
const C = {
  keyword: "#f0883e",
  string: "#7ee787",
  comment: "#6b7c8a",
  number: "#79c0ff",
  type: "#d2a8ff",
  annotation: "#ff7b72",
  plain: "#e9eff5",
};

const KEYWORDS = new Set([
  "abstract", "as", "assert", "async", "await", "break", "case", "catch", "class",
  "const", "continue", "covariant", "default", "deferred", "do", "dynamic", "else",
  "enum", "export", "extends", "extension", "external", "factory", "false", "final",
  "finally", "for", "get", "hide", "if", "implements", "import", "in", "interface",
  "is", "late", "library", "mixin", "new", "null", "on", "operator", "part",
  "required", "rethrow", "return", "sealed", "set", "show", "static", "super",
  "switch", "sync", "this", "throw", "true", "try", "typedef", "var", "void",
  "when", "while", "with", "yield",
]);

type Tok = { text: string; color: string; italic?: boolean };

// One regex, alternatives ordered so the greedy multi-character forms win before
// their prefixes: block comment before `/`, triple-quoted string before single,
// raw string before identifier. Getting this order wrong is what makes a
// highlighter colour half a string as code.
const TOKENIZER = new RegExp(
  [
    "(\\/\\*[\\s\\S]*?\\*\\/|\\/\\/[^\\n]*)", // 1 comment
    "(r?'''[\\s\\S]*?'''|r?\"\"\"[\\s\\S]*?\"\"\"|r?'(?:\\\\.|[^'\\\\\\n])*'|r?\"(?:\\\\.|[^\"\\\\\\n])*\")", // 2 string
    "(@[A-Za-z_]\\w*)", // 3 annotation
    "(\\b\\d[\\d_]*(?:\\.\\d+)?(?:[eE][+-]?\\d+)?\\b|\\b0x[0-9a-fA-F]+\\b)", // 4 number
    "([A-Za-z_$][\\w$]*)", // 5 identifier
  ].join("|"),
  "g",
);

function tokenize(src: string): Tok[] {
  const out: Tok[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  TOKENIZER.lastIndex = 0;
  while ((m = TOKENIZER.exec(src)) !== null) {
    if (m.index > last) out.push({ text: src.slice(last, m.index), color: C.plain });
    const [, comment, str, annotation, num, ident] = m;
    if (comment) out.push({ text: comment, color: C.comment, italic: true });
    else if (str) out.push({ text: str, color: C.string });
    else if (annotation) out.push({ text: annotation, color: C.annotation });
    else if (num) out.push({ text: num, color: C.number });
    else if (ident) {
      // Dart has no type keyword list worth maintaining, but it does have an
      // enforced convention: types are UpperCamelCase and nothing else is. That
      // heuristic is right far more often than a hard-coded list of Flutter
      // class names could stay.
      const color = KEYWORDS.has(ident)
        ? C.keyword
        : /^[A-Z_$]/.test(ident)
          ? C.type
          : C.plain;
      out.push({ text: ident, color });
    }
    last = m.index + m[0].length;
  }
  if (last < src.length) out.push({ text: src.slice(last), color: C.plain });
  return out;
}

/**
 * Splits tokens across lines so a gutter can number them. Done after tokenizing
 * rather than before, because block comments and triple-quoted strings span
 * newlines — highlighting line by line would break both.
 */
function toLines(toks: Tok[]): Tok[][] {
  const lines: Tok[][] = [[]];
  for (const t of toks) {
    const parts = t.text.split("\n");
    parts.forEach((p, i) => {
      if (i > 0) lines.push([]);
      if (p) lines[lines.length - 1].push({ ...t, text: p });
    });
  }
  return lines;
}

export function DartBlock({
  code,
  highlightLines = [],
}: {
  code: string;
  /** 1-indexed lines to mark, e.g. where the linter found something. */
  highlightLines?: number[];
}) {
  const [copied, setCopied] = useState(false);
  const lines = toLines(tokenize(code));
  const marked = new Set(highlightLines);

  const copy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      // Clipboard access is refused outside a secure context, which is exactly
      // where this runs during local development over plain http. Saying so
      // beats a button that appears to do nothing.
      setCopied(false);
      alert("Kopyalanamadı — tarayıcı pano erişimini reddetti (http üzerinde normal).");
    }
  }, [code]);

  return (
    <div className="card overflow-hidden">
      <div
        className="flex items-center justify-between px-3.5 py-2.5"
        style={{ background: "var(--panel-2)", borderBottom: "1px solid var(--line)" }}
      >
        <span className="eyebrow">dart · {lines.length} satır</span>
        {/* The label is the whole feedback mechanism, so it must not resize the
            button when it changes — a control that jumps at the moment it is
            clicked reads as a misfire. Fixed width, centred text. */}
        <button
          className="btn btn-ghost btn-sm justify-center"
          onClick={copy}
          style={{
            minWidth: "6.5rem",
            color: copied ? "var(--ok)" : undefined,
            borderColor: copied ? "var(--ok-line)" : undefined,
          }}
        >
          {copied ? "Kopyalandı" : "Kopyala"}
        </button>
      </div>

      {/* The code sits in a well: recessed, where the panel around it is raised.
          The two depths are what separate the model's output from the app's own
          chrome at a glance. */}
      <div className="overflow-x-auto scrollbar-thin" style={{ background: "var(--bg-sunk)" }}>
        <pre className="mono text-xs leading-relaxed py-3" style={{ color: C.plain }}>
          {lines.map((toks, i) => (
            <div
              key={i}
              className="flex"
              style={{
                background: marked.has(i + 1) ? "var(--brand-wash)" : undefined,
                boxShadow: marked.has(i + 1)
                  ? "inset 2px 0 0 var(--brand)"
                  : undefined,
              }}
            >
              <span
                className="select-none shrink-0 text-right pr-3 pl-3 num"
                style={{ color: "var(--text-faint)", minWidth: "3.25rem" }}
              >
                {i + 1}
              </span>
              <span className="pr-4 whitespace-pre">
                {toks.length === 0
                  ? " "
                  : toks.map((t, j) => (
                      <span
                        key={j}
                        style={{ color: t.color, fontStyle: t.italic ? "italic" : undefined }}
                      >
                        {t.text}
                      </span>
                    ))}
              </span>
            </div>
          ))}
        </pre>
      </div>
    </div>
  );
}
