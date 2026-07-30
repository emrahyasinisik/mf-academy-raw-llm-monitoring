"""Parser test for the memory sampler. The measurement itself needs a GPU."""

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import probe_step_cost as probe


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
