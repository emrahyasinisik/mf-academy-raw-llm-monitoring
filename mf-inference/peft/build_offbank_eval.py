#!/usr/bin/env python3
"""Turn the hand-written off-bank cases into an eval set rubric_eval.py can read.

The point of the exercise is in offbank_cases.py's docstring: the published
held-out split shares 100% of its evidence text with the training split, so
`present_score_mae` there cannot distinguish reading from recall. These rows
have no text in common with the bank at all, and this script *proves* that
rather than asserting it — `--train` is checked quote by quote and the build
fails if a single one turns up on both sides.

Everything else here exists to make the two sets comparable. The prompt is
fetched from the running backend, not copied, for the same reason
build_dataset.py fetches it: a local copy drifts the moment either side is
edited, and what comes out is an eval set measuring a prompt nothing sends.
The quote neutralisation, the bare-JSON answer with no fence, the compact
separators — all mirrored, because a difference in any of them would show up as
a score difference and be read as a difference in the model.

Usage:
    PORT=8090 go run ./cmd/server &          # from mf-backend/
    export BASE_URL=http://localhost:8090 TOKEN=<a token>
    python3 build_offbank_eval.py --out data/offbank_investment.jsonl \
        --train data/rubric_train.jsonl
"""

from __future__ import annotations

import argparse
import json
import os
import sys

from build_dataset import fetch_prompt, neutralise_quotes, render_user_message
from offbank_cases import CASES

DOMAIN = "startup-investability"


def unwrap(text: str) -> str:
    """Undo the source file's hard wrapping, keeping section breaks.

    The cases are written wrapped to fit a source file, which is a property of
    the editor and not of the document. Left in, the wrap points end up inside
    evidence spans: a quote that reads as one sentence contains a newline in the
    middle, so it never matches the case verbatim, and the invariant that every
    citation is real would have to be relaxed to accommodate a text width.

    Headings are all-caps lines and stay on their own; everything else in a
    paragraph joins with a single space.
    """
    out, para = [], []

    def flush() -> None:
        if para:
            out.append(" ".join(para))
            para.clear()

    for raw in text.splitlines():
        line = raw.strip()
        if not line:
            flush()
            out.append("")
        elif line == line.upper() and any(ch.isalpha() for ch in line):
            flush()
            out.append(line)
        else:
            para.append(line)
    flush()

    # Collapse the runs of blank lines the flushing leaves behind.
    cleaned: list[str] = []
    for line in out:
        if line or (cleaned and cleaned[-1]):
            cleaned.append(line)
    return "\n".join(cleaned).strip()


