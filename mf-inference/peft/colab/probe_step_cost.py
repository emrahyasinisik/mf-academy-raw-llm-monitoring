#!/usr/bin/env python3
"""Measure what one row costs on this VM's card. Trains nothing.

The sibling of kaggle/probe/rubric-probe.ipynb, which asked whether the device
count set the step cost and answered no (26.0 -> 21.2 s/row across the two
regimes). The question here is the next suspect on that list: bitsandbytes'
NF4 dequant, which runs on every matmul and is the plausible reason 28-35
s/row sits 6-8x above what a T4's FLOPs explain.

Two regimes, same script, same rows, same everything else:

    4bit   what the cancelled Kaggle run did
    fp16   --no-4bit; ~8 GB of weights on a 16 GB card, so it may OOM,
           and an OOM is a result — it closes the branch honestly

train_qlora_qwen.py runs as a subprocess rather than an import, for the same
reason the Kaggle probe did it: the regimes differ in how CUDA initialises,
which one process cannot do twice. Running the real training script also
matters — a hand-written loop would not measure the code that will run.

    colab exec -s pilot -f colab/probe_step_cost.py --timeout 3000
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import threading
import time

PEFT = os.environ.get("PEFT_DIR", "/content/peft")

# `colab exec -f` does not run this as a script. It sends the text to the
# kernel and executes it as a cell, where __file__ does not exist — so a
# sys.path derived from it raises NameError before a single row is measured.
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__))
                if "__file__" in globals() else os.path.join(PEFT, "colab"))
import pilot_math  # noqa: E402
PROBE_ROWS = 8
EVAL_ROWS = 4
# What the full run is: the merged 1600-row set, three epochs.
FULL_ROWS, FULL_EPOCHS = 1600, 3.0
SESSION_HOURS = 3.0  # one free-tier slice, per the design's quota note

OUT = "/content/out/probe.json"


def measured_s_per_row(metrics: dict, rows: int = 0) -> float:
    """Cost of one row from the Trainer's own clock.

    train_runtime covers the training loop and nothing else — not the model
    download, not tokenisation, not the closing eval. That distinction is the
    whole measurement. The first version of this probe subtracted a guessed
    240 s load from wall time and priced two runs of identical work at 20.6
    and 10.3 s/row; the difference was an 8 GB download present in one and
    cached in the other, so the guess, not the work, was what varied. The
    Trainer read 232.4 s and 235.7 s across those same two runs.
    """
    runtime = metrics.get("train", {}).get("train_runtime")
    if not runtime:
        raise ValueError("train_metrics.json has no train.train_runtime — "
                         "the run did not reach the end of training")
    n = rows or metrics.get("row_passes") or 0
    if n <= 0:
        raise ValueError("no row count to divide by")
    return runtime / n


def parse_smi_mib(text: str | None) -> int:
    """Largest MiB figure in an nvidia-smi memory query; 0 if unreadable.

    Never raises: this runs on a sampler thread, and losing the thread would
    lose the wall-clock measurement the probe actually exists for.
    """
    nums = [int(n) for n in re.findall(r"\d+", text or "")]
    return max(nums) if nums else 0


class PeakMemory:
    """Poll nvidia-smi while a child trains.

    torch.cuda.max_memory_allocated lives in the child's process and dies with
    it, and it would only report torch's own allocator anyway — not the CUDA
    context or bitsandbytes' buffers, which are exactly what is in question
    when asking whether the fp16 arm fits in 16 GB.
    """

    def __init__(self, interval: float = 2.0):
        self.interval, self.peak = interval, 0
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._run, daemon=True)

    def _run(self) -> None:
        cmd = ["nvidia-smi", "--query-gpu=memory.used",
               "--format=csv,noheader,nounits"]
        while not self._stop.is_set():
            try:
                out = subprocess.run(cmd, capture_output=True, text=True,
                                     timeout=10).stdout
                self.peak = max(self.peak, parse_smi_mib(out))
            except Exception:  # noqa: BLE001 - a lost sample is not a failure
                pass
            self._stop.wait(self.interval)

    def __enter__(self) -> "PeakMemory":
        self._thread.start()
        return self

    def __exit__(self, *exc: object) -> None:
        self._stop.set()
        self._thread.join(timeout=5)


def sample_rows() -> None:
    """8 train / 4 eval rows at a fixed stride.

    Stride, not head: sequence length drives the cost being measured, and the
    front of the file is not a random sample of its length distribution.
    """
    for src, dst, n in (("data/pilot/rubric_train.jsonl",
                         "data/probe_train.jsonl", PROBE_ROWS),
                        ("data/pilot/rubric_eval.jsonl",
                         "data/probe_eval.jsonl", EVAL_ROWS)):
        with open(os.path.join(PEFT, src), encoding="utf-8") as fh:
            rows = [line for line in fh if line.strip()]
        stride = max(1, len(rows) // n)
        picked = [rows[i * stride] for i in range(min(n, len(rows)))]
        with open(os.path.join(PEFT, dst), "w", encoding="utf-8") as fh:
            fh.writelines(picked)
        print(f"{dst}: {len(picked)} of {len(rows)} rows", flush=True)


def run_regime(label: str, extra_args: list[str]) -> dict:
    # grad-accum 1 so one optimizer step is one row: the atomic cost, without
    # the 16x amplification that made the cancelled run's step read 910 s.
    args = [sys.executable, "train_qlora_qwen.py",
            "--train", "data/probe_train.jsonl",
            "--eval", "data/probe_eval.jsonl",
            "--max-seq-len", "2560",
            "--epochs", "1", "--grad-accum", "1",
            "--out-dir", f"out/probe_{label}"] + extra_args

    env = dict(os.environ, PYTORCH_CUDA_ALLOC_CONF="expandable_segments:True")
    print(f"\n===== regime {label} =====", flush=True)
    t0 = time.time()
    with PeakMemory() as mem:
        proc = subprocess.run(args, cwd=PEFT, env=env,
                              capture_output=True, text=True)
    wall = time.time() - t0

    tail = (proc.stdout or "")[-1500:] + (proc.stderr or "")[-1500:]
    print(tail, flush=True)

    result = {"wall_s": round(wall, 1), "s_per_row": 0.0,
              "peak_mib": mem.peak, "returncode": proc.returncode,
              "tail": tail[-1500:]}

    if proc.returncode != 0:
        print(f"{label}: FAILED (exit {proc.returncode}) — no projection. "
              f"For the fp16 arm, out of memory closes the branch and is worth "
              f"as much as a number; anything else is a bug to fix and rerun.",
              flush=True)
        return result

    # Measured, not inferred: read the Trainer's clock out of the run's own
    # metrics file rather than timing the subprocess and guessing at overhead.
    metrics_path = os.path.join(PEFT, "out", f"probe_{label}", "train_metrics.json")
    try:
        with open(metrics_path, encoding="utf-8") as fh:
            per_row = measured_s_per_row(json.load(fh), rows=PROBE_ROWS)
    except (OSError, ValueError) as exc:
        print(f"{label}: exit 0 but no usable train_runtime ({exc})", flush=True)
        result["returncode"] = 1
        return result
    result["s_per_row"] = round(per_row, 1)

    hours = pilot_math.project_full_run_hours(per_row, FULL_ROWS, FULL_EPOCHS)
    sessions = pilot_math.sessions_needed(hours, SESSION_HOURS)
    print(f"{label}: wall {wall / 60:.1f} min   {per_row:.1f} s/row   "
          f"peak {mem.peak} MiB", flush=True)
    print(f"  full run ({FULL_ROWS} rows x {FULL_EPOCHS:g}) -> {hours:.1f} h "
          f"= {sessions} free-tier sessions of {SESSION_HOURS:g} h", flush=True)
    result["full_run_hours"] = round(hours, 1)
    result["sessions"] = sessions
    return result


def main() -> int:
    sample_rows()
    regimes = {"4bit": run_regime("4bit", []),
               "fp16": run_regime("fp16", ["--no-4bit"])}

    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as fh:
        json.dump({"rows": PROBE_ROWS, "load_s": LOAD_GUESS_S,
                   "regimes": regimes}, fh, indent=2)
    print(f"\nwrote {OUT}")

    ok = [r for r in regimes.values() if r["returncode"] == 0]
    if not ok:
        print("both regimes failed — there is no measured cost, so there is no "
              "honest --max-steps to derive from one. Do not start training; "
              "read the tail fields in probe.json.")
        return 1

    print(f"\ncheapest regime: {min(r['s_per_row'] for r in ok):.1f} s/row")
    return 0


if __name__ == "__main__":
    # Only a failure exits: inside a kernel even sys.exit(0) prints "An
    # exception has occurred", which on the success path reads like the
    # failure it is not.
    _rc = main()
    if _rc:
        sys.exit(_rc)
