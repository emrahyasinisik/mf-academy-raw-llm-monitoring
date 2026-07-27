#!/usr/bin/env python3
"""Build v8 of the Flutter training set: v7's briefs, with evidence in front.

v7 taught the house style from the model's own memory of Flutter. That memory
has a cutoff, and the product's whole claim — the spec's "researches before it
answers" — is that the served model uses *current* API facts instead. v8 is the
prompt shape that makes that possible: every row carries a numbered evidence
block assembled the way the backend will assemble it at inference, so the model
is trained on the question it will actually be asked.

Nothing about the answers is invented here. All 139 Dart bodies come from v7
unchanged (bar the setState repair below), so v8 cannot introduce a bug that v7
did not already have. Only the user message is rewritten.

Three row kinds, and the kind is the label
------------------------------------------
grounded    The evidence states the API facts the answer already uses, plus
            distractors it does not. Teaches: read the block, use the parts that
            apply. Without distractors the model would learn "cite everything",
            which is the same as reading nothing.

migration   The evidence says an API was removed and names its replacement —
            and the answer uses the replacement. This is the row that carries
            the product's actual promise, and it is free: v7's code is already
            modern, so the replacement is already there to point at.

thin        The evidence found nothing. The answer is unchanged. Teaches: with
            no sources, fall back to core widgets rather than inventing an API.
            This is the codegen analogue of the persona's "ask instead of
            guessing" — the failure it prevents is a confident hallucinated
            widget, which reads exactly like a correct one.

The brief is normalised, on purpose
-----------------------------------
v7's State line was free text with ~95 distinct phrasings ("flutter_bloc
(TimerCubit — Timer.periodic, close'da iptal)"). The UI can send three. That gap
is drift the SHA guard never covered: the served prompt was never a prompt the
adapter had seen. v8 collapses State to the three canonical strings the UI emits
and writes `Alanlar:` where the UI wrote `Alanlar/İçerik:`, so verify_contract.py
passes against the shipped frontend rather than failing four ways.

Usage:
    python3 build_flutter_dataset_v8.py --src data/flutter_screens_train_v7.jsonl
"""

from __future__ import annotations

import argparse
import collections
import hashlib
import json
import os
import random
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))

# --- the wire format ---------------------------------------------------------
# Mirrors internal/decision.Agent.gather so the backend's codegen agent can emit
# a byte-identical block. When that agent lands, this header must move to a
# GET /codegen/prompt fetch for the reason build_persona_dataset.py does it: a
# copy drifts the first time either end is edited, and the failure is silent.

EVIDENCE_HEADER = "KAYNAKLAR (güncel Flutter API bilgisi — kodu bunlara göre yaz):"
NO_EVIDENCE = "- Canlı arama sonuç döndürmedi; yalnızca çekirdek Flutter API'lerini kullan."
TURN_INSTRUCTION = (
    "Yukarıdaki kaynaklara göre brief'i karşılayan tam Dart widget'ını üret. "
    "Kaynak bir API'nin kaldırıldığını söylüyorsa yerine önerileni kullan."
)

# --- API facts ---------------------------------------------------------------
# The evidence corpus. Every entry is a fact about a real Flutter or pub.dev API,
# keyed by the modern identifier that appears in the answers. `replaces` is what
# makes a row a migration: it names the removed API the frontend linter already
# rejects, so the two ends of the pipeline agree by construction — a rule that
# keeps code out of training is a rule the evidence teaches the model to follow.