def collect_quotes(path: str) -> set[str]:
    """Every evidence span the bank's rows use as a target."""
    quotes: set[str] = set()
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            if not line.strip():
                continue
            row = json.loads(line)
            answer = json.loads(
                next(m["content"] for m in row["messages"]
                     if m["role"] == "assistant"))
            for f in answer["findings"]:
                for q in f.get("evidence") or []:
                    quotes.add(q)
    return quotes


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", "http://localhost:8090"))
    ap.add_argument("--token", default=os.environ.get("TOKEN", ""))
    ap.add_argument("--out", default="data/offbank_investment.jsonl")
    ap.add_argument("--train", default="data/rubric_train.jsonl",
                    help="the bank's training split, to prove no text is shared")
    args = ap.parse_args()

    if not args.token:
        sys.exit("a token is required: --token or TOKEN=... "
                 "(any registered account will do)")

    spec = fetch_prompt(args.base_url, args.token, DOMAIN)
    criteria = spec["criteria"]
    keys = [c["key"] for c in criteria]
    template = spec["user_prompt_example"]
    if "{{subject}}" not in template:
        sys.exit("the backend's user template lost its placeholder; "
                 "check analysis.Handler.Prompt")

    bank = collect_quotes(args.train) if os.path.exists(args.train) else set()
    if not bank:
        print(f"NOTE: {args.train} not found — the off-bank proof is skipped, "
              f"which is the one check this file exists for. Regenerate the "
              f"bank sets and re-run before trusting the output.")

    rows, stats = [], {"present": 0, "absent": 0}
    for case in CASES:
        text = unwrap(neutralise_quotes(case["text"]))
        title = neutralise_quotes(case["title"])
        by_key = {f["key"]: f for f in case["findings"]}

        unknown = set(by_key) - set(keys)
        assert not unknown, (
            f"{case['title']}: rubric has no criterion {sorted(unknown)}. "
            f"The rubric is the backend's; edit offbank_cases.py, not this list.")

        findings = []
        # Emitted in the rubric's own order, and absences written out rather
        # than left to omission: a reader of the jsonl should not have to know
        # which keys the rubric has to know which ones this case ducked.
        for key in keys:
            f = by_key.get(key)
            if f is None or f.get("score") is None:
                findings.append({
                    "key": key, "evidence_found": False, "score": None,
                    "evidence": [],
                    "rationale": neutralise_quotes(
                        f["rationale"] if f else
                        "Vaka metni bu konuya hic deginmiyor, degerlendirilemedi."),
                })
                stats["absent"] += 1
                continue

            spans = [neutralise_quotes(q) for q in f["evidence"]]
            assert spans, f"{case['title']}/{key}: scored but cites nothing"
            for q in spans:
                # The invariant the whole product's credibility rests on: a
                # quote that is not in the case is a fabricated citation, and
                # training or measuring against one teaches exactly the failure
                # the rubric exists to prevent.
                assert q in text, (
                    f"{case['title']}/{key}: quote not found verbatim in the "
                    f"case text:\n  {q!r}")
                if q in bank:
                    sys.exit(
                        f"{case['title']}/{key}: this quote is also in the "
                        f"bank's training split, so the case is not off-bank:\n"
                        f"  {q!r}\nRewrite the sentence.")
            findings.append({
                "key": key, "evidence_found": True, "score": f["score"],
                "evidence": spans,
                "rationale": neutralise_quotes(f["rationale"]),
            })
            stats["present"] += 1

        target = json.dumps({"findings": findings},
                            ensure_ascii=False, separators=(",", ":"))
        rows.append({"messages": [
            {"role": "system", "content": spec["system_prompt"]},
            {"role": "user", "content": render_user_message(template, title, text)},
            {"role": "assistant", "content": target},
        ]})

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    with open(args.out, "w", encoding="utf-8") as fh:
        for r in rows:
            fh.write(json.dumps(r, ensure_ascii=False) + "\n")

    total = stats["present"] + stats["absent"]
    share = stats["absent"] / total if total else 0
    print(f"{args.out}: {len(rows)} cases, {total} findings, "
          f"{stats['absent']} absent ({share:.0%})")

    if bank:
        mine = {q for r in rows
                for f in json.loads(r["messages"][2]["content"])["findings"]
                for q in f["evidence"]}
        print(f"off-bank proof: {len(mine)} distinct quotes, "
              f"{len(mine & bank)} shared with {args.train} (must be 0)")

    dist: dict[int, int] = {}
    for r in rows:
        for f in json.loads(r["messages"][2]["content"])["findings"]:
            if f["score"] is not None:
                dist[f["score"]] = dist.get(f["score"], 0) + 1
    print("score dagilimi:", " ".join(f"{k}:{dist.get(k, 0)}" for k in range(1, 6)))
    # A 3 has to be reachable, and for the same reason it had to be added to the
    # bank: the base's documented failure is answering 3 when it has no evidence,
    # so a set with no 3s lets an adapter score well by learning "never say 3"
    # rather than the behaviour anyone wanted.
    if not dist.get(3):
        print("WARNING: hic 3 yok — 'asla 3 deme' ogrenen bir adapter bu sette "
              "iyi gorunur ve olcum bunu ayirt edemez")


if __name__ == "__main__":
    main()
