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