FACTS: list[dict] = [
    {"key": "FilledButton", "replaces": "RaisedButton", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/FilledButton-class.html",
     "text": "FilledButton, Material 3'ün birincil eylem butonudur. RaisedButton "
             "Flutter 3.0'da kaldırıldı; yerine FilledButton veya ElevatedButton kullanılır."},
    {"key": "TextButton", "replaces": "FlatButton", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/TextButton-class.html",
     "text": "TextButton, düşük vurgulu eylemler içindir. FlatButton kaldırıldı; "
             "doğrudan karşılığı TextButton'dır."},
    {"key": "NavigationBar", "replaces": "BottomNavigationBar", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/NavigationBar-class.html",
     "text": "NavigationBar, Material 3'ün alt gezinme bileşenidir ve destination "
             "listesi alır. BottomNavigationBar Material 2 tasarımıdır."},
    {"key": "titleLarge", "replaces": "headline6", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/TextTheme-class.html",
     "text": "TextTheme adları 2022'de yenilendi: headline6 → titleLarge, "
             "bodyText2 → bodyMedium. Eski adlar kaldırıldı."},
    {"key": "withValues", "replaces": "withOpacity", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/dart-ui/Color/withValues.html",
     "text": "Color.withOpacity Flutter 3.27'de deprecated oldu; "
             "yerine withValues(alpha: 0.5) kullanılır."},
    {"key": "ScaffoldMessenger", "replaces": "Scaffold.of(context).showSnackBar", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/ScaffoldMessenger-class.html",
     "text": "SnackBar artık ScaffoldMessenger.of(context).showSnackBar ile gösterilir; "
             "Scaffold.of(context).showSnackBar kaldırıldı."},
    # Non-migration facts: current API detail with no deprecated ancestor. These
    # make `grounded` rows and, unused, make distractors.
    {"key": "BlocProvider", "kind": "pub.dev",
     "url": "https://pub.dev/packages/flutter_bloc",
     "text": "flutter_bloc ^9.1.1: BlocProvider bir Cubit/Bloc örneğini alt ağaca sağlar; "
             "create ile kurulan örneği kendisi close eder."},
    {"key": "BlocBuilder", "kind": "pub.dev",
     "url": "https://pub.dev/documentation/flutter_bloc/latest/",
     "text": "flutter_bloc ^9.1.1: BlocBuilder<B, S> yalnızca state değiştiğinde yeniden "
             "çizer; buildWhen ile gereksiz çizimler engellenir."},
    {"key": "TextEditingController", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/widgets/TextEditingController-class.html",
     "text": "TextEditingController bir ChangeNotifier'dır ve oluşturan tarafından "
             "dispose edilmelidir; edilmezse listener sızıntısı oluşur."},
    {"key": "ListView", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/widgets/ListView-class.html",
     "text": "ListView.builder öğeleri tembel oluşturur; uzun veya bilinmeyen "
             "uzunluktaki listelerde ListView(children:) yerine bu tercih edilir."},
    {"key": "InputDecoration", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/InputDecoration-class.html",
     "text": "InputDecoration Material 3'te filled ve border alanlarını "
             "InputDecorationTheme üzerinden devralır."},
    {"key": "Equatable", "kind": "pub.dev",
     "url": "https://pub.dev/packages/equatable",
     "text": "equatable ^2.1.0: props listesine giren alanlar eşitlik karşılaştırmasına "
             "katılır; eksik bırakılan alan state değişimini görünmez kılar."},
    # Facts keyed to the widgets that appear most often across the answers. Their
    # job is coverage: without them a third of the rows match no fact at all and
    # fall through to `thin`, which would drown the grounding signal in rows that
    # teach the model there is usually nothing to read.
    {"key": "SegmentedButton", "replaces": "ToggleButtons", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/SegmentedButton-class.html",
     "text": "SegmentedButton, Material 3'ün segment seçicisidir ve selected kümesi "
             "ile çalışır. ToggleButtons Material 2 bileşenidir."},
    {"key": "DropdownMenu", "replaces": "DropdownButton", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/DropdownMenu-class.html",
     "text": "DropdownMenu Material 3'ün açılır seçicisidir; DropdownMenuEntry listesi "
             "alır ve metin alanı davranışını içerir."},
    {"key": "Scaffold", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/Scaffold-class.html",
     "text": "Scaffold, Material düzeninin iskeletidir: appBar, body, floatingActionButton "
             "ve bottomNavigationBar yuvalarını sağlar."},
    {"key": "AppBar", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/AppBar-class.html",
     "text": "Material 3'te AppBar başlığı varsayılan olarak sola hizalıdır ve "
             "kaydırmada surfaceTintColor ile renk tonu kazanır."},
    {"key": "ListTile", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/ListTile-class.html",
     "text": "ListTile leading/title/subtitle/trailing yuvalarını taşır; Material 3'te "
             "yükseklik ve iç boşluk ListTileTheme'den gelir."},
    {"key": "Theme", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/Theme-class.html",
     "text": "Theme.of(context).colorScheme, Material 3 renk rollerini verir "
             "(primary, surface, onSurfaceVariant); sabit renk yazmak yerine bu kullanılır."},
    {"key": "IconButton", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/IconButton-class.html",
     "text": "Material 3, IconButton.filled ve IconButton.filledTonal kurucularını "
             "ekledi; düz IconButton düşük vurgulu eylemler içindir."},
    {"key": "SwitchListTile", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/SwitchListTile-class.html",
     "text": "SwitchListTile bir Switch'i ListTile düzeniyle birleştirir; ayar "
             "ekranlarında satır ve anahtarı ayrı ayrı kurmaya gerek bırakmaz."},
    {"key": "showDialog", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/showDialog.html",
     "text": "showDialog bir Future döndürür ve kapanış değeri Navigator.pop ile verilir; "
             "AlertDialog eylemleri actions listesine konur."},
    {"key": "Expanded", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/widgets/Expanded-class.html",
     "text": "Expanded, Row/Column içinde kalan alanı doldurur. Kaydırılabilir bir "
             "çocuğu sınırsız yükseklikte bırakmamak için gereklidir."},
    {"key": "Card", "kind": "api.flutter.dev",
     "url": "https://api.flutter.dev/flutter/material/Card-class.html",
     "text": "Material 3'te Card varsayılan olarak düşük yükseltilidir ve rengi "
             "surfaceContainerLow'dan alır; gölge yerine renk tonu kullanılır."},
]

