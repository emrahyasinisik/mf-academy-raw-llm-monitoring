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
    persona_eval.py --before qwen3-4b-instruct-q4f16_1-MLC --after persona-v1
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


def local_completer(base_model: str, adapter: str, four_bit: bool,
                    max_new: int):
    """A `complete` that runs the weights here instead of calling a host.

    Kaggle has no inference server, so the only place the adapter can be
    measured in the session that trained it is in-process. The decode settings
    mirror rubric_eval.py — greedy, same chat template path — because the two
    instruments are read side by side and a sampled answer would make the
    persona look unstable next to a rubric measured deterministically.

    Note this is NOT the same measurement as the --base-url path: that one
    scores the MLC build the product actually serves, quantisation and all.
    This one scores the adapter. When they disagree, the served build is the
    one that is true about the product.
    """
    try:
        import torch
        from transformers import (AutoModelForCausalLM, AutoTokenizer,
                                  BitsAndBytesConfig)
    except ImportError as exc:
        sys.exit(f"missing dependency for --local: {exc}")

    tokenizer = AutoTokenizer.from_pretrained(base_model)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    kwargs = {"device_map": {"": 0}, "attn_implementation": "sdpa",
              "torch_dtype": torch.float16}
    if four_bit:
        kwargs["quantization_config"] = BitsAndBytesConfig(
            load_in_4bit=True, bnb_4bit_quant_type="nf4",
            bnb_4bit_use_double_quant=True, bnb_4bit_compute_dtype=torch.float16)

    model = AutoModelForCausalLM.from_pretrained(base_model, **kwargs)
    if adapter:
        # Imported here, not at module scope, so the base side of the
        # comparison runs on a machine with no peft installed.
        from peft import PeftModel
        model = PeftModel.from_pretrained(model, adapter)
    model.eval()

    def complete(messages: list[dict]) -> str:
        try:
            text = tokenizer.apply_chat_template(
                messages, tokenize=False, add_generation_prompt=True,
                enable_thinking=False)
        except TypeError:
            text = tokenizer.apply_chat_template(
                messages, tokenize=False, add_generation_prompt=True)
        ids = tokenizer(text, return_tensors="pt").to(model.device)
        with torch.no_grad():
            out = model.generate(**ids, max_new_tokens=max_new, do_sample=False,
                                 pad_token_id=tokenizer.pad_token_id)
        return tokenizer.decode(out[0][ids["input_ids"].shape[1]:],
                                skip_special_tokens=True)

    return complete


