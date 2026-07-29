#!/usr/bin/env python3
"""Build a contrast set: pairs of cases that differ in exactly one criterion.

Why the held-out set is not enough
----------------------------------
A held-out split answers 'did the model see this case before'. It does not
answer 'did the model learn the rule or a proxy for it', and on a generated set
those come apart easily: every case here is assembled from a bank of fifty-one
fragments, so a model can score well by recognising fragments rather than by
reading them. The training-time metrics cannot tell the difference, because both
behaviours produce the same answers on cases built the same way.

Gardner et al. (2020) named the fix — perturb an evaluated instance in a small
way that *changes the gold label*, and check the prediction changes with it.
Across ten NLP datasets, models that looked strong on the original test set lost
up to 25 points on the perturbed one. That gap is the part of the score that was
never capability.

The perturbations
-----------------
Both hold everything else fixed — the same company, the same section order, the
same fragments for every other criterion — so a change in the model's answer has
exactly one possible cause.

  quality  One criterion's fragment is swapped for another of the same criterion
           at a materially different score (a 5 becomes a 1 or 2). The section
           still exists and still discusses the criterion; only how well it does
           so changes. A model reading the evidence lowers that finding's score.
           A model recognising 'this case has a PAZAR section' does not.

  removal  One criterion's section is deleted outright. The gold finding flips to
           evidence_found=false with a null score. This is the behaviour the
           whole adapter exists to install, tested at its sharpest: the same case
           the model just rated, minus the paragraph it rated it from.

What makes a pair informative
-----------------------------
Only the perturbed criterion should move. A model that rewrites every finding
when one paragraph changes has not learned a per-criterion judgement, it has
learned to react to the case as a whole — and that failure is invisible to
absent_rate, which never compares two answers to each other.

Cases are drawn from the eval side of the split (see split_of), so nothing here
was trained on.

Usage:
    python3 build_contrast_set.py --domain startup-investability --n 40 \\
        --out data/contrast_investment.jsonl
"""

from __future__ import annotations

import argparse
import copy
import dataclasses
import json
import os
import random
import sys

from build_dataset import (
    DOMAIN_BANKS,
    EVAL_SHARE,
    RATIONALES_ABSENT,
    draw_spec,
    fetch_prompt,
    render_case,
    render_user_message,
    split_of,
)

# How far apart the two fragments of a `quality` pair must score. A 4 swapped for
# a 3 is a real difference the rubric cares about, but it is inside the noise a
# 5-point scale carries, and a model that does not move on it has not clearly
# failed. Two bands apart makes the expected direction unambiguous.
MIN_SCORE_GAP = 2


def perturb_quality(spec, criteria, bank, rng):
    """Swap one criterion's fragment for a much better or worse one.

    Returns (new_spec, key, old_score, new_score) or None when no criterion in
    this case has an alternative far enough away.
    """
    fragments = bank["fragments"]
    candidates = []
    for key, idx in spec.picked.items():
        cur = fragments[key][idx]["score"]
        for j, frag in enumerate(fragments[key]):
            if abs(frag["score"] - cur) >= MIN_SCORE_GAP:
                candidates.append((key, j, cur, frag["score"]))
    if not candidates:
        return None

    key, j, old, new = rng.choice(candidates)
    out = copy.deepcopy(spec)
    out.picked[key] = j
    # The quote count is redrawn against the new fragment, which may cite fewer
    # spans than the old one. Clamping rather than redrawing at random keeps the
    # pair otherwise identical.
    out.quotes_n[key] = min(out.quotes_n[key], len(fragments[key][j]["quotes"]))
    return out, key, old, new


def perturb_removal(spec, criteria, bank, rng):
    """Delete one criterion's section, so its gold finding becomes absent."""
    if len(spec.picked) <= bank["min_present"]:
        # Removing here would produce a case thinner than the generator ever
        # makes, which is a different distribution rather than a fair probe.
        return None

    key = rng.choice(sorted(spec.picked))
    old = bank["fragments"][key][spec.picked[key]]["score"]

    out = copy.deepcopy(spec)
    del out.picked[key]
    del out.quotes_n[key]
    out.order = [k for k in out.order if k != key]
    out.absent_rationale[key] = rng.choice(RATIONALES_ABSENT)
    return out, key, old, None