# House-style passages. The DeepKwiki corpus is where the team's own conventions
# live, and the served agent retrieves from it alongside the web — so the trained
# block has to contain that shape too, or wiki hits at inference are a surprise.
WIKI_FACTS: list[dict] = [
    {"key": "_View", "kind": "DeepKwiki", "url": "",
     "text": "Ev stili · Ekran yapısı: BlocProvider'ı kuran widget ile ağacı çizen widget "
             "ayrılır. Sağlayıcı ekranın kendisi, çizim ayrı bir _View widget'ıdır."},
    {"key": "dispose", "kind": "DeepKwiki", "url": "",
     "text": "Ev stili · Kaynak sahipliği: bir controller'ı oluşturan onu dispose eder. "
             "Parametre olarak alınan controller çağıranın sorumluluğundadır."},
    {"key": "validator", "kind": "DeepKwiki", "url": "",
     "text": "Ev stili · Formlar: her Form alanı validator taşır ve gönderim "
             "formKey.currentState!.validate() ile korunur."},
]

# Migrations reserved for the held-out set. Chosen as the tail of the decisive-API
# distribution: each appears once or twice, so removing them costs the training
# set almost nothing and buys the only question worth asking of v8 — does it
# follow evidence for an API whose migration it never saw?
HELDOUT_MIGRATIONS = {"SegmentedButton", "DropdownMenu", "NavigationBar", "withValues"}

CANONICAL_STATE = {
    "cubit": "flutter_bloc (Cubit).",
    "bloc": "flutter_bloc (Bloc + event).",
    "stateless": "yok (StatelessWidget).",
}


