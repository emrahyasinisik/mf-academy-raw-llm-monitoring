#!/usr/bin/env python3
"""Measure whether the Flutter adapter reads the evidence it is given.

The frontend's lintDart tells you the output contains no known-bad pattern. That
was the whole quality gate for v7 and it cannot answer v8's question, which is
not "is this code clean" but "did the block above change what the model wrote".

The held-out set is built to answer it. build_flutter_dataset_v8.py reserves
four migrations — SegmentedButton, DropdownMenu, NavigationBar, withValues —
so the model never sees those APIs migrated during training, while FilledButton
and titleLarge migrations appear on both sides. That gives two numbers whose
*gap* is the result:

    followed_seen     evidence obeyed for a migration seen in training
    followed_unseen   evidence obeyed for one it has never seen

Equal, and the model learned the rule. A large gap, and it memorised two API
names and v8 bought nothing the fact corpus could not have hard-coded. This is
the measurement that decides whether the evidence pipeline is worth building
out, so it is the one worth being honest about.

The supporting numbers are the v7 gate, kept so a gain in grounding that costs
code quality is visible rather than hidden:

    clean            no error-severity lint finding
    fenced           answered inside a ```dart block, as the contract requires
    complete         the fence closed — this model's first failure is truncation

Two backends. `hf` loads the base model and adapter directly and is what the
Kaggle notebook uses: it produces numbers minutes after training, without the
merge and MLC compile that serving needs. `openai` replays against the inference
host the way persona_eval.py does, for scoring what is actually deployed.

    python3 flutter_eval.py --backend hf --adapter out/flutter-v8
    python3 flutter_eval.py --backend openai --model qwen3-4b-flutter-v8-q4f16_1-MLC
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))

# Ported from PATTERNS in mf-frontend/src/lib/flutterContract.ts. Only the
# error-severity rules: the two warnings there are ambiguous by design and a
# metric that counts them would move for reasons unrelated to the model.
BAD = [
    (r"\bRaisedButton\b", "RaisedButton"),
    (r"\bFlatButton\b", "FlatButton"),
    (r"\bwithOpacity\s*\(", "withOpacity"),
    (r"\bheadline6\b", "headline6"),
    (r"\bBottomNavigationBar\b", "BottomNavigationBar"),
    (r"\bmarkNeedsBuild\s*\(", "markNeedsBuild"),
    (r"\bsetState\s*\(", "setState"),
    (r"\bTODO\b", "TODO"),
    (r"[＀-￯]", "full-width character"),
]

FENCE_OPEN = re.compile(r"```dart\b")
FENCE_ANY = re.compile(r"```")

# The deprecated ancestor for each modern API, mirroring FACTS in the generator.
# Used to score a migration: the replacement must appear and the removed API
# must not.
REPLACES = {
    "FilledButton": "RaisedButton",
    "TextButton": "FlatButton",
    "NavigationBar": "BottomNavigationBar",
    "titleLarge": "headline6",
    "withValues": "withOpacity",
    "ScaffoldMessenger": "Scaffold.of(context).showSnackBar",
    "SegmentedButton": "ToggleButtons",
    "DropdownMenu": "DropdownButton",
}

# Kept in step with HELDOUT_MIGRATIONS in build_flutter_dataset_v8.py.
HELDOUT = {"SegmentedButton", "DropdownMenu", "NavigationBar", "withValues"}


def load_jsonl(path: str) -> list[dict]:
    if not os.path.exists(path):
        sys.exit(f"{path} not found — run build_flutter_dataset_v8.py first")
    with open(path, encoding="utf-8") as fh:
        return [json.loads(line) for line in fh if line.strip()]


def extract_code(text: str) -> tuple[str, bool, bool]:
    """Return (code, fenced, complete)."""
    m = FENCE_OPEN.search(text)
    if not m:
        return text, False, False
    rest = text[m.end():]
    close = FENCE_ANY.search(rest)
    if not close:
        return rest, True, False
    return rest[:close.start()], True, True


def score_one(text: str, meta: dict) -> dict:
    code, fenced, complete = extract_code(text)
    out = {
        "fenced": fenced,
        "complete": complete,
        "clean": not any(re.search(p, code) for p, _ in BAD),
    }
    key = meta.get("decisive")
    if key and key in REPLACES:
        old = REPLACES[key]
        followed = (key in code) and (old not in code)
        out["followed_unseen" if key in HELDOUT else "followed_seen"] = followed
    return out


# --- backends ---------------------------------------------------------------

def gen_hf(rows: list[dict], base: str, adapter: str | None, max_new: int):
    import torch
    from transformers import AutoModelForCausalLM, AutoTokenizer

    tok = AutoTokenizer.from_pretrained(base)
    model = AutoModelForCausalLM.from_pretrained(
        base, torch_dtype=torch.float16, device_map={"": 0})
    if adapter:
        from peft import PeftModel
        model = PeftModel.from_pretrained(model, adapter)
    model.eval()

    for r in rows:
        msgs = [m for m in r["messages"] if m["role"] != "assistant"]
        try:
            prompt = tok.apply_chat_template(
                msgs, tokenize=False, add_generation_prompt=True,
                enable_thinking=False)
        except TypeError:
            prompt = tok.apply_chat_template(
                msgs, tokenize=False, add_generation_prompt=True)
        ids = tok(prompt, return_tensors="pt").to(model.device)
        with torch.no_grad():
            # Greedy. The metric must move because the model changed, not
            # because sampling landed differently between two runs.
            out = model.generate(**ids, max_new_tokens=max_new, do_sample=False)
        yield tok.decode(out[0][ids["input_ids"].shape[1]:], skip_special_tokens=True)


def gen_openai(rows: list[dict], base_url: str, api_key: str, model: str, max_new: int):
    import urllib.request

    for r in rows:
        msgs = [m for m in r["messages"] if m["role"] != "assistant"]
        payload = {"model": model, "messages": msgs,
                   "temperature": 0.0, "max_tokens": max_new}
        req = urllib.request.Request(
            base_url.rstrip("/") + "/v1/chat/completions",
            data=json.dumps(payload).encode(),
            headers={"Content-Type": "application/json",
                     "X-API-Key": api_key,
                     "Authorization": f"Bearer {api_key}"})
        with urllib.request.urlopen(req, timeout=300) as resp:
            body = json.load(resp)
        yield body["choices"][0]["message"]["content"]


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--eval", default=os.path.join(HERE, "data", "flutter_screens_eval_v8.jsonl"))
    ap.add_argument("--meta", default=os.path.join(HERE, "data", "flutter_screens_eval_v8_meta.jsonl"))
    ap.add_argument("--backend", choices=("hf", "openai"), default="hf")
    ap.add_argument("--base-model", default="Qwen/Qwen3-4B-Instruct-2507")
    ap.add_argument("--adapter", default=None,
                    help="hf backend: adapter dir. Omit to score the base model, "
                         "which is the before-number this is compared against")
    ap.add_argument("--model", default=None, help="openai backend: served model id")
    ap.add_argument("--base-url", default=os.environ.get("LLM_BASE_URL", ""))
    ap.add_argument("--api-key", default=os.environ.get("LLM_API_KEY", ""))
    ap.add_argument("--max-new-tokens", type=int, default=1600)
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--dump", default=None, help="write raw completions here")
    args = ap.parse_args()

    rows = load_jsonl(args.eval)
    metas = load_jsonl(args.meta)
    if len(rows) != len(metas):
        sys.exit(f"eval has {len(rows)} rows but meta has {len(metas)} — "
                 f"regenerate both, do not move one file by hand")
    if args.limit:
        rows, metas = rows[:args.limit], metas[:args.limit]

    if args.backend == "hf":
        stream = gen_hf(rows, args.base_model, args.adapter, args.max_new_tokens)
        label = args.adapter or f"{args.base_model} (base)"
    else:
        if not args.base_url or not args.model:
            sys.exit("openai backend needs --base-url/LLM_BASE_URL and --model")
        stream = gen_openai(rows, args.base_url, args.api_key, args.model,
                            args.max_new_tokens)
        label = args.model

    agg: dict[str, list[bool]] = {}
    dumped = []
    for i, (text, meta) in enumerate(zip(stream, metas), 1):
        s = score_one(text, meta)
        for k, v in s.items():
            agg.setdefault(k, []).append(v)
        dumped.append({"i": i, "meta": meta, "completion": text})
        print(f"  [{i}/{len(rows)}] {meta['kind']:9s} "
              f"{'/'.join(k for k, v in s.items() if v)}")

    if args.dump:
        with open(args.dump, "w", encoding="utf-8") as fh:
            for d in dumped:
                fh.write(json.dumps(d, ensure_ascii=False) + "\n")

    def rate(k: str) -> str:
        vals = agg.get(k)
        if not vals:
            return "   n/a"
        return f"{100*sum(vals)/len(vals):5.1f}%  (n={len(vals)})"

    print(f"\n=== {label} ===")
    print(f"  followed_unseen  {rate('followed_unseen')}   <- the result")
    print(f"  followed_seen    {rate('followed_seen')}")
    print(f"  clean            {rate('clean')}")
    print(f"  fenced           {rate('fenced')}")
    print(f"  complete         {rate('complete')}")

    seen, unseen = agg.get("followed_seen"), agg.get("followed_unseen")
    if seen and unseen:
        gap = (sum(seen) / len(seen)) - (sum(unseen) / len(unseen))
        print(f"\n  memorisation gap {100*gap:+.1f} points")
        if gap > 0.3:
            print("  A gap this wide means the adapter learned the two API names "
                  "that dominate\n  the training set, not the rule. The evidence "
                  "pipeline is not yet earning\n  its cost — widen the migration "
                  "variety before building the backend agent.")


if __name__ == "__main__":
    main()
