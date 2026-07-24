#!/usr/bin/env python3
"""Measure what a fine-tune actually changed.

Runs the same case through the same endpoint twice — once per model — and
reports the difference on the numbers that decide whether an adapter ships.

The comparison goes through the product's own /analysis/trial route rather than
calling the inference host directly. That is deliberate: the rubric, the prompt,
the quote neutralisation, the parser and the scoring must all be identical
between the two runs, and the only way to guarantee that is to use the same code
path. A bespoke evaluation harness measures the harness.

Four numbers, in the order they matter
--------------------------------------
absent_rate     Share of findings correctly marked evidence_found=false. The
                base model measured 0% here: asked about a criterion the case
                never mentions, it writes "the text contains no information
                about competitors" and rates it 3 out of 5 regardless. Every
                report therefore contains fabricated ratings and coverage always
                claims 1.0, which makes the product's central promise — that a
                rejection can be defended — untrue. Nothing else on this list
                matters if this does not move.

schema_valid    Share of answers that parsed with no repair. The base model
                measured 0% because it fences every answer in ```json. The
                content was fine; this is pure format discipline.

completed       Share of runs that produced a scoreable report at all. The base
                model managed 4 of 5.

stddev          Spread of the overall score across identical inputs. The base
                model measured 0 on the runs that worked, so there is nothing to
                improve — only something to avoid breaking.

Usage:
    compare.py --before gemma-2-2b-it-q4f16_1-MLC --after tuned-v1
"""

from __future__ import annotations

import argparse
import json
import os
import statistics
import sys
import urllib.error
import urllib.request


