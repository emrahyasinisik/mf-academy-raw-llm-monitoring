#!/usr/bin/env python3
"""Fail in the first ten seconds of a Colab session, not the fortieth minute.

Two things about a free-tier VM are not knowable in advance: which card it
hands you, and how old its preinstalled libraries are. Left unchecked, both
failures arrive after the 8 GB model download wearing other masks — an
"unknown architecture", or a cost curve that quietly belongs to a different
GPU. This runs before anything is downloaded.

    colab exec -s pilot -f colab/preflight.py --timeout 120
"""

from __future__ import annotations

import json
import os
import re
import sys

FLOORS = {"transformers": "4.51", "peft": "0.11",
          "bitsandbytes": "0.43", "accelerate": "0.30"}

OUT = "/content/out/preflight.json"


def version_at_least(found: str, floor: str) -> bool:
    """Numeric, component-wise comparison.

    Not a string compare: "4.9" sorts above "4.51" and that is exactly the
    stale transformers this gate exists to catch.
    """
    def parts(v: str) -> list[int]:
        return [int(p) for p in re.findall(r"\d+", v)]

    a, b = parts(found), parts(floor)
    a += [0] * (len(b) - len(a))
    b += [0] * (len(a) - len(b))
    return a >= b


def check_capability(cap: tuple[int, int], allow_any: bool) -> tuple[bool, str]:
    """The pilot's cost arithmetic is a T4's. Anything else is a different run."""
    sm = f"sm_{cap[0]}{cap[1]}"
    if cap == (7, 5):
        return True, f"{sm} — T4 as designed"
    if allow_any:
        return True, (f"{sm} is NOT a T4; PILOT_ALLOW_NON_T4 is set, so the run "
                      f"proceeds. Every projection it produces belongs to this "
                      f"card and has to be labelled as such.")
    return False, (f"{sm} is not sm_75. The measured cost, the fp16/bf16 choice "
                   f"and the whole projection assume a T4. Release this VM and "
                   f"ask again, or set PILOT_ALLOW_NON_T4=1 to proceed knowing "
                   f"the numbers describe a different card.")


def main() -> int:
    import torch

    if not torch.cuda.is_available():
        print("no CUDA device — the session was created without --gpu T4")
        return 1

    cap = torch.cuda.get_device_capability(0)
    name = torch.cuda.get_device_name(0)
    total_gb = torch.cuda.get_device_properties(0).total_memory / 1024**3
    count = torch.cuda.device_count()
    print(f"device: {name}  sm_{cap[0]}{cap[1]}  {total_gb:.1f} GB  "
          f"(device_count {count})")

    ok, msg = check_capability(cap, bool(os.environ.get("PILOT_ALLOW_NON_T4")))
    print(msg)

    versions: dict[str, str] = {}
    stale: list[str] = []
    for mod, floor in FLOORS.items():
        try:
            versions[mod] = __import__(mod).__version__
        except Exception as exc:  # noqa: BLE001 - report it, do not raise
            versions[mod] = f"<missing: {exc}>"
            stale.append(f"{mod} not importable")
            continue
        if not version_at_least(versions[mod], floor):
            stale.append(f"{mod} {versions[mod]} < {floor}")
    print("  " + "  ".join(f"{k} {v}" for k, v in versions.items()))

    for s in stale:
        print(f"STALE: {s} — run `colab install -r colab/requirements-colab.txt`")

    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as fh:
        json.dump({"device": name, "sm": f"{cap[0]}{cap[1]}",
                   "total_gb": round(total_gb, 1), "device_count": count,
                   "versions": versions, "stale": stale, "capability_ok": ok},
                  fh, indent=2)

    return 0 if (ok and not stale) else 1


if __name__ == "__main__":
    sys.exit(main())