def run_side(complete, label: str, model: str,
             examples: list[dict], metas: list[dict], limit: int,
             keep_samples: int = 6) -> dict:
    print(f"\n  {label}: {model}")
    agg = {"citation_valid": [], "grounded_format": [],
           "asked_when_thin": [], "decision_match": []}
    # Rates say what happened; they never say why, and a 0.00 is exactly where
    # the why matters most. persona-v1 scored asked_when_thin 0/28 and the
    # mechanism had to be inferred from a *second* metric — grounded_format at
    # 1.00 — because nothing here kept a single generation. Failures are kept in
    # preference to passes for the same reason: a run that looks right needs no
    # explaining.
    samples: list[dict] = []
    n = min(limit, len(examples)) if limit else len(examples)
    for i in range(n):
        prompt = [m for m in examples[i]["messages"] if m["role"] != "assistant"]
        text = complete(prompt)
        s = score_one(text, metas[i])
        for k, v in s.items():
            agg[k].append(v)
        if keep_samples and (any(v is False for v in s.values())
                             or len(samples) < 2):
            if len(samples) < keep_samples:
                samples.append({"row": i, "mode": metas[i]["mode"],
                                "expected_label": metas[i].get("label"),
                                "n_sources": metas[i]["n_sources"],
                                "scores": s, "answer": text})
        if (i + 1) % 10 == 0:
            print(f"    {i + 1}/{n}")

    def rate(k: str) -> float | None:
        xs = agg[k]
        return sum(1 for x in xs if x) / len(xs) if xs else None

    out = {k: rate(k) for k in agg}
    out["n"] = n
    out["samples"] = samples
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
    # No default, and deliberately so. It was gemma-2-2b-it-q4f16_1-MLC, from
    # when that was the only build there was; after the move to Qwen3-4B, taking
    # it measured persona-v1 against a *different base model* and handed the
    # whole difference to the adapter. Every number this file prints is a delta,
    # so a wrong before side does not degrade the measurement, it inverts what
    # it means — and it does that silently, which is how the Gemma-era defaults
    # survived a base migration in the first place.
    #
    # There is no id that is right for both lines, so the caller names it.
    ap.add_argument("--before", default="",
                    help="the untuned build to compare against, e.g. "
                         "qwen3-4b-instruct-q4f16_1-MLC; must share the "
                         "adapter's base or the delta is meaningless")
    # Not required: --local names the tuned side with --adapter instead, and a
    # required flag there would have to be given a value nothing reads.
    ap.add_argument("--after", default="", help="the tuned model id")
    ap.add_argument("--eval", default="data/persona_eval.jsonl")
    ap.add_argument("--meta", default="data/persona_eval_meta.jsonl")
    ap.add_argument("--limit", type=int, default=40,
                    help="examples per side (0 = all); each is one GPU call")
    # The Kaggle path. --before/--after name MLC builds served over a tunnel,
    # which is the right measurement and the one that decides a ship — but it
    # cannot run in the session that produced the adapter, and waiting for a
    # merge and an MLC compile to find out whether training worked is how the
    # Flutter line spent two runs.
    ap.add_argument("--local", action="store_true",
                    help="load weights in-process instead of calling a host; "
                         "for measuring inside the training session")
    ap.add_argument("--local-base-model", default="Qwen/Qwen3-4B-Instruct-2507")
    ap.add_argument("--adapter", default="",
                    help="--local: adapter dir for the after side; the before "
                         "side is always the bare base")
    ap.add_argument("--four-bit", action="store_true",
                    help="--local: load in 4-bit NF4 rather than fp16")
    # Split the two sides across two invocations. The pre-training gate and the
    # "before" column are the same measurement, and running the base twice to
    # produce both costs a model load plus a full generation pass — real money
    # against a session budget, and two numbers that can disagree for no reason
    # anyone can name. rubric_eval.py carries the same pair of flags.
    # Replace the system turn without regenerating the set. The rows carry the
    # prompt the backend served when they were built, which is what makes them a
    # faithful training distribution — but it also means the only way to ask
    # "would a better instruction fix this" used to be a full regeneration, and
    # a regenerated set is not the same set. Overriding here holds the evidence,
    # the ordering and the ground truth fixed and changes exactly one thing.
    #
    # It is a measurement tool, not a deployment path: a prompt that wins here
    # has to be landed in the backend, because that is where inference reads it
    # from. Winning on a file nothing sends is the same failure as training
    # against one.
    ap.add_argument("--system-prompt-file", default="",
                    help="replace every row's system turn with this file's "
                         "contents; for comparing prompts on fixed evidence")
    ap.add_argument("--base-only", action="store_true",
                    help="measure the base and stop; writes --out for --baseline")
    ap.add_argument("--baseline", default="",
                    help="a --base-only result to use as the before side "
                         "instead of measuring the base again")
    ap.add_argument("--max-new-tokens", type=int, default=512)
    ap.add_argument("--out", default="", help="write the two sides as JSON")
    args = ap.parse_args()

    if not args.local and not args.base_url:
        sys.exit("an inference host is required: --base-url or LLM_BASE_URL "
                 "(or --local to run the weights here)")
    if not args.local and not args.after and not args.base_only:
        sys.exit("--after names the tuned model id to measure")
    if not args.local and not args.before:
        sys.exit("--before names the untuned build to compare against; there is "
                 "no default because the only wrong answer is a build on a "
                 "different base, and that one reads as an adapter result")
    if args.local and not args.adapter and not args.base_only:
        sys.exit("--local needs --adapter; without it both sides are the base "
                 "and the delta is noise by construction "
                 "(--base-only if that is what you meant)")

    examples = load_jsonl(args.eval)
    metas = load_jsonl(args.meta)
    if len(examples) != len(metas):
        sys.exit(f"eval and meta lengths differ ({len(examples)} vs {len(metas)}); "
                 "regenerate both with build_persona_dataset.py")

    prompt_label = "(as generated)"
    if args.system_prompt_file:
        with open(args.system_prompt_file, encoding="utf-8") as fh:
            override = fh.read().strip()
        if not override:
            sys.exit(f"{args.system_prompt_file} is empty")
        swapped = 0
        for row in examples:
            for m in row["messages"]:
                if m["role"] == "system":
                    m["content"] = override
                    swapped += 1
        # Every row has exactly one system turn; if that stops being true the
        # comparison is no longer one-variable and should fail rather than skew.
        if swapped != len(examples):
            sys.exit(f"expected one system turn per row, replaced {swapped} "
                     f"across {len(examples)} rows")
        prompt_label = os.path.basename(args.system_prompt_file)
        print(f"system prompt overridden from {args.system_prompt_file} "
              f"({len(override)} chars, {swapped} rows)")

    if args.local:
        before_name = f"{args.local_base_model} (base)"
        if args.baseline:
            saved = json.load(open(args.baseline, encoding="utf-8"))
            before = saved["before"]
            before_name = saved.get("before_model", before_name)
            if saved.get("limit") != args.limit:
                # Not fatal, but the delta is then between two different sample
                # sizes and the note has to travel with the number.
                print(f"\nNOTE: baseline measured {saved.get('limit')} rows, "
                      f"this run measures {args.limit}.")
            print(f"\n  before (kayittan): {before_name}")
        else:
            # Sequential, not both-at-once: a 4B base plus an adapted copy does
            # not fit beside itself on a 16 GB T4, and the failure would land
            # after the first side had already been paid for.
            before = run_side(
                local_completer(args.local_base_model, "", args.four_bit,
                                args.max_new_tokens),
                "before", before_name, examples, metas, args.limit)

        if not args.base_only:
            import gc
            gc.collect()
            try:
                import torch
                torch.cuda.empty_cache()
            except ImportError:
                pass
            after_name = args.adapter
            after = run_side(
                local_completer(args.local_base_model, args.adapter,
                                args.four_bit, args.max_new_tokens),
                "after", after_name, examples, metas, args.limit)
    else:
        before_name, after_name = args.before, args.after
        http = lambda model: (
            lambda msgs: chat(args.base_url, args.api_key, model, msgs))
        before = run_side(http(args.before), "before", args.before,
                          examples, metas, args.limit)
        if not args.base_only:
            after = run_side(http(args.after), "after", args.after,
                             examples, metas, args.limit)

    # One side, either transport. This used to live inside the --local branch,
    # which quietly made it a local-only flag: over a tunnel the run measured
    # both sides regardless and --after was mandatory. That is wrong for the
    # measurement it was added for — a prompt comparison is two *separate*
    # one-sided runs on the same build, and forcing a second side doubles a
    # 100-row pass into 200 host calls and prints a delta of a model against
    # itself. The tunnel is also the only transport that can measure the
    # quantised build the product actually serves, so it is the last one that
    # should be locked out of a single-sided run.
    if args.base_only:
        out = args.out or "out/persona_base.json"
        os.makedirs(os.path.dirname(out) or ".", exist_ok=True)
        with open(out, "w", encoding="utf-8") as fh:
            json.dump({"before": before, "before_model": before_name,
                       "system_prompt": prompt_label,
                       "local": args.local, "limit": args.limit}, fh, indent=2)
        print(f"\nwrote {out}  (base only — no adapter measured)")
        return

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

    if args.out:
        # The log is not the artefact. A Kaggle kernel's stdout is readable for
        # as long as the version exists and is unparseable the moment anyone
        # wants to put two runs beside each other; the JSON is what a curve is
        # built from.
        os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
        with open(args.out, "w", encoding="utf-8") as fh:
            json.dump({"before": before, "after": after,
                       "before_model": before_name, "after_model": after_name,
                       "system_prompt": prompt_label,
                       "local": args.local, "limit": args.limit}, fh, indent=2)
        print(f"\nwrote {args.out}")

    # Both of these disqualify a build, and for a while only the first one did.
    # persona-v1's first run is why: citation_valid held at 1.00, so nothing was
    # printed, while asked_when_thin went 3/7 -> 0/7 — the adapter had stopped
    # asking and started answering every thin case with a verdict. Three metrics
    # improved and the run looked like a win in the table above.
    #
    # A confident verdict on absent evidence is the failure this product exists
    # to avoid, so losing that behaviour is not a trade the other three can pay
    # for. Refuse on either.
    blockers = []
    for key, why in (("citation_valid", "grounding"),
                     ("asked_when_thin", "asking instead of guessing")):
        b, a = before.get(key), after.get(key)
        if b is not None and a is not None and a < b:
            blockers.append(f"{key} {b:.2f} -> {a:.2f} ({why} got WORSE)")

    if blockers:
        print("\n" + "!" * 64)
        for line in blockers:
            print(f"  {line}")
        print("  Do not ship this adapter.")
        print("!" * 64)

    # A rate over a handful of rows is a direction, not a magnitude, and
    # asked_when_thin is the one that gets a thin denominator: it is scored only
    # on CLARIFY rows, which are a minority of the set by construction. Saying so
    # here costs a line and stops the number being quoted as if it were solid.
    clarify_n = sum(1 for m in metas[:len(examples)][:args.limit or len(metas)]
                    if m.get("mode") == "clarify")
    if clarify_n < 20:
        print(f"\nNOTE: asked_when_thin rests on {clarify_n} clarify row(s). "
              f"Raise --limit before treating its value as a rate.")


if __name__ == "__main__":
    main()
