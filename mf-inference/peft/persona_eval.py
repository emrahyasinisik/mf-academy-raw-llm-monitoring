#!/usr/bin/env python3
"""Measure what the persona fine-tune actually changed.

compare.py runs the rubric task through the backend's /analysis/trial route.
The persona cannot be measured that way: at inference it researches live, so the
evidence — and therefore the answer — is different every run, and a metric that
moves for that reason measures the web, not the model.

So this harness holds the evidence fixed. It replays the held-out set
(persona_eval.jsonl) straight against the inference host's OpenAI endpoint —
the same [system, user] the agent would send, minus the live research — and
scores the completion against the ground truth the generator recorded
(persona_eval_meta.jsonl). Two models, same prompts, one difference.

Four numbers, in the order they matter
--------------------------------------
citation_valid   Share of answers whose every [n] citation exists in the
                 evidence it was given. The base model invents citation numbers
                 and cites sources it was not shown; nothing else matters if a
                 verdict cannot be traced to what it saw.

grounded_format  Share of DECIDE cases answered in the KARAR/SKOR/GEREKÇE shape
                 the UI parses. Content can be right and still unusable if the
                 verdict is buried in prose.

asked_when_thin  Share of CLARIFY cases the model answered with a question
                 instead of a verdict. This is the behaviour the product exists
                 to get right: not guessing when the decisive fact is absent.

decision_match   Share of DECIDE cases whose verdict label matches the one the
                 evidence implies. Softer than the rest — a defensible verdict
                 can differ by one band — so it is read last and as a trend.

Usage:
    persona_eval.py --before gemma-2-2b-it-q4f16_1-MLC --after persona-v1
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.request

CITATION_RE = re.compile(r"\[(\d+)\]")
KARAR_RE = re.compile(r"KARAR:\s*([^\n]+)", re.IGNORECASE)


def chat(base_url: str, api_key: str, model: str, messages: list[dict],
         timeout: int = 300) -> str:
    """One completion from the inference host's OpenAI-compatible endpoint.

    Sends both header styles for the same reason internal/llm does: the Caddy
    gateway checks X-API-Key, a hosted provider checks the bearer token, and
    sending both keeps the URL swappable.
    """
    payload = {"model": model, "messages": messages,
               "temperature": 0.2, "max_tokens": 1024, "stream": False}
    req = urllib.request.Request(
        f"{base_url.rstrip('/')}/v1/chat/completions",
        data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json",
                 "X-API-Key": api_key,
                 "Authorization": f"Bearer {api_key}"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = json.load(resp)
        return body["choices"][0]["message"]["content"]
    except urllib.error.HTTPError as e:
        sys.exit(f"inference call failed ({e.code}):\n"
                 f"{e.read().decode(errors='replace')[:400]}")
    except urllib.error.URLError as e:
        sys.exit(f"could not reach {base_url}: {e.reason}")


def load_jsonl(path: str) -> list[dict]:
    with open(path, encoding="utf-8") as fh:
        return [json.loads(line) for line in fh if line.strip()]


def citations_valid(text: str, n_sources: int) -> bool:
    nums = [int(m) for m in CITATION_RE.findall(text)]
    if not nums:
        # A decide answer with no citation is not "valid grounding" — but this
        # metric is specifically about invented numbers, and the format metric
        # already punishes a citation-free verdict. No numbers cannot be wrong.
        return True
    return all(1 <= x <= n_sources for x in nums)


def label_of(text: str) -> str | None:
    m = KARAR_RE.search(text)
    return m.group(1).strip().lower() if m else None


def score_one(text: str, meta: dict) -> dict:
    n = meta["n_sources"]
    out = {"citation_valid": citations_valid(text, n)}
    if meta["mode"] == "decide":
        has_verdict = "KARAR:" in text.upper() and "SKOR:" in text.upper()
        out["grounded_format"] = has_verdict
        want = (meta["label"] or "").lower()
        got = label_of(text) or ""
        out["decision_match"] = bool(want) and want in got
    else:  # clarify
        asked = ("?" in text) and ("KARAR:" not in text.upper())
        out["asked_when_thin"] = asked
    return out


def run_side(base_url: str, api_key: str, label: str, model: str,
             examples: list[dict], metas: list[dict], limit: int) -> dict:
    print(f"\n  {label}: {model}")
    agg = {"citation_valid": [], "grounded_format": [],
           "asked_when_thin": [], "decision_match": []}
    n = min(limit, len(examples)) if limit else len(examples)
    for i in range(n):
        prompt = [m for m in examples[i]["messages"] if m["role"] != "assistant"]
        text = chat(base_url, api_key, model, prompt)
        s = score_one(text, metas[i])
        for k, v in s.items():
            agg[k].append(v)
        if (i + 1) % 10 == 0:
            print(f"    {i + 1}/{n}")

    def rate(k: str) -> float | None:
        xs = agg[k]
        return sum(1 for x in xs if x) / len(xs) if xs else None

    out = {k: rate(k) for k in agg}
    out["n"] = n
    print(f"    citation_valid {fmt(out['citation_valid'])}   "
          f"grounded_format {fmt(out['grounded_format'])}   "
          f"asked_when_thin {fmt(out['asked_when_thin'])}   "
          f"decision_match {fmt(out['decision_match'])}")
    return out


def fmt(v) -> str:
    return "n/a" if v is None else f"{v:.2f}"


def delta(before, after) -> str:
    if before is None or after is None:
        return "—"
    d = after - before
    if abs(d) < 1e-9:
        return "  0.00"
    return f"{'▲' if d > 0 else '▼'} {d:+.2f}"


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base-url", default=os.environ.get("LLM_BASE_URL", ""),
                    help="inference host root, e.g. https://host (no /v1); or LLM_BASE_URL")
    ap.add_argument("--api-key", default=os.environ.get("LLM_API_KEY", ""))
    ap.add_argument("--before", default="gemma-2-2b-it-q4f16_1-MLC")
    ap.add_argument("--after", required=True, help="the tuned model id")
    ap.add_argument("--eval", default="data/persona_eval.jsonl")
    ap.add_argument("--meta", default="data/persona_eval_meta.jsonl")
    ap.add_argument("--limit", type=int, default=40,
                    help="examples per side (0 = all); each is one GPU call")
    args = ap.parse_args()

    if not args.base_url:
        sys.exit("an inference host is required: --base-url or LLM_BASE_URL")

    examples = load_jsonl(args.eval)
    metas = load_jsonl(args.meta)
    if len(examples) != len(metas):
        sys.exit(f"eval and meta lengths differ ({len(examples)} vs {len(metas)}); "
                 "regenerate both with build_persona_dataset.py")

    before = run_side(args.base_url, args.api_key, "before", args.before,
                      examples, metas, args.limit)
    after = run_side(args.base_url, args.api_key, "after", args.after,
                     examples, metas, args.limit)

    rows = [
        ("citation_valid", "invented citations gone"),
        ("grounded_format", "verdict in KARAR/SKOR shape"),
        ("asked_when_thin", "asks instead of guessing"),
        ("decision_match", "verdict band matches evidence"),
    ]
    print("\n" + "=" * 64)
    print(f"  {'metric':<18} {'before':>8} {'after':>8} {'Δ':>10}   note")
    print("  " + "-" * 60)
    for key, note in rows:
        print(f"  {key:<18} {fmt(before[key]):>8} {fmt(after[key]):>8} "
              f"{delta(before[key], after[key]):>10}   {note}")
    print("=" * 64)

    if after["citation_valid"] is not None and before["citation_valid"] is not None \
            and after["citation_valid"] < before["citation_valid"]:
        print("\nWARNING: grounding got WORSE. Do not ship this adapter.")


if __name__ == "__main__":
    main()
