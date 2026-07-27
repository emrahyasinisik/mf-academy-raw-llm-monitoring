// The Flutter screen generator's contract: the system prompt the adapter was
// trained against, the brief shape it was trained on, and the checks its output
// has to survive.
//
// Why all three live in one file
// ------------------------------
// They are one thing. The fine-tune learned to answer *this* system prompt when
// given *this* brief layout, and to produce code that passes *these* checks —
// because the training set was filtered by them. Splitting them across the app
// is how they drift apart, and drift here has no symptom: the model still
// answers, the answer is still Dart, and it is quietly worse than the numbers
// from the training run promised.
//
// Where this should eventually live
// ---------------------------------
// On the server. internal/decision already solved this problem for the
// investment persona — GET /decision/prompt serves the prompt so the dataset
// builder never copies it — and this file is the copy that pattern exists to
// prevent. Until there is a GET /codegen/prompt to fetch, the constant below is
// the single place it is written down, and VERIFY_COMMAND is how you prove it
// still matches the data the adapter was trained on.

/**
 * Byte-identical to the `system` message on every row of the training set.
 *
 * Verified against flutter_screens_train_v7.jsonl on 2026-07-27 — see
 * SYSTEM_PROMPT_SHA256 below and VERIFY_COMMAND for how to re-check it.
 *
 * It is **one line**. The pipeline document prints it wrapped, and transcribing
 * it from there produced a seven-line string that hashed differently: same
 * words, six newlines the training data does not contain. Nothing about that
 * failure is visible at runtime — the model still answers, still in Dart, just
 * off the distribution it was tuned on. Do not reflow this string to fit the
 * editor.
 */
export const FLUTTER_SYSTEM_PROMPT =
  "Sen bir Flutter ekran üreticisisin. Verilen ekran brief'ine göre Material 3 ve güncel Flutter API'leriyle, tek dosyada çalışan tam bir Dart widget'ı üretirsin. Durum yönetimi için flutter_bloc kullanırsın: basit durumda Cubit, olay güdümlü akışta Bloc+event. BlocProvider'ı ayrı bir _View widget'ından bölersin. Yalnızca veri gösteren widget'ları StatelessWidget yaparsın. const kullanır, controller'ları dispose eder, Form alanlarında validator yazarsın; placeholder veya TODO bırakmazsın. Yanıtı yalnızca ```dart kod bloğu olarak verirsin.";

/**
 * sha256 of the string above, and of the system message in v7 of the dataset.
 * They were equal when this was written; if they ever differ, the adapter is
 * being asked a question it was not trained to answer.
 */
export const SYSTEM_PROMPT_SHA256 =
  "0c1d64dec3eabddad5ad84e6d3f36225a636f112f0f5a42ad4f5fe0e6881deaf";

/**
 * Re-proves the constant against a dataset version. Run it in the training
 * notebook whenever the prompt or the dataset changes — it compares hashes
 * rather than eyeballs, because the difference that matters here is whitespace.
 */
export const VERIFY_COMMAND = `python3 -c "
import hashlib, json
DATA = 'flutter_screens_train_v7.jsonl'
got = json.loads(open(DATA, encoding='utf-8').readline())['messages'][0]['content']
print(hashlib.sha256(got.encode()).hexdigest())
# must equal SYSTEM_PROMPT_SHA256 in flutterContract.ts"`;

// ---- the brief ----

/**
 * How the state field is phrased in training. A closed set rather than free
 * text: "State: Riverpod" is a prompt the adapter has no answer for, and it will
 * improvise one.
 *
 * These strings are byte-checked against the dataset by
 * `mf-inference/peft/flutter/verify_contract.py`. They were not, until v8: the
 * comment here claimed the model had seen these three shapes and nothing else,
 * and the dataset disagreed with all three. v7 phrased the stateless case as
 * "yok (StatelessWidget)." 45 times and "yok, StatelessWidget" never — so every
 * generation ran a prompt the adapter had never been trained on, with no symptom
 * beyond output that was slightly worse than it should have been. v8 makes the
 * dataset's own phrasing canonical and the verifier keeps the two ends equal.
 */
export const STATE_CHOICES = [
  { id: "cubit", label: "Cubit", wire: "flutter_bloc (Cubit)" },
  { id: "bloc", label: "Bloc + event", wire: "flutter_bloc (Bloc + event)" },
  { id: "stateless", label: "Durumsuz", wire: "yok (StatelessWidget)" },
] as const;

export type StateChoice = (typeof STATE_CHOICES)[number]["id"];