def classify_state(raw: str) -> str:
    """Collapse v7's free-text State into the three strings the UI can send."""
    low = raw.lower()
    if "bloc + event" in low or "tam bloc" in low or re.search(r"\bbloc\b\s*\+", low):
        return "bloc"
    if "flutter_bloc" in low:
        return "cubit"
    return "stateless"


def repair_setstate(code: str) -> str | None:
    """v7 has three rows using setState, which the contract forbids and the
    frontend linter reports as an error. Training on them teaches the model to
    produce exactly what the checker rejects, so they are dropped rather than
    rewritten: a mechanical rewrite of state management is not a transformation
    this script can do correctly, and a wrong repair is worse than 136 rows."""
    return None if re.search(r"\bsetState\s*\(", code) else code


def facts_for(answer: str) -> tuple[list[dict], list[dict]]:
    """Split the corpus into facts the answer uses and facts it does not."""
    used, unused = [], []
    for f in FACTS + WIKI_FACTS:
        (used if f["key"] in answer else unused).append(f)
    return used, unused


def render_evidence(entries: list[dict]) -> str:
    """Assemble the numbered block. Layout mirrors decision.Agent.gather: one
    header, then `[n] (source) title — url` with the passage on the next line."""
    if not entries:
        return EVIDENCE_HEADER + "\n" + NO_EVIDENCE
    out = [EVIDENCE_HEADER]
    for n, e in enumerate(entries, 1):
        head = f"[{n}] ({e['kind']}) {e['key']}"
        if e.get("url"):
            head += f" — {e['url']}"
        out.append(head)
        out.append(e["text"])
    return "\n".join(out)


