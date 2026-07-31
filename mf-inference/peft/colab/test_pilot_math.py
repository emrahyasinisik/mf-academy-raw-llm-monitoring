"""Unit tests for the pilot's arithmetic.

Run from mf-inference/peft/:
    python3 -m unittest discover -s colab -p 'test_*.py' -v
"""

import unittest

import pilot_math


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


class TestTrainingBudget(unittest.TestCase):
    """The lease is the wall, not the training loop.

    The first pilot run derived 23 steps from a hardcoded 2700 s and lost the
    adapter: the session was leased for 3608 s and the loop alone spent 2943 s
    of it, because neither eval was in the arithmetic.
    """

    def test_the_measured_run(self):
        # 3600 lease, 250 s load+tokenise, two 361 s evals, 180 s to pull.
        self.assertAlmostEqual(
            pilot_math.training_budget_s(3600.0, 250.0, 361.0, 2, 180.0),
            2448.0)

    def test_both_evals_are_charged(self):
        # eval_strategy="epoch" fires one inside train_runtime and
        # trainer.evaluate() runs another after. Counting one is the bug.
        one = pilot_math.training_budget_s(3600.0, 250.0, 361.0, 1, 180.0)
        two = pilot_math.training_budget_s(3600.0, 250.0, 361.0, 2, 180.0)
        self.assertAlmostEqual(one - two, 361.0)

    def test_the_reserve_is_what_saves_the_adapter(self):
        # The adapter is written before the final eval, but it still has to be
        # pulled, and a lease that ends first takes /content with it.
        self.assertAlmostEqual(
            pilot_math.training_budget_s(3600.0, 250.0, 361.0, 2, 600.0),
            2028.0)

    def test_a_lease_that_buys_no_training_is_an_error(self):
        # Better to refuse than to rent a T4 for an hour of overhead.
        with self.assertRaises(ValueError):
            pilot_math.training_budget_s(900.0, 250.0, 361.0, 2, 180.0)

    def test_end_to_end_against_the_measured_cost(self):
        # 2448 s at the probe's 29.1 s/row, effective batch 4: 21 steps, two
        # fewer than the run that overran.
        budget = pilot_math.training_budget_s(3600.0, 250.0, 361.0, 2, 180.0)
        self.assertEqual(pilot_math.compute_max_steps(budget, 29.1, 1, 4), 21)


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
