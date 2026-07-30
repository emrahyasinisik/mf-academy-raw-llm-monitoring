"""Unit tests for the pilot's arithmetic.

Run from mf-inference/peft/:
    python3 -m unittest discover -s colab -p 'test_*.py' -v
"""

import unittest

import pilot_math


class TestSecondsPerRow(unittest.TestCase):
    def test_subtracts_the_load_share(self):
        # 8 rows in 20 min, of which 3 min was the model load.
        self.assertAlmostEqual(
            pilot_math.seconds_per_row(1200.0, 8, 180.0), 127.5)

    def test_never_negative(self):
        # A probe shorter than the load estimate is a broken measurement,
        # not a negative cost. Clamp rather than propagate a sign error into
        # a max_steps that would then be enormous.
        self.assertEqual(pilot_math.seconds_per_row(100.0, 8, 180.0), 0.0)

    def test_rejects_zero_rows(self):
        with self.assertRaises(ValueError):
            pilot_math.seconds_per_row(1200.0, 0, 180.0)


class TestComputeMaxSteps(unittest.TestCase):
    def test_the_design_arithmetic(self):
        # 2700 s of training at 30 s/row, effective batch 1x4 = 4 rows/step.
        self.assertEqual(
            pilot_math.compute_max_steps(2700.0, 30.0, 1, 4), 22)

    def test_floors_rather_than_rounds(self):
        # 2700 / (35 * 4) = 19.28 -> 19. Rounding up overruns the budget,
        # and the budget is the only thing the pilot has to hit.
        self.assertEqual(
            pilot_math.compute_max_steps(2700.0, 35.0, 1, 4), 19)

    def test_at_least_one_step(self):
        # An unaffordably slow card still gets one step: a run that trains
        # nothing proves nothing, and 0 would be read by Trainer as "no limit".
        self.assertEqual(
            pilot_math.compute_max_steps(60.0, 500.0, 1, 4), 1)

    def test_rejects_zero_cost(self):
        with self.assertRaises(ValueError):
            pilot_math.compute_max_steps(2700.0, 0.0, 1, 4)


class TestProjection(unittest.TestCase):
    def test_full_run_hours(self):
        # The number the pilot exists to produce: 1600 rows, 3 epochs.
        self.assertAlmostEqual(
            pilot_math.project_full_run_hours(30.0, 1600, 3.0), 40.0)

    def test_sessions_needed_rounds_up(self):
        # 40 hours in 3-hour slices is 14 sessions, not 13.33.
        self.assertEqual(pilot_math.sessions_needed(40.0, 3.0), 14)

    def test_exact_fit_does_not_add_a_session(self):
        self.assertEqual(pilot_math.sessions_needed(9.0, 3.0), 3)


if __name__ == "__main__":
    unittest.main()