/**
 * What is being asked for. The training set opens a brief with `Ekran:` 104
 * times and `Bileşen:` 32 times, and the two produce different answers: a screen
 * is a Scaffold with an AppBar, a component is a widget meant to be dropped into
 * someone else's tree. Until this existed the form could only say `Ekran:`, so a
 * quarter of what the adapter was trained to do had no way to be asked for.
 */
export const SUBJECT_KINDS = [
  { id: "ekran", label: "Ekran", wire: "Ekran", hint: "kısa ad" },
  { id: "bilesen", label: "Bileşen", wire: "Bileşen", hint: "tek widget" },
] as const;

export type SubjectKind = (typeof SUBJECT_KINDS)[number]["id"];

export interface Brief {
  kind: SubjectKind;
  screen: string;
  description: string;
  fields: string;
  state: StateChoice;
}

/**
 * Assembles the four-line layout every training prompt used. The labels and
 * their order are part of what the adapter learned; a free-form textarea would
 * be a friendlier input and a worse prompt, which is why the form is the primary
 * surface and raw text is the escape hatch rather than the default.
 *
 * Omitting an empty optional line rather than sending a bare label, because
 * "Alanlar/İçerik:" with nothing after it appears nowhere in the training set.
 */
export function buildBrief(b: Brief): string {
  const state = STATE_CHOICES.find((s) => s.id === b.state) ?? STATE_CHOICES[0];
  const kind = SUBJECT_KINDS.find((k) => k.id === b.kind) ?? SUBJECT_KINDS[0];
  const lines = [`${kind.wire}: ${b.screen.trim()}`];
  if (b.description.trim()) lines.push(`Açıklama: ${b.description.trim()}`);
  if (b.fields.trim()) lines.push(`Alanlar/İçerik: ${b.fields.trim()}`);
  lines.push(`State: ${state.wire}.`);
  return lines.join("\n");
}

// ---- reading the answer ----

export interface Extracted {
  code: string;
  /** False when the model ignored the contract and answered without a fence. */
  fenced: boolean;
  /** True when the fence opened and never closed — a max_tokens cutoff. */
  truncated: boolean;
  /** Prose the model emitted outside the block, which the contract forbids. */
  stray: string;
}

/**
 * Pulls the Dart out of the reply.
 *
 * Truncation is reported rather than repaired. A cut-off widget is the failure
 * mode this model hits first — 1200 completion tokens is not always enough for a
 * form screen — and silently rendering the fragment as if it were the answer
 * trains the operator to trust incomplete output.
 */
