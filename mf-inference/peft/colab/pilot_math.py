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


def seconds_per_row(wall_s: float, rows: int, load_s: float) -> float:
    """Cost of one forward+backward pass, with fixed startup removed.

    load_s is the model download and quantised load, which a probe pays once
    and a real run also pays once. Leaving it in prices a short probe far
    above a long run.
    """
    if rows <= 0:
        raise ValueError("rows must be positive")
    return max(0.0, wall_s - load_s) / rows


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
