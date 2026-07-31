#!/usr/bin/env python3
"""The pilot's arithmetic, kept free of torch so it can be tested on the Mac.

Every number the Colab pilot decides with passes through here: the measured
cost of a row, the step limit derived from it, and the projection of the full
run. They are separated from the scripts that use them because the machine
that can run the tests is not the machine that has a GPU, and an untested
max_steps is exactly the guess that already cost this project twelve hours and
a third of a weekly GPU quota.
"""

from __future__ import annotations

import math


# There was a seconds_per_row(wall_s, rows, load_s) here, subtracting a guessed
# model-load time from the subprocess wall clock. It priced two runs of the
# same eight rows at 20.6 and 10.3 s/row: the first paid an 8 GB download, the
# second found it cached, and both had the same 240 s taken off. It is deleted
# rather than corrected — the Trainer already reports train_runtime over the
# training loop alone, so probe_step_cost.measured_s_per_row reads that, and
# leaving the wall-clock version here would only invite its reuse.


def training_budget_s(lease_s: float, load_s: float, eval_s: float,
                      evals: int, reserve_s: float) -> float:
    """How much of a session lease the training loop may actually spend.

    The first pilot run passed a hardcoded 2700 s to compute_max_steps, took
    23 steps, and lost its adapter. The session log says why: created 12:50:13,
    terminated 13:50:21 — a 3608 s lease, of which train_runtime alone took
    2943 s. Nothing in the arithmetic knew about the rest of the hour.

    What the lease pays for besides stepping:
      * load_s    — model download and tokenisation before step 1 (~250 s
                    measured, and consistent with the probe's 240 s load).
      * eval_s x evals — one pass over the eval set, 361 s measured. Two of
                    them run: eval_strategy="epoch" fires one *inside*
                    train_runtime, and train_qlora_qwen.py calls
                    trainer.evaluate() again afterwards. Counting neither is
                    what pushed a 45-minute budget into a 60-minute lease.
      * reserve_s — the window left to pull the adapter. It is written before
                    the final eval, so it exists early; but /content dies with
                    the session, and last time it existed for five minutes and
                    was never fetched.

    Raises rather than returning a negative or derisory budget: renting a T4
    for an hour that buys no training is the one outcome worth refusing.
    """
    budget = lease_s - load_s - eval_s * evals - reserve_s
    if budget <= 0:
        raise ValueError(
            f"a {lease_s:.0f}s lease buys no training after {load_s:.0f}s load, "
            f"{evals}x{eval_s:.0f}s eval and {reserve_s:.0f}s reserve")
    return budget


def compute_max_steps(budget_s: float, s_per_row: float,
                      batch_size: int, grad_accum: int) -> int:
    """How many optimizer steps fit in budget_s at the measured cost.

    Floored, never rounded: the budget is a wall, and the pilot's whole job is
    to finish inside it. Never zero, because Trainer reads max_steps<=0 as
    "no limit" — the exact failure this function exists to prevent.
    """
    if s_per_row <= 0:
        raise ValueError("s_per_row must be positive — the probe failed")
    rows_per_step = batch_size * grad_accum
    return max(1, int(budget_s // (s_per_row * rows_per_step)))


def project_full_run_hours(s_per_row: float, rows: int, epochs: float) -> float:
    """Wall hours the full run would take at the measured cost."""
    return s_per_row * rows * epochs / 3600.0


def sessions_needed(hours: float, session_hours: float) -> int:
    """Free-tier sessions the full run would be chopped into.

    Rounded up: a run that needs 13.3 sessions needs 14, and the fraction is
    the one that has to survive a resume.
    """
    if session_hours <= 0:
        raise ValueError("session_hours must be positive")
    return max(1, math.ceil(hours / session_hours))