export function extractDart(reply: string): Extracted {
  const open = reply.match(/```(?:dart)?[ \t]*\r?\n/);
  if (!open || open.index === undefined) {
    // No fence at all. Still show it — the operator needs to see what the model
    // actually said to tell a bad adapter from a bad prompt.
    return { code: reply.trim(), fenced: false, truncated: false, stray: "" };
  }
  const bodyStart = open.index + open[0].length;
  const rest = reply.slice(bodyStart);
  const close = rest.indexOf("```");
  if (close === -1) {
    return { code: rest.trimEnd(), fenced: true, truncated: true, stray: "" };
  }
  const before = reply.slice(0, open.index).trim();
  const after = rest.slice(close + 3).trim();
  return {
    code: rest.slice(0, close).trimEnd(),
    fenced: true,
    truncated: false,
    stray: [before, after].filter(Boolean).join("\n\n"),
  };
}

// ---- the checks ----

export type Severity = "error" | "warn";

export interface Finding {
  severity: Severity;
  rule: string;
  detail: string;
  /** 1-indexed line in the extracted code, or null for whole-file findings. */
  line: number | null;
}

// The same bad patterns the dataset scanner rejects, applied to output instead
// of input. Deliberately the same list: a rule that keeps an example out of
// training is a rule the trained model is not supposed to violate, so the two
// ends of the pipeline agree by construction.
//
// Severity is not in the dataset scanner, which fails on any hit. Here it is,
// because `primary:` is a legitimate ColorScheme argument even though it is also
// the deprecated button argument — hard-failing it would cry wolf on correct
// code, and a checker the operator learns to ignore checks nothing.
const PATTERNS: { re: RegExp; severity: Severity; rule: string; detail: string }[] = [
  { re: /\bRaisedButton\b/, severity: "error", rule: "RaisedButton", detail: "kaldırıldı — ElevatedButton veya FilledButton kullan" },
  { re: /\bFlatButton\b/, severity: "error", rule: "FlatButton", detail: "kaldırıldı — TextButton kullan" },
  { re: /\bwithOpacity\s*\(/, severity: "error", rule: "withOpacity", detail: "deprecated — .withValues(alpha:) kullan" },
  { re: /\bheadline6\b/, severity: "error", rule: "headline6", detail: "eski TextTheme adı — titleLarge kullan" },
  { re: /\bBottomNavigationBar\b/, severity: "error", rule: "BottomNavigationBar", detail: "Material 3'te NavigationBar kullan" },
  { re: /\bmarkNeedsBuild\s*\(/, severity: "error", rule: "markNeedsBuild", detail: "framework iç API'si — durum yönetimiyle çözülmeli" },
  { re: /\bsetState\s*\(/, severity: "error", rule: "setState", detail: "sözleşme durumu flutter_bloc ile yönetmeyi şart koşuyor" },
  { re: /\bTODO\b|\/\/\s*\.\.\.$/m, severity: "error", rule: "TODO", detail: "sözleşme placeholder bırakmayı yasaklıyor" },
  { re: /[＀-￯]/, severity: "error", rule: "tam-genişlik karakter", detail: "Dart'ta derlenmez — ASCII rakam/noktalama kullan" },
  { re: /(?<![.\w])primary:/, severity: "warn", rule: "primary:", detail: "ColorScheme argümanıysa doğru, buton argümanıysa deprecated — kontrol et" },
  { re: /(?<![.\w])onPrimary:/, severity: "warn", rule: "onPrimary:", detail: "aynı belirsizlik — buton stilinde deprecated" },
];

/**
 * Scans generated code for the patterns the contract forbids, plus the two
 * structural rules the trial run showed the model getting wrong: the
 * BlocProvider/_View split, and disposing controllers it creates.
 *
 * This is a lint pass, not a compiler. It cannot tell you the widget builds —
 * only that it does not contain the mistakes this fine-tune is known to make.
 */
export function lintDart(code: string, state: StateChoice): Finding[] {
  const out: Finding[] = [];
  const lines = code.split("\n");

  for (const p of PATTERNS) {
    // Report the first line that matches rather than the whole file, so the
    // finding is navigable. A second hit of the same rule adds no information.
    const idx = lines.findIndex((l) => p.re.test(l));
    if (idx !== -1) {
      out.push({ severity: p.severity, rule: p.rule, detail: p.detail, line: idx + 1 });
    }
  }

  const wantsBloc = state === "cubit" || state === "bloc";
  if (wantsBloc) {
    if (!/\bBlocProvider\b/.test(code)) {
      out.push({ severity: "error", rule: "BlocProvider yok", detail: "durumlu ekran BlocProvider ile sarılmalı", line: null });
    }
    if (!/class\s+_\w*View\b/.test(code)) {
      out.push({ severity: "warn", rule: "_View bölünmesi yok", detail: "sözleşme BlocProvider'ı ayrı bir _View widget'ından bölmeyi istiyor", line: null });
    }
    if (state === "bloc" && !/\bon<\w+>\(/.test(code)) {
      out.push({ severity: "warn", rule: "event handler yok", detail: "Bloc+event seçildi ama on<Event> kaydı görünmüyor", line: null });
    }
  } else if (/\bStatefulWidget\b/.test(code)) {
    out.push({ severity: "warn", rule: "StatefulWidget", detail: "durumsuz istendi ama StatefulWidget üretilmiş", line: null });
  }

  // Controllers are the trial run's other weak spot, and an undisposed one is a
  // real leak rather than a style preference.
  //
  // Matching the *constructor call*, not a typed declaration: `final ctrl =
  // TextEditingController()` is the ordinary Dart idiom and has no type to match
  // on. Ownership is what creates the obligation to dispose, and construction is
  // what ownership looks like — a controller received as a parameter or read off
  // an inherited widget belongs to somebody else and must not be disposed here.
  const owns =
    /\b\w*Controller\s*\(/.test(code) || /\blate\s+final\s+\w*Controller\b/.test(code);
  if (owns && !/\.dispose\s*\(/.test(code)) {
    out.push({ severity: "error", rule: "dispose yok", detail: "controller oluşturulmuş ama dispose edilmemiş", line: null });
  }

  if (/\bForm\b/.test(code) && !/validator\s*:/.test(code)) {
    out.push({ severity: "warn", rule: "validator yok", detail: "Form var ama hiçbir alanda validator yok", line: null });
  }

  return out;
}
