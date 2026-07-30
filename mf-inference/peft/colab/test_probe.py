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