def call(base_url: str, token: str, path: str, payload: dict | None = None,
         timeout: int = 1800) -> dict:
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(
        f"{base_url}{path}", data=data,
        headers={"Authorization": f"Bearer {token}",
                 "Content-Type": "application/json"},
        method="POST" if data else "GET",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        sys.exit(f"{path} failed ({e.code}):\n"
                 f"{e.read().decode(errors='replace')[:400]}")
    except urllib.error.URLError as e:
        sys.exit(f"could not reach {base_url}: {e.reason}")


def absent_rate(base_url: str, token: str, group: str) -> tuple[float, int, int]:
    """Share of findings that used the evidence_found=false path.

    Read from the stored assessments rather than the trial summary, because the
    summary reports coverage — which is the *weighted* complement of this and
    cannot distinguish "the model marked three criteria absent" from "one heavy
    criterion was absent".
    """
    detail = call(base_url, token, f"/analysis/trials/{group}")
    absent = total = 0
    for a in detail.get("assessments", []):
        for f in a.get("findings", []):
            total += 1
            if not f.get("evidence_found"):
                absent += 1
    return (absent / total if total else 0.0), absent, total


def run_side(base_url: str, token: str, label: str, model: str,
             domain: str, subject: str, title: str, trials: int) -> dict:
    print(f"\n  {label}: {model}  ({trials} trials, sequential — this takes a while)")
    payload = {"domain": domain, "subject_title": title,
               "subject": subject, "trials": trials}
    if model:
        payload["model"] = model

    res = call(base_url, token, "/analysis/trial", payload)
    rate, absent, total = absent_rate(base_url, token, res["trial_group"])

    out = {
        "model": model or "(deployment default)",
        "trial_group": res["trial_group"],
        "completed": res["trials"],
        "requested": trials,
        "schema_valid_rate": res["schema_valid_rate"],
        "mean_score": res.get("mean_score"),
        "stddev_score": res.get("stddev_score"),
        "mean_coverage": res["mean_coverage"],
        "mean_latency_ms": res["mean_latency_ms"],
        "absent_rate": rate,
        "absent_findings": absent,
        "total_findings": total,
    }
    print(f"    schema_valid {out['schema_valid_rate']:.2f}   "
          f"absent {rate:.2f}   completed {out['completed']}/{trials}")
    return out


def fmt(v) -> str:
    if v is None:
        return "n/a"
    return f"{v:.2f}" if isinstance(v, float) else str(v)


def delta(before, after, higher_is_better=True) -> str:
    """Format a change, or say why it cannot be computed."""
    if before is None or after is None:
        return "—"
    d = after - before
    if abs(d) < 1e-9:
        return "  0.00"
    arrow = "▲" if (d > 0) == higher_is_better else "▼"
    return f"{arrow} {d:+.2f}"


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", "http://localhost:8090"))
    ap.add_argument("--token", default=os.environ.get("TOKEN", ""))
    ap.add_argument("--domain", default="startup-investability")
    ap.add_argument("--before", default="gemma-2-2b-it-q4f16_1-MLC",
                    help="model id serving the untuned base")
    ap.add_argument("--after", required=True,
                    help="model id serving the merged adapter build")
    ap.add_argument("--trials", type=int, default=5)
    ap.add_argument("--case-file",
                    default=os.path.join(os.path.dirname(__file__),
                                         "..", "..", "mf-backend", "testdata", "ornek-deck.txt"))
    ap.add_argument("--out", default="out/comparison.json")
    args = ap.parse_args()

    if not args.token:
        sys.exit("a token is required: --token or TOKEN=...")
    if not os.path.exists(args.case_file):
        sys.exit(f"case file not found: {args.case_file}")

    with open(args.case_file, encoding="utf-8") as fh:
        subject = fh.read()
    title = os.path.basename(args.case_file)

    print(f"case: {args.case_file}  rubric: {args.domain}")
    before = run_side(args.base_url, args.token, "before", args.before,
                      args.domain, subject, title, args.trials)
    after = run_side(args.base_url, args.token, "after ", args.after,
                     args.domain, subject, title, args.trials)

    rows = [
        ("absent_rate      ", before["absent_rate"], after["absent_rate"], True),
        ("schema_valid_rate", before["schema_valid_rate"], after["schema_valid_rate"], True),
        ("completed        ", before["completed"] / args.trials, after["completed"] / args.trials, True),
        ("mean_coverage    ", before["mean_coverage"], after["mean_coverage"], True),
        ("stddev_score     ", before["stddev_score"], after["stddev_score"], False),
        ("mean_score       ", before["mean_score"], after["mean_score"], True),
    ]

    print("\n" + "=" * 62)
    print(f"{'metric':<18} {'before':>10} {'after':>10} {'change':>12}")
    print("-" * 62)
    for label, b, a, higher in rows:
        print(f"{label} {fmt(b):>10} {fmt(a):>10} {delta(b, a, higher):>12}")
    print("=" * 62)

    lat = f"{before['mean_latency_ms']:.0f} → {after['mean_latency_ms']:.0f} ms"
    print(f"latency: {lat}")

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    with open(args.out, "w", encoding="utf-8") as fh:
        json.dump({"before": before, "after": after,
                   "case_file": os.path.abspath(args.case_file),
                   "domain": args.domain, "trials": args.trials}, fh,
                  indent=2, ensure_ascii=False)
    print(f"written to {args.out}")

    # The verdict is stated rather than left to the reader, because the
    # temptation after a training run is to accept it. absent_rate is the gate:
    # a build that improves formatting while still inventing ratings for
    # criteria the case never addressed is worse than no build, since it
    # produces confident reports that cannot be defended.
    print()
    if after["absent_rate"] <= before["absent_rate"] + 0.05:
        print("VERDICT: do not ship. The absent-evidence behaviour did not move, "
              "which is the one thing this adapter exists to fix.")
    elif after["completed"] < before["completed"]:
        print("VERDICT: do not ship. Fewer runs produced a report than before; "
              "the adapter traded reliability for format.")
    else:
        print("VERDICT: candidate. Confirm on a case the training generator "
              "never saw before activating it.")


if __name__ == "__main__":
    main()
