#!/usr/bin/env python3
"""Score a base model and an adapter on the held-out rubric set, offline.

Why this exists next to compare.py
----------------------------------
compare.py is the better measurement and stays the one that decides a ship: it
runs cases through the product's own /analysis/trial route, so the rubric, the
prompt, the quote neutralisation, the parser and the scoring are provably the
ones production uses. It cannot run on Kaggle — there is no backend there and no
inference host — and waiting until a build is merged, compiled and served to
learn whether training worked is how the Flutter line spent two runs.

So this is the *training-time* instrument: it loads the weights directly, runs
the held-out split, and scores against ground truth the generator wrote. It
answers "did this adapter learn the behaviour" in the same session that trained
it. It does not answer "does the product get better reports", and no number here
should be quoted as if it did.

What the base actually measures
-------------------------------
Everything below used to be written around a base that scored 0 on absent_rate
and 0 on schema_valid. That base was gemma-2-2b-it, measured through the product
route by baseline-trial.sh (see ../README.md). This line serves
Qwen/Qwen3-4B-Instruct-2507, and those two numbers were carried across without
being measured again. Measured (out/base_gate.json, 20 rows, 2026-07-29):

    absent_rate 0.89 · schema_valid 0.95 · completed 0.95
    present_score_mae 0.77 · hallucinated_quotes 0.013

So the gap this adapter was built to close is largely not there on this base,
and the two numbers that are still wrong are the rating itself and the
occasional invented quote. The gates at the bottom of this file are written
against that, not against the Gemma-era premise.

What is measured, in the order it matters
-----------------------------------------
present_score_mae  Mean absolute error of the rating, over findings whose ground
                truth has evidence. 0.77 on a 1-5 scale means the base agrees
                that the evidence is there and then rates it nearly a band off,
                which is the part of a report a reader acts on. This is what has
                to move.

absent_rate     Of the findings whose ground truth is "the case is silent on
                this criterion", the share the model marked evidence_found=false
                with a null score. On Gemma this was 0 and was the whole reason
                for the adapter; on this base it is already 0.89, so it is now a
                floor — an adapter that trades it away for a better MAE has
                bought the rating with the thing that makes a rejection
                defensible, and does not ship.

schema_valid    Share of answers that parsed as JSON with the expected keys and
                no repair. Also a floor now (0.95). Pure format discipline is
                the cheapest thing a LoRA learns, which is exactly why a gain
                here must not be mistaken for the result.

hallucinated_quotes  Share of cited evidence spans that do not appear verbatim
                in the case. The generator guarantees every ground-truth quote
                does, so anything above 0 is invention. The base sits at 1.3%:
                small, but it is the promise that citations are real.

Both sides run the same prompts in the same order with greedy decoding, so a
difference between them is the adapter and not the sampler.

Usage (inside the Kaggle notebook, after training):
    python3 rubric_eval.py --adapter out/rubric-v1 --limit 60
    python3 rubric_eval.py --base-only --limit 60      # before training
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys

try:
    import torch
    from transformers import AutoModelForCausalLM, AutoTokenizer, BitsAndBytesConfig
except ImportError as exc:
    sys.exit(f"missing dependency: {exc}\npip install -r requirements.txt")


def load_rows(path: str, limit: int) -> list[dict]:
    if not os.path.exists(path):
        sys.exit(f"{path} not found — run merge_rubric_sets.py first")
    rows = []
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            if line.strip():
                rows.append(json.loads(line))
            if limit and len(rows) >= limit:
                break
    return rows


def parse_answer(text: str) -> tuple[dict | None, bool]:
    """Return (parsed, clean).

    `clean` is whether it parsed with no repair at all — that is the number
    schema_valid reports. The repaired parse is still returned so the content
    metrics can be computed on a fenced-but-correct answer; scoring only the
    clean ones would flatter a model by dropping its messiest outputs.
    """
    try:
        return json.loads(text), True
    except json.JSONDecodeError:
        pass

    # The base model's habit: a ```json fence, sometimes with prose around it.
    fenced = re.search(r"```(?:json)?\s*(.+?)```", text, re.S)
    candidate = fenced.group(1) if fenced else text
    # Failing that, the outermost braces.
    if not fenced:
        start, end = candidate.find("{"), candidate.rfind("}")
        if start == -1 or end <= start:
            return None, False
        candidate = candidate[start:end + 1]
    try:
        return json.loads(candidate), False
    except json.JSONDecodeError:
        return None, False


def findings_by_key(payload: dict | None) -> dict[str, dict]:
    if not isinstance(payload, dict):
        return {}
    out = {}
    for f in payload.get("findings", []) or []:
        if isinstance(f, dict) and isinstance(f.get("key"), str):
            out[f["key"]] = f
    return out


def build_model(base: str, adapter: str | None, four_bit: bool):
    kwargs = {"device_map": {"": 0}, "attn_implementation": "sdpa",
              "torch_dtype": torch.float16}
    if four_bit:
        kwargs["quantization_config"] = BitsAndBytesConfig(
            load_in_4bit=True, bnb_4bit_quant_type="nf4",
            bnb_4bit_use_double_quant=True, bnb_4bit_compute_dtype=torch.float16)

    model = AutoModelForCausalLM.from_pretrained(base, **kwargs)
    if adapter:
        # Imported here rather than at module scope so --base-only works in an
        # environment without peft at all.
        from peft import PeftModel
        model = PeftModel.from_pretrained(model, adapter)
    model.eval()
    return model


def render(tokenizer, system: str, user: str) -> str:
    msgs = [{"role": "system", "content": system},
            {"role": "user", "content": user}]
    try:
        return tokenizer.apply_chat_template(
            msgs, tokenize=False, add_generation_prompt=True, enable_thinking=False)
    except TypeError:
        return tokenizer.apply_chat_template(
            msgs, tokenize=False, add_generation_prompt=True)


def evaluate(model, tokenizer, rows: list[dict], max_new: int) -> dict:
    clean = parsed = 0
    absent_hit = absent_total = 0
    abs_err: list[float] = []
    quotes_total = quotes_bad = 0

    for i, row in enumerate(rows, 1):
        msgs = row["messages"]
        system = next(m["content"] for m in msgs if m["role"] == "system")
        user = next(m["content"] for m in msgs if m["role"] == "user")
        truth = findings_by_key(json.loads(
            next(m["content"] for m in msgs if m["role"] == "assistant")))

        ids = tokenizer(render(tokenizer, system, user),
                        return_tensors="pt", add_special_tokens=False).to(model.device)
        with torch.no_grad():
            # Greedy. Both sides must see the same sampler or the comparison
            # measures temperature.
            out = model.generate(**ids, max_new_tokens=max_new, do_sample=False,
                                 pad_token_id=tokenizer.pad_token_id)
        text = tokenizer.decode(out[0][ids["input_ids"].shape[1]:],
                                skip_special_tokens=True)

        payload, was_clean = parse_answer(text)
        clean += was_clean
        if payload is None:
            print(f"  [{i}/{len(rows)}] unparseable", flush=True)
            continue
        parsed += 1
        got = findings_by_key(payload)

        for key, want in truth.items():
            have = got.get(key)
            if have is None:
                continue
            if not want["evidence_found"]:
                absent_total += 1
                # Both halves are required. A finding marked absent that still
                # carries a rating is the exact hedge this is measuring against.
                if not have.get("evidence_found") and have.get("score") is None:
                    absent_hit += 1
            else:
                if isinstance(have.get("score"), (int, float)):
                    abs_err.append(abs(float(have["score"]) - float(want["score"])))
                for span in have.get("evidence") or []:
                    quotes_total += 1
                    if isinstance(span, str) and span not in user:
                        quotes_bad += 1

        if i % 10 == 0:
            print(f"  [{i}/{len(rows)}] …", flush=True)

    n = len(rows)
    return {
        "rows": n,
        "schema_valid": clean / n if n else 0.0,
        "completed": parsed / n if n else 0.0,
        "absent_rate": absent_hit / absent_total if absent_total else 0.0,
        "absent_n": absent_total,
        "present_score_mae": sum(abs_err) / len(abs_err) if abs_err else None,
        "hallucinated_quotes": quotes_bad / quotes_total if quotes_total else 0.0,
        "quotes_n": quotes_total,
    }


def compare_pair(pair: dict, got_b: dict, got_v: dict) -> tuple | None:
    """Score one contrast pair from the two parsed answers.

    Pure, and separated from generation so it can be tested without a GPU — the
    arithmetic here is what the contrast number means, and it is the part that
    can be wrong in a way no amount of running would reveal.

    Returns (direction_ok, stable_hits, stable_total, all_stable), or None when
    the pair is unusable because the perturbed criterion is missing from either
    answer. Unusable is not failure: scoring an unparseable answer here would
    fold a format problem into a reasoning number, and schema_valid already
    reports format.
    """
    key = pair["criterion"]
    if key not in got_b or key not in got_v:
        return None

    before, after = got_b[key], got_v[key]
    if pair["expected"] == "absent":
        direction_ok = (not after.get("evidence_found")) and after.get("score") is None
    else:
        sb, sa = before.get("score"), after.get("score")
        direction_ok = bool(
            isinstance(sb, (int, float)) and isinstance(sa, (int, float))
            and ((sa < sb) if pair["expected"] == "down" else (sa > sb)))

    stable_hits = stable_total = 0
    for other in got_b:
        if other == key or other not in got_v:
            continue
        stable_total += 1
        same = (got_b[other].get("evidence_found") == got_v[other].get("evidence_found")
                and got_b[other].get("score") == got_v[other].get("score"))
        stable_hits += same
    return direction_ok, stable_hits, stable_total, stable_hits == stable_total


def evaluate_contrast(model, tokenizer, pairs: list[dict], max_new: int) -> dict:
    """Score a contrast set: does the answer move when, and only when, it should.

    Three numbers, and the third is the one the other two exist to produce.

      direction    Of the perturbed criteria, the share whose predicted finding
                   moved the way the gold label did — down when the evidence got
                   weaker, to evidence_found=false when the section was removed.

      stability    Of the *un*perturbed criteria, the share whose prediction did
                   not move at all. A model that rewrites the whole report when
                   one paragraph changes is not making per-criterion judgements,
                   and absent_rate cannot see that because it never compares two
                   answers to each other.

      consistency  Pairs that got both right. This is the contrast-set number:
                   it is what a held-out score would have been if the held-out
                   set could not be passed by recognising fragments.
    """
    dir_hit = dir_total = 0
    stable_hit = stable_total = 0
    both = usable = 0

    for i, pair in enumerate(pairs, 1):
        answers = []
        for side in ("base", "variant"):
            msgs = pair[side]["messages"]
            system = next(m["content"] for m in msgs if m["role"] == "system")
            user = next(m["content"] for m in msgs if m["role"] == "user")
            ids = tokenizer(render(tokenizer, system, user), return_tensors="pt",
                            add_special_tokens=False).to(model.device)
            with torch.no_grad():
                out = model.generate(**ids, max_new_tokens=max_new, do_sample=False,
                                     pad_token_id=tokenizer.pad_token_id)
            text = tokenizer.decode(out[0][ids["input_ids"].shape[1]:],
                                    skip_special_tokens=True)
            answers.append(findings_by_key(parse_answer(text)[0]))

        scored = compare_pair(pair, *answers)
        if scored is None:
            continue
        ok_dir, s_hits, s_total, ok_stable = scored

        usable += 1
        dir_total += 1
        dir_hit += ok_dir
        stable_hit += s_hits
        stable_total += s_total
        both += ok_dir and ok_stable
        if i % 5 == 0:
            print(f"  [{i}/{len(pairs)}] …", flush=True)

    return {
        "pairs": len(pairs),
        "usable": usable,
        "direction": dir_hit / dir_total if dir_total else 0.0,
        "stability": stable_hit / stable_total if stable_total else 0.0,
        "consistency": both / usable if usable else 0.0,
    }


def report_contrast(label: str, m: dict) -> None:
    print(f"\n{label} — contrast  ({m['usable']} of {m['pairs']} pairs parsed)")
    print(f"  direction            {m['direction']:.1%}")
    print(f"  stability            {m['stability']:.1%}")
    print(f"  consistency          {m['consistency']:.1%}")


def report(label: str, m: dict) -> None:
    mae = "—" if m["present_score_mae"] is None else f"{m['present_score_mae']:.2f}"
    print(f"\n{label}  ({m['rows']} rows)")
    print(f"  absent_rate          {m['absent_rate']:.1%}   (n={m['absent_n']})")
    print(f"  schema_valid         {m['schema_valid']:.1%}")
    print(f"  completed            {m['completed']:.1%}")
    print(f"  present_score_mae    {mae}")
    print(f"  hallucinated_quotes  {m['hallucinated_quotes']:.1%}   (n={m['quotes_n']})")


def main() -> None:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--data", default="data/rubric_eval.jsonl")
    ap.add_argument("--base-model", default="Qwen/Qwen3-4B-Instruct-2507")
    ap.add_argument("--adapter", default="out/rubric-v1")
    ap.add_argument("--base-only", action="store_true",
                    help="measure the base alone — run this BEFORE training")
    ap.add_argument("--baseline", default="",
                    help="a JSON file written by an earlier --base-only run; "
                         "reuses its numbers instead of measuring the base "
                         "again, which on a T4 is a quarter hour of the run")
    ap.add_argument("--limit", type=int, default=60,
                    help="held-out rows to score; 0 for all")
    ap.add_argument("--contrast", default="",
                    help="a contrast set from build_contrast_set.py; each pair "
                         "costs two generations, so it is priced separately "
                         "from --limit")
    ap.add_argument("--contrast-limit", type=int, default=30,
                    help="pairs to score; 0 for all")
    ap.add_argument("--max-new-tokens", type=int, default=900)
    ap.add_argument("--four-bit", action="store_true",
                    help="load in 4-bit; fp16 is the default because it is what "
                         "the server runs")
    ap.add_argument("--out", default="")
    args = ap.parse_args()

    rows = load_rows(args.data, args.limit)
    tokenizer = AutoTokenizer.from_pretrained(args.base_model)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    print(f"base: {args.base_model}   rows: {len(rows)}")

    model = None
    if args.baseline:
        if not os.path.exists(args.baseline):
            sys.exit(f"{args.baseline} not found — run with --base-only first")
        saved = json.load(open(args.baseline, encoding="utf-8"))
        before = saved["base"]
        # A baseline measured on a different number of rows is not a baseline.
        # Silently comparing 60 adapter rows against 200 base ones would produce
        # a delta that is mostly sampling.
        if before.get("rows") != len(rows):
            sys.exit(f"{args.baseline} measured {before.get('rows')} rows but this "
                     f"run has {len(rows)} — re-measure or match --limit")
        print(f"(baseline reused from {args.baseline})")
    else:
        model = build_model(args.base_model, None, args.four_bit)
        before = evaluate(model, tokenizer, rows, args.max_new_tokens)
    report("BASE", before)

    result = {"base": before, "base_model": args.base_model, "rows": len(rows)}

    pairs = load_rows(args.contrast, args.contrast_limit) if args.contrast else []
    if pairs and model is not None:
        before_c = evaluate_contrast(model, tokenizer, pairs, args.max_new_tokens)
        report_contrast("BASE", before_c)
        result["base_contrast"] = before_c
    elif pairs and args.baseline:
        # The baseline file may predate --contrast. Say so rather than silently
        # reporting an adapter number with nothing to compare it against.
        saved_c = json.load(open(args.baseline, encoding="utf-8")).get("base_contrast")
        if saved_c:
            report_contrast("BASE", saved_c)
            result["base_contrast"] = saved_c
        else:
            print("\n(no base contrast numbers in the baseline file — "
                  "the adapter's will have nothing to be compared against)")

    if not args.base_only:
        if not os.path.isdir(args.adapter):
            sys.exit(f"\n{args.adapter} not found — train first, or pass --base-only")
        if model is not None:
            del model
        torch.cuda.empty_cache()
        print(f"\nadapter: {args.adapter}")
        model = build_model(args.base_model, args.adapter, args.four_bit)
        after = evaluate(model, tokenizer, rows, args.max_new_tokens)
        report("ADAPTER", after)
        result["adapter"] = after
        result["adapter_path"] = args.adapter

        after_c = None
        if pairs:
            after_c = evaluate_contrast(model, tokenizer, pairs, args.max_new_tokens)
            report_contrast("ADAPTER", after_c)
            result["adapter_contrast"] = after_c

        print("\ndelta")
        mae_b, mae_a = before["present_score_mae"], after["present_score_mae"]
        if mae_b is not None and mae_a is not None:
            print(f"  {'present_score_mae':20} {mae_b:.2f} → {mae_a:.2f}"
                  f"   ({mae_a - mae_b:+.2f}, lower is better)")
        for k in ("absent_rate", "schema_valid", "completed",
                  "hallucinated_quotes"):
            print(f"  {k:20} {before[k]:.1%} → {after[k]:.1%}"
                  f"   ({after[k] - before[k]:+.1%})")
        base_c = result.get("base_contrast")
        if after_c and base_c:
            for k in ("direction", "stability", "consistency"):
                print(f"  {k:20} {base_c[k]:.1%} → {after_c[k]:.1%}"
                      f"   ({after_c[k] - base_c[k]:+.1%})")

        # The gates. present_score_mae leads because it is the one number this
        # base gets wrong (see the header): absent_rate led while the base was
        # gemma-2-2b-it and measured 0, and leaving it in that seat now would
        # pass any adapter that nudged 0.89 to 0.90 while the ratings stayed a
        # band off. absent_rate and schema_valid become floors — an adapter that
        # only learned to drop the ```json fence is a formatting patch being
        # presented as a capability.
        if mae_b is None or mae_a is None:
            print("\npresent_score_mae is missing on one side — this adapter "
                  "cannot be judged; do not ship it")
        elif mae_a >= mae_b:
            print("\npresent_score_mae did not improve — do not ship this adapter")
        elif after["absent_rate"] < before["absent_rate"] - 0.05:
            print("\nthe rating improved but absent-evidence behaviour "
                  "regressed — a report that rates well and hides what it "
                  "could not find is worse; do not ship this adapter")
        elif after["schema_valid"] < before["schema_valid"] - 0.05:
            print("\nschema_valid regressed — do not ship this adapter")
        elif after["hallucinated_quotes"] > before["hallucinated_quotes"]:
            print("\nquote invention got worse — do not ship this adapter")
        elif after_c and base_c and after_c["consistency"] <= base_c["consistency"]:
            # The held-out gain can be memorisation: every eval case is built
            # from the same fifty-one fragments the adapter trained on, so
            # recognising them is enough. The contrast pairs are the same cases
            # with one paragraph changed, and recognition does not survive that.
            # A better MAE with no rise here is the shape of a model that
            # learned the bank rather than the rule.
            print("\ncontrast consistency did not improve — the gain on the "
                  "held-out set may be fragment recognition rather than "
                  "judgement; do not ship on present_score_mae alone")

    if args.out:
        os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
        with open(args.out, "w", encoding="utf-8") as fh:
            json.dump(result, fh, indent=2, ensure_ascii=False)
        print(f"\nwrote {args.out}")


if __name__ == "__main__":
    main()