def build_row(brief_lines: list[str], answer: str, kind: str,
              entries: list[dict], system: str) -> dict:
    user = render_evidence(entries) + "\n\n" + "\n".join(brief_lines) + "\n\n" + TURN_INSTRUCTION
    return {"messages": [
        {"role": "system", "content": system},
        {"role": "user", "content": user},
        {"role": "assistant", "content": answer},
    ]}


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--src", default=os.path.join(HERE, "data", "flutter_screens_train_v7.jsonl"))
    ap.add_argument("--out", default=os.path.join(HERE, "data", "flutter_screens_train_v8.jsonl"))
    ap.add_argument("--meta", default=os.path.join(HERE, "data", "flutter_screens_train_v8_meta.jsonl"))
    ap.add_argument("--out-eval", default=os.path.join(HERE, "data", "flutter_screens_eval_v8.jsonl"))
    ap.add_argument("--meta-eval", default=os.path.join(HERE, "data", "flutter_screens_eval_v8_meta.jsonl"))
    ap.add_argument("--eval-extra", type=int, default=14,
                    help="non-migration rows added to eval so the loss curve is "
                         "measured on the training distribution, not on the hard slice")
    ap.add_argument("--thin-share", type=float, default=0.15)
    ap.add_argument("--max-distractors", type=int, default=2)
    ap.add_argument("--seed", type=int, default=20260727)
    args = ap.parse_args()

    if not os.path.exists(args.src):
        sys.exit(f"source not found: {args.src}\nRun ./fetch_dataset.sh first.")

    rng = random.Random(args.seed)
    src = [json.loads(l) for l in open(args.src, encoding="utf-8") if l.strip()]
    system = src[0]["messages"][0]["content"]

    rows, metas = [], []
    dropped = 0
    counts: collections.Counter = collections.Counter()

    for r in src:
        brief, answer = r["messages"][1]["content"], r["messages"][2]["content"]
        answer = repair_setstate(answer)
        if answer is None:
            dropped += 1
            continue

        # Rewrite the brief: canonical State, and the label the UI actually sends.
        lines, state_id = [], "stateless"
        for line in brief.split("\n"):
            if not line.strip():
                continue
            if line.startswith("State:"):
                state_id = classify_state(line.split(":", 1)[1])
                lines.append("State: " + CANONICAL_STATE[state_id])
            elif line.startswith("Alanlar:"):
                lines.append("Alanlar/İçerik:" + line.split(":", 1)[1])
            else:
                lines.append(line)

        used, unused = facts_for(answer)
        migrations = [f for f in used if "replaces" in f]

        if rng.random() < args.thin_share:
            kind, entries = "thin", []
        elif migrations:
            # Lead with one migration fact so the decisive evidence is present,
            # then fill with the rest of what the answer uses.
            lead = rng.choice(migrations)
            entries = [lead] + [f for f in used if f is not lead]
            kind = "migration"
        elif used:
            kind, entries = "grounded", list(used)
        else:
            kind, entries = "thin", []

        if entries:
            entries = entries[:4] + rng.sample(unused, min(args.max_distractors, len(unused)))
            rng.shuffle(entries)

        counts[kind] += 1
        rows.append(build_row(lines, answer, kind, entries, system))
        metas.append({
            "kind": kind,
            "state": state_id,
            "n_sources": len(entries),
            "decisive": next((e["key"] for e in entries if "replaces" in e and e in used), None),
            "distractors": [e["key"] for e in entries if e in unused],
        })

    # --- the split -----------------------------------------------------------
    # Not random. The question v8 has to answer is whether the model follows
    # evidence *in general* or has merely memorised the two migrations that
    # dominate the data (FilledButton 21, titleLarge 16 of 48). So the held-out
    # set takes every migration whose decisive API is in HELDOUT_MIGRATIONS —
    # APIs the model will therefore never have seen migrated during training.
    # If it still writes SegmentedButton when the evidence says ToggleButtons is
    # Material 2, it read the block. If it does not, v8 learned two names.
    #
    # A random split cannot ask that question: it would put FilledButton rows on
    # both sides and report a high score for memorisation.
    eval_idx = {i for i, m in enumerate(metas)
                if m["decisive"] in HELDOUT_MIGRATIONS}
    # Plus a small sample of the other kinds, so eval loss during training is
    # measured on the same distribution as training rather than on migrations
    # alone — otherwise the loss curve tracks the hardest slice and looks broken.
    rest = [i for i in range(len(rows)) if i not in eval_idx]
    eval_idx |= set(rng.sample(rest, min(args.eval_extra, len(rest))))

    def write(path: str, items: list, idx: set) -> None:
        with open(path, "w", encoding="utf-8") as fh:
            for i, item in enumerate(items):
                if i in idx:
                    fh.write(json.dumps(item, ensure_ascii=False) + "\n")

    train_idx = {i for i in range(len(rows)) if i not in eval_idx}
    write(args.out, rows, train_idx)
    write(args.meta, metas, train_idx)
    write(args.out_eval, rows, eval_idx)
    write(args.meta_eval, metas, eval_idx)

    held = collections.Counter(metas[i]["decisive"] for i in eval_idx if metas[i]["decisive"])
    print(f"  {os.path.relpath(args.out, HERE)}: {len(train_idx)} rows "
          f"(+ {os.path.basename(args.meta)})")
    print(f"  {os.path.relpath(args.out_eval, HERE)}: {len(eval_idx)} rows "
          f"(+ {os.path.basename(args.meta_eval)})")
    if dropped:
        print(f"  dropped {dropped} row(s) using setState — the contract forbids it")
    print("\nrow kinds: " + ", ".join(f"{k} {v}" for k, v in counts.most_common()))
    print("held-out migrations: " + (", ".join(f"{k} {v}" for k, v in held.most_common()) or "none"))
    print("system prompt sha256: " + hashlib.sha256(system.encode()).hexdigest())
    print("\nThe system prompt is unchanged from v7, so SYSTEM_PROMPT_SHA256 in "
          "flutterContract.ts still holds.\nThe user message is not: the frontend must "
          "send the evidence block too once this\nadapter is served. Until the backend "
          "codegen agent exists, an empty block\n(the NO_EVIDENCE line) is the correct "
          "thing to send — it is in the training set.")


if __name__ == "__main__":
    main()