def main() -> None:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", "http://localhost:8090"))
    ap.add_argument("--token", default=os.environ.get("TOKEN", ""))
    ap.add_argument("--domain", default="startup-investability", choices=sorted(DOMAIN_BANKS))
    ap.add_argument("--n", type=int, default=40, help="pairs per perturbation kind")
    ap.add_argument("--seed", type=int, default=20260724,
                    help="must match the generator's, or the eval-side split "
                         "assignment will not be the same one")
    ap.add_argument("--out", default="data/contrast.jsonl")
    args = ap.parse_args()

    if not args.token:
        sys.exit("a token is required: --token or TOKEN=...")

    bank = DOMAIN_BANKS[args.domain]
    spec_prompt = fetch_prompt(args.base_url, args.token, args.domain)
    system_prompt = spec_prompt["system_prompt"]
    user_template = spec_prompt["user_prompt_example"]
    criteria = spec_prompt["criteria"]

    # Offset from the generator's stream so the contrast cases are not the same
    # eval rows already being scored — they only have to come from the same
    # held-out region, not to be the same draws.
    rng = random.Random(args.seed + 1)

    def row(title, subject, findings):
        return {
            "messages": [
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": render_user_message(user_template, title, subject)},
                {"role": "assistant", "content": json.dumps(
                    {"findings": findings}, ensure_ascii=False, separators=(",", ":"))},
            ]
        }

    pairs: list[dict] = []
    counts = {"quality": 0, "removal": 0}
    attempts = 0
    limit = args.n * 2 * 400

    while min(counts.values()) < args.n:
        attempts += 1
        if attempts > limit:
            sys.exit(f"gave up after {attempts} draws with {counts}; the rubric "
                     f"may be too small to perturb this many distinct cases")

        base_spec = draw_spec(rng, criteria, bank)
        b_title, b_body, b_findings, b_sig = render_case(criteria, bank, base_spec)
        if split_of(b_sig, args.seed, EVAL_SHARE) != "eval":
            continue

        kind = "quality" if counts["quality"] <= counts["removal"] else "removal"
        if counts[kind] >= args.n:
            kind = "removal" if kind == "quality" else "quality"

        got = (perturb_quality if kind == "quality" else perturb_removal)(
            base_spec, criteria, bank, rng)
        if got is None:
            continue
        var_spec, key, old_score, new_score = got

        v_title, v_body, v_findings, _ = render_case(criteria, bank, var_spec)

        pairs.append({
            "kind": kind,
            "criterion": key,
            "domain": args.domain,
            "base_score": old_score,
            "variant_score": new_score,
            # What the model's answer must do between the two. `down`/`up` are
            # directions on the perturbed criterion's score; `absent` means the
            # finding must flip to evidence_found=false.
            "expected": ("absent" if kind == "removal"
                         else ("down" if new_score < old_score else "up")),
            "base": row(b_title, b_body, b_findings),
            "variant": row(v_title, v_body, v_findings),
        })
        counts[kind] += 1

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    with open(args.out, "w", encoding="utf-8") as fh:
        for p in pairs:
            fh.write(json.dumps(p, ensure_ascii=False) + "\n")

    print(f"{args.out}: {len(pairs)} pairs ({counts['quality']} quality, "
          f"{counts['removal']} removal) from {attempts} draws")
    print(f"rubric: {spec_prompt['domain']}, {len(criteria)} criteria")

    # A pair whose two halves render identically would silently score as a
    # perfect result, so prove they differ before anyone measures against them.
    same = sum(1 for p in pairs
               if p["base"]["messages"][1]["content"] == p["variant"]["messages"][1]["content"])
    assert not same, f"{same} pair(s) have identical prompts"
    print("every pair differs in its user message")


if __name__ == "__main__":
    main()
