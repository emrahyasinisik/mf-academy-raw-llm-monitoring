"""Parser test for the memory sampler. The measurement itself needs a GPU."""

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import probe_step_cost as probe


class TestRunsAsANotebookCell(unittest.TestCase):
    def test_imports_without_dunder_file(self):
        # `colab exec -f` does not run the file as a script — it sends the text
        # to the kernel and executes it as a cell, where __file__ is undefined.
        # The first version of this module derived its sys.path entry from
        # __file__ and died on line 34 with a NameError, before measuring
        # anything.
        src = open(probe.__file__, encoding="utf-8").read()
        cell_globals = {"__name__": "not_main"}   # so main() does not fire
        exec(compile(src, "<cell>", "exec"), cell_globals)
        self.assertIn("pilot_math", cell_globals)


class TestMeasuredCost(unittest.TestCase):
    """s/row must come from the Trainer's clock, never from wall minus a guess.

    Two runs of identical work priced out at 20.6 and 10.3 s/row because the
    first paid an 8 GB model download and the second did not, while both had
    the same guessed 240 s subtracted. The Trainer's own train_runtime read
    232.4 s and 235.7 s across those same two runs — a 1% spread on the number
    that actually decides --max-steps.
    """

    def test_reads_train_runtime_from_the_metrics_file(self):
        metrics = {"train": {"train_runtime": 232.4435}, "row_passes": 8}
        self.assertAlmostEqual(probe.measured_s_per_row(metrics), 29.06, places=2)

    def test_falls_back_to_the_row_count_it_was_given(self):
        metrics = {"train": {"train_runtime": 100.0}}
        self.assertAlmostEqual(probe.measured_s_per_row(metrics, rows=8), 12.5)

    def test_missing_runtime_is_an_error_not_a_zero(self):
        # A silent 0.0 would flow into compute_max_steps, which raises on it —
        # but the message would blame the probe rather than the metrics file.
        with self.assertRaises(ValueError):
            probe.measured_s_per_row({"train": {}}, rows=8)

    def test_zero_rows_is_an_error(self):
        with self.assertRaises(ValueError):
            probe.measured_s_per_row({"train": {"train_runtime": 100.0}}, rows=0)


class TestParseSmiMib(unittest.TestCase):
    def test_single_gpu(self):
        self.assertEqual(probe.parse_smi_mib("5312 MiB\n"), 5312)

    def test_units_optional(self):
        # nvidia-smi with --format=csv,noheader,nounits drops the suffix.
        self.assertEqual(probe.parse_smi_mib("5312\n"), 5312)

    def test_takes_the_max_across_devices(self):
        self.assertEqual(probe.parse_smi_mib("512 MiB\n15109 MiB\n"), 15109)

    def test_garbage_is_zero_not_a_crash(self):
        # A sampler that raises would kill the probe thread and take the
        # measurement with it. A missing memory number is worth far less than
        # a missing s/row.
        self.assertEqual(probe.parse_smi_mib("N/A\n"), 0)
        self.assertEqual(probe.parse_smi_mib(""), 0)
        self.assertEqual(probe.parse_smi_mib(None), 0)


if __name__ == "__main__":
    unittest.main()
