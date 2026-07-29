#!/usr/bin/env python3
"""Merge the per-rubric training sets into the one the adapter is trained on.

Why one adapter over two rubrics rather than one adapter each
-------------------------------------------------------------
The two rubrics ask for different criteria, but they ask for the same
*behaviour*: fill this schema, quote the case, and when the case is silent on a
criterion say so instead of inventing a middle rating. That behaviour is what
the base model measures 0 on, and it is not rubric-specific — a model that has
learned it on nine investment criteria has learned it, not learned nine facts.

Training separately would also mean two adapters to build, measure, publish and
hot-swap between for what is one product surface, and the runtime holds adapters
per base model rather than per screen.

Why the mix is shuffled rather than concatenated
------------------------------------------------
The Trainer shuffles the training split anyway, so for training this changes
nothing. It matters for *eval*: the eval split is consumed in order, and a
concatenated file evaluates 100 investment rows and then 100 marketing rows.
Any per-step eval view of the loss would then show a step change at the seam
that is a change of rubric, not a change of model — the kind of chart somebody
later reads as a training instability.

The seed is fixed so the mix is reproducible from the same inputs, which is the
same promise the generators make.

Usage, from mf-inference/peft/:
    python3 merge_rubric_sets.py \
        --input data/investment --input data/marketing --out-dir data
"""

from __future__ import annotations

import argparse
import json
import os
import random
import sys


def load_jsonl(path: str) -> list[dict]:
    if not os.path.exists(path):
        sys.exit(f"{path} not found — run build_dataset.py for that rubric first")
    with open(path, encoding="utf-8") as fh:
        return [json.loads(line) for line in fh if line.strip()]


def describe(rows: list[dict]) -> str:
    """Absent-finding share, which is the number this whole line exists to move.

    Reported per merged split so a mix that silently under-represents the absent
    branch is visible here rather than three hours later in the eval.
    """
    total = absent = 0
    for r in rows:
        try:
            out = json.loads(next(m["content"] for m in r["messages"]
                                  if m["role"] == "assistant"))
        except (StopIteration, json.JSONDecodeError):
            continue
        for f in out.get("findings", []):
            total += 1
            if not f.get("evidence_found"):
                absent += 1
    if not total:
        return "no findings"
    return f"{total} findings, {absent} absent ({100 * absent // total}%)"


def main() -> None:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--input", action="append", required=True, metavar="DIR",
                    help="a directory holding train.jsonl and eval.jsonl; "
                         "repeat once per rubric")
    ap.add_argument("--out-dir", default="data")
    ap.add_argument("--prefix", default="rubric",
                    help="output basename: <prefix>_train.jsonl / _eval.jsonl")
    ap.add_argument("--seed", type=int, default=20260729)
    args = ap.parse_args()

    if len(args.input) < 2:
        sys.exit("merging one input is a copy; pass --input twice or more")

    os.makedirs(args.out_dir, exist_ok=True)
    rng = random.Random(args.seed)

    for split in ("train", "eval"):
        merged: list[dict] = []
        for d in args.input:
            rows = load_jsonl(os.path.join(d, f"{split}.jsonl"))
            print(f"  {d}/{split}.jsonl: {len(rows)} rows")
            merged.extend(rows)

        rng.shuffle(merged)
        out = os.path.join(args.out_dir, f"{args.prefix}_{split}.jsonl")
        with open(out, "w", encoding="utf-8") as fh:
            for row in merged:
                fh.write(json.dumps(row, ensure_ascii=False) + "\n")
        print(f"{out}: {len(merged)} rows — {describe(merged)}\n")


if __name__ == "__main__":
    main()
