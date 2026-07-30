"""Flag-surface tests for train_qlora_qwen.py, without a GPU or torch.

The module imports torch, transformers, peft and datasets at import time and
exits if any is missing — none of which exist on the Mac. The four heavy
modules are stubbed in sys.modules so argparse can be reached. That is worth
doing rather than skipping: a typo in a flag name is not visible until a VM
has been rented, a model downloaded and forty minutes spent, and it surfaces
as an unrecognised-argument error at the very end of the setup.
"""

import importlib
import os
import sys
import unittest
from unittest import mock

PEFT_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

HEAVY = ("torch", "datasets", "peft", "transformers")


def load_train_module():
    with mock.patch.dict(sys.modules, {name: mock.MagicMock() for name in HEAVY}):
        sys.path.insert(0, PEFT_DIR)
        try:
            import train_qlora_qwen
            return importlib.reload(train_qlora_qwen)
        finally:
            sys.path.remove(PEFT_DIR)


class TestPilotFlags(unittest.TestCase):
    def setUp(self):
        self.mod = load_train_module()

    def parse(self, argv):
        with mock.patch.object(sys, "argv", ["train_qlora_qwen.py", *argv]):
            return self.mod.parse_args()

    def test_defaults_are_todays_behaviour(self):
        # The Kaggle notebooks pass none of these and must keep running
        # unchanged; every default here is the pre-pilot behaviour.
        args = self.parse([])
        self.assertEqual(args.max_steps, 0)
        self.assertEqual(args.save_steps, 0)
        self.assertFalse(args.resume)
        self.assertFalse(args.no_4bit)

    def test_pilot_invocation_parses(self):
        args = self.parse([
            "--train", "data/pilot/rubric_train.jsonl",
            "--eval", "data/pilot/rubric_eval.jsonl",
            "--out-dir", "out/colab-pilot",
            "--grad-accum", "4",
            "--max-steps", "22",
            "--save-steps", "5",
        ])
        self.assertEqual(args.max_steps, 22)
        self.assertEqual(args.save_steps, 5)
        self.assertEqual(args.grad_accum, 4)
        self.assertEqual(args.out_dir, "out/colab-pilot")

    def test_no_4bit_is_the_probes_second_regime(self):
        self.assertTrue(self.parse(["--no-4bit"]).no_4bit)

    def test_resume_is_a_switch(self):
        self.assertTrue(self.parse(["--resume"]).resume)


if __name__ == "__main__":
    unittest.main()
