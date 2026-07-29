#!/usr/bin/env python3
"""Measure how long the training rows actually are, in the tokeniser that will train them.

Why this is a script and not a guess
------------------------------------
`--max-seq-len` decides two things at once: how much GPU memory a step costs,
and which rows get clipped. Clipping in train_qlora_qwen.py happens from the
left, so an over-tight bound eats the front of a case — the row still trains,
still reports a normal loss, and quietly teaches the model to answer about
evidence it was not shown. The failure is invisible in every number the training
run prints, which is exactly why the bound is measured rather than chosen.

A number measured on one tokeniser does not transfer to another. Gemma-2's
vocabulary is 256k against Qwen3's 152k, and Turkish text with its suffixes
segments differently in each; the rubric line's 2560 was measured on Gemma and
means nothing here. Neither does Flutter v8's 2560, which was measured on Qwen3
but against Dart source.

What to do with the output
--------------------------
Set `--max-seq-len` at or above p100 if it fits in memory. If it does not, the
question to ask is not "what fits" but "how many rows lose evidence" — the
clipped count at each candidate is printed for that reason.

Usage:
    python3 measure_tokens.py --data data/rubric_train.jsonl
"""

from __future__ import annotations

import argparse
import json
import os
import sys

try:
    from transformers import AutoTokenizer
except ImportError:
    sys.exit("missing dependency: transformers\n"
             "install with: pip install transformers")


def render(tokenizer, system: str, user: str) -> str:
    """The exact prompt text train_qlora_qwen.py builds, so lengths match it."""
    msgs = [{"role": "system", "content": system},
            {"role": "user", "content": user}]
    try:
        return tokenizer.apply_chat_template(
            msgs, tokenize=False, add_generation_prompt=True, enable_thinking=False)
    except TypeError:
        return tokenizer.apply_chat_template(
            msgs, tokenize=False, add_generation_prompt=True)


def main() -> None:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--data", default="data/rubric_train.jsonl")
    ap.add_argument("--base-model", default="Qwen/Qwen3-4B-Instruct-2507")
    ap.add_argument("--candidates", default="2048,2560,3072,3584,4096",
                    help="lengths to report a clipped-row count for")
    args = ap.parse_args()

    if not os.path.exists(args.data):
        sys.exit(f"{args.data} not found")

    tokenizer = AutoTokenizer.from_pretrained(args.base_model)
    # Qwen3 closes a turn with <|im_end|>, and the trainer includes it in the
    # target — so a length measured without it is short by one token on every
    # row, which is the difference between fitting and clipping at the boundary.
    stop = "<|im_end|>"

    prompt_lens: list[int] = []
    answer_lens: list[int] = []
    with open(args.data, encoding="utf-8") as fh:
        for line in fh:
            if not line.strip():
                continue
            msgs = json.loads(line)["messages"]
            system = next(m["content"] for m in msgs if m["role"] == "system")
            user = next(m["content"] for m in msgs if m["role"] == "user")
            target = next(m["content"] for m in msgs if m["role"] == "assistant")

            prompt_lens.append(len(tokenizer(render(tokenizer, system, user),
                                             add_special_tokens=False)["input_ids"]))
            answer_lens.append(len(tokenizer(target + stop,
                                             add_special_tokens=False)["input_ids"]))

    if not prompt_lens:
        sys.exit(f"{args.data} has no rows")

    totals = sorted(p + a for p, a in zip(prompt_lens, answer_lens))
    n = len(totals)

    def pct(p: float) -> int:
        return totals[min(n - 1, int(n * p))]

    print(f"{n} rows from {args.data}\n")
    print(f"  prompt   mean {sum(prompt_lens) // n:5}  max {max(prompt_lens):5}")
    print(f"  answer   mean {sum(answer_lens) // n:5}  max {max(answer_lens):5}")
    print(f"  total    mean {sum(totals) // n:5}  p50 {pct(0.50):5}  "
          f"p95 {pct(0.95):5}  p99 {pct(0.99):5}  p100 {totals[-1]:5}\n")

    # The answer is what must survive; a row clipped past its prompt teaches
    # nothing and the trainer errors on it rather than training it.
    print("  max-seq-len   rows clipped   rows losing their answer")
    for c in (int(x) for x in args.candidates.split(",")):
        clipped = sum(1 for t in totals if t > c)
        gutted = sum(1 for a in answer_lens if a > c)
        print(f"  {c:11}   {clipped:12}   {gutted:23}")

    print(f"\nSmallest bound that clips nothing: {totals[-1]}")


if __name__ == "__main__":
    main()
