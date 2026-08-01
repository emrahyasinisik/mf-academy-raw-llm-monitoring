"""Version and capability gates, tested without importing torch."""

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import preflight


class TestVersionAtLeast(unittest.TestCase):
    def test_equal_passes(self):
        self.assertTrue(preflight.version_at_least("4.51", "4.51"))

    def test_patch_above_floor_passes(self):
        self.assertTrue(preflight.version_at_least("4.51.3", "4.51"))

    def test_below_floor_fails(self):
        self.assertFalse(preflight.version_at_least("4.44.2", "4.51"))

    def test_minor_compared_numerically_not_lexically(self):
        # "4.9" > "4.51" as strings. This is the comparison that lets a stale
        # transformers through, and it fails at model load half an hour later
        # wearing an "unknown architecture" mask.
        self.assertFalse(preflight.version_at_least("4.9.0", "4.51"))

    def test_dev_suffix_tolerated(self):
        self.assertTrue(preflight.version_at_least("4.52.0.dev0", "4.51"))


class TestCheckCapability(unittest.TestCase):
    def test_t4_passes(self):
        ok, msg = preflight.check_capability((7, 5), allow_any=False)
        self.assertTrue(ok)
        self.assertIn("sm_75", msg)

    def test_other_card_fails_loudly(self):
        ok, msg = preflight.check_capability((8, 9), allow_any=False)
        self.assertFalse(ok)
        self.assertIn("PILOT_ALLOW_NON_T4", msg)

    def test_override_passes_but_says_the_numbers_move(self):
        ok, msg = preflight.check_capability((8, 9), allow_any=True)
        self.assertTrue(ok)
        self.assertIn("T4", msg)


if __name__ == "__main__":
    unittest.main()
