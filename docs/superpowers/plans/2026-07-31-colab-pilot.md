# Colab Pilot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `mf-inference/peft/colab/` hattını kurmak ve ücretsiz tier Colab'da bir saatlik pilot koşuyu yürütüp Colab'ın kendi ölçülmüş s/satır sayısını kaydetmek.

**Architecture:** Mac sürücü, VM icracı. `colab` CLI ile T4 oturumu açılır; script'ler `colab upload` ile VM'e gider (git clone değil — gerekçe aşağıda), veri `colab upload` ile, ölçüm `probe_step_cost.py` ile alınır, `--max-steps` o ölçümden hesaplanır, eğitim VM'de detached koşar, adapter `colab download` ile Mac'e iner, eval aynı oturumda taban+adapter için koşar.

**Tech Stack:** Python 3 (stdlib `unittest`, ek test bağımlılığı yok), bash, `colab` CLI 0.6.0, transformers/peft/bitsandbytes/accelerate/datasets, Qwen3-4B-Instruct-2507, T4 (sm_75).

**Kaynak tasarım:** `docs/superpowers/specs/2026-07-30-colab-pilot-design.md` — okunmadan bu plan uygulanmaz. Aritmetiği, kapsam dışı bırakılan iki kolu ve kabul kriterlerini o belge taşıyor.

## Global Constraints

- **Ücretsiz tier.** Compute unit satın alınmıyor, `colab pay` çağrılmıyor.
- **Tek T4, sm_75.** fp16 + GradScaler; bf16 yok, flash-attention 2 yok. Başka kart gelirse koşu durur (kaçış: `PILOT_ALLOW_NON_T4=1`), çünkü maliyet aritmetiği T4'e ait.
- **`--max-seq-len 2560` değişmez.** Ölçülmüş sayı (`measure_tokens.py`). Kırpmak soldan olur ve modele görmediği kanıta atıf yapmayı öğretir.
- **Vaka başına kriter sayısı düşürülmez.** Üretim 9 kriterlik vaka servis ediyor.
- **Çıktı adı `colab-pilot`, dizin `out/colab-pilot`.** `out/rubric-v1` ile karışmayacak; bu çıktı yayına girmez ve hiçbir sayı iddia etmez.
- **Pilot seti `data/pilot/` altına yazılır.** `merge_rubric_sets.py --out-dir` atlanırsa çıktı `data/rubric_train.jsonl` olur ve tam seti ezer; `data/` gitignore'da, geri alınacak sürüm yok.
- **Kaggle hattı silinmiyor.** `kaggle/` altındaki notebook'lar, `push.sh`, `kernel-metadata.json`'lar olduğu gibi kalır.
- **Bugünkü davranış varsayılan kalır.** `train_qlora_qwen.py`'ye eklenen dört bayrağın hepsi kapalı varsayılana sahip; Kaggle notebook'ları değişmeden koşmaya devam eder.
- **Yorumlar *neden*i anlatır** (CLAUDE.md). Kod İngilizce, belge Türkçe — repodaki mevcut ayrım.
- **Testler ek bağımlılık istemez.** Mac'te torch da pytest de kurulu değil; testler stdlib `unittest` ile koşar ve torch'u `sys.modules` üzerinden stub'lar.

### Tasarımdan bilinçli sapma: git clone yerine upload

Tasarım script'lerin VM'de `git clone` ile alınmasını yazıyordu. Uygulama `colab upload` kullanıyor, iki sebeple:

1. Clone, koşulacak kodun **push edilmiş** olmasını şart koşar. Pilotun tamamı bu planın kendi commit'leriyle eş zamanlı yazılıyor; yarım push'lanmış bir ağaçtan clone, Mac'te düzelttiğin hatayı VM'de tekrar görmek demek.
2. Aynı desen bu repoda zaten bir koşuya mal oldu: Flutter v7 yalnız Kaggle'ın sürüm geçmişinde yaşamıştı.

Karşı maliyet — koşulan kodun bir SHA'sı olmaması — sürücünün her koşuda `out/run_manifest.json`'a `git rev-parse HEAD` ve kirlilik bayrağı yazmasıyla karşılanır (Task 5).

---

### Task 1: Pilot aritmetiği — `pilot_math.py`

Sürücünün `--max-steps`'i hesapladığı ve probe'un projeksiyonu bastığı üç fonksiyon. Ayrı ve saf bir modül, çünkü Mac'te torch yok: bu repoda yerel olarak koşabilen tek test yüzeyi bağımlılıksız olan.

**Files:**
- Create: `mf-inference/peft/colab/pilot_math.py`
- Test: `mf-inference/peft/colab/test_pilot_math.py`

**Interfaces:**
- Consumes: yok.
- Produces:
  - `seconds_per_row(wall_s: float, rows: int, load_s: float) -> float`
  - `compute_max_steps(budget_s: float, s_per_row: float, batch_size: int, grad_accum: int) -> int`
  - `project_full_run_hours(s_per_row: float, rows: int, epochs: float) -> float`
  - `sessions_needed(hours: float, session_hours: float) -> int`

- [ ] **Step 1: Write the failing test**

`mf-inference/peft/colab/test_pilot_math.py`:

```python
"""Unit tests for the pilot's arithmetic.

Run from the repo root:
    python3 -m unittest discover -s mf-inference/peft/colab -p 'test_*.py' -v
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest discover -s mf-inference/peft/colab -p 'test_*.py' -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'pilot_math'`

- [ ] **Step 3: Write minimal implementation**

`mf-inference/peft/colab/pilot_math.py`:

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest discover -s mf-inference/peft/colab -p 'test_*.py' -v`
Expected: PASS — 9 tests

- [ ] **Step 5: Commit**

```bash
git add mf-inference/peft/colab/pilot_math.py mf-inference/peft/colab/test_pilot_math.py
git commit -m "feat(peft): price the pilot's step budget where it can be tested

The Mac has no torch and no GPU, so the arithmetic that decides --max-steps
lives in a module that imports neither. An untested step count is the guess
that already cost a twelve-hour run."
```

---

### Task 2: `train_qlora_qwen.py` — dört bayrak

`--max-steps` pilotun süre sınırını taşır, `--save-steps` kotanın pilotu kesmesine karşı korur, `--resume` tam koşunun oturum zincirinin altyapısını bırakır, `--no-4bit` probe'un ölçtüğü ikinci rejimi açar. Hepsinin varsayılanı bugünkü davranış.

**Files:**
- Modify: `mf-inference/peft/train_qlora_qwen.py` (argparse bloğu ~satır 103-118; model yükleme ~204-219; TrainingArguments ~252-276; `trainer.train()` ~285)
- Test: `mf-inference/peft/colab/test_train_flags.py`

**Interfaces:**
- Consumes: yok.
- Produces: `train_qlora_qwen.parse_args()` artık `args.max_steps: int`, `args.save_steps: int`, `args.resume: bool`, `args.no_4bit: bool` taşır. Task 4 ve Task 5 bu bayrakları komut satırından geçirir.

- [ ] **Step 1: Write the failing test**

`mf-inference/peft/colab/test_train_flags.py`:

```python
"""Flag-surface tests for train_qlora_qwen.py, without a GPU or torch.

The module imports torch, transformers, peft and datasets at import time and
exits if any is missing — none of which exist on the Mac. The four heavy
modules are stubbed in sys.modules so argparse can be reached. That is worth
doing rather than skipping: a typo in a flag name is not visible until a VM
has been rented, a model downloaded and forty minutes spent, and it surfaces
as an unrecognised-argument error at the very end of the setup.
"""

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
            import importlib
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest discover -s mf-inference/peft/colab -p 'test_train_flags.py' -v`
Expected: FAIL — `AttributeError: 'Namespace' object has no attribute 'max_steps'`

- [ ] **Step 3: Write minimal implementation**

`train_qlora_qwen.py` — argparse bloğuna, `--seed` satırının hemen üstüne ekle:

```python
    # The pilot's four flags. Every default below is the pre-pilot behaviour,
    # so the Kaggle notebooks keep running unchanged.
    #
    # A step limit rather than a smaller dataset: data size *predicts* wall
    # time, max_steps *guarantees* it, and the free tier gives a session wall
    # whose position is not known in advance.
    ap.add_argument("--max-steps", type=int, default=0,
                    help=">0 replaces --epochs with a hard optimizer-step "
                         "limit; derive it from a measured s/row, never type it")
    ap.add_argument("--save-steps", type=int, default=0,
                    help=">0 switches to step-based checkpointing, which the "
                         "free tier's dynamic quota makes load-bearing")
    ap.add_argument("--resume", action="store_true",
                    help="continue from the last checkpoint in --out-dir")
    # 28-35 s/row is roughly 6-8x slower than the T4's FLOP budget explains,
    # and the first suspect is bitsandbytes' NF4 dequant on every matmul. This
    # flag is how that hypothesis gets measured instead of argued about.
    ap.add_argument("--no-4bit", action="store_true",
                    help="load the base in fp16 instead of 4-bit NF4; may OOM "
                         "on a 16 GB card, which is itself a result")
```

Model yükleme bloğunu (`quant = BitsAndBytesConfig(...)` ile `model = prepare_model_for_kbit_training(...)` arası) şununla değiştir:

```python
    if args.no_4bit:
        # ~8 GB of fp16 weights on a 16 GB T4, with gradient checkpointing,
        # batch 1 and LoRA only on attention. prepare_model_for_kbit_training
        # is skipped because there are no k-bit layers to prepare; what it
        # does that still matters here — enabling checkpointing and making
        # the frozen embedding pass gradients through to the adapters — is
        # done explicitly below.
        model = AutoModelForCausalLM.from_pretrained(
            args.base_model,
            device_map={"": 0},
            attn_implementation="sdpa",
            torch_dtype=torch.float16,
        )
        model.config.use_cache = False
        model.gradient_checkpointing_enable(
            gradient_checkpointing_kwargs={"use_reentrant": False})
        model.enable_input_require_grads()
    else:
        quant = BitsAndBytesConfig(
            load_in_4bit=True,
            bnb_4bit_quant_type="nf4",
            bnb_4bit_use_double_quant=True,
            bnb_4bit_compute_dtype=torch.float16,
        )

        model = AutoModelForCausalLM.from_pretrained(
            args.base_model,
            quantization_config=quant,
            device_map={"": 0},
            attn_implementation="sdpa",
            torch_dtype=torch.float16,
        )
        model.config.use_cache = False
        model = prepare_model_for_kbit_training(
            model, use_gradient_checkpointing=True)
```

`Trainer(...)` çağrısının hemen üstüne ekle:

```python
    # Step-based saving and epoch-based eval cannot both be the reference for
    # "best": Trainer requires the two strategies to match when it is asked to
    # reload the best checkpoint. Under a step limit there is usually no epoch
    # boundary at all, so there would be no checkpoint to reload and the run
    # would die at the end having trained fine.
    save_strategy = "steps" if args.save_steps > 0 else "epoch"
    load_best = not (args.save_steps > 0 or args.max_steps > 0)
```

`TrainingArguments(...)` içinde şu üç satırı değiştir:

```python
            num_train_epochs=args.epochs,
            max_steps=args.max_steps if args.max_steps > 0 else -1,
            ...
            save_strategy=save_strategy,
            save_steps=args.save_steps if args.save_steps > 0 else 500,
            save_total_limit=2,
            load_best_model_at_end=load_best,
```

Adım sayısı çıktısını ve `trainer.train()` çağrısını değiştir:

```python
    steps = (args.max_steps if args.max_steps > 0 else
             max(1, int(len(train_ds) * args.epochs //
                        (args.batch_size * args.grad_accum))))
    limit = " (--max-steps)" if args.max_steps > 0 else ""
    print(f"\neffective batch: {args.batch_size * args.grad_accum} rows/step"
          f"  ~{steps} optimizer steps{limit}")
    trainer.train(resume_from_checkpoint=args.resume or None)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest discover -s mf-inference/peft/colab -p 'test_*.py' -v`
Expected: PASS — 13 tests (Task 1'in 9'u + bu 4)

Ayrıca sözdizimi: `python3 -m py_compile mf-inference/peft/train_qlora_qwen.py` → çıktı yok, exit 0.

- [ ] **Step 5: Commit**

```bash
git add mf-inference/peft/train_qlora_qwen.py mf-inference/peft/colab/test_train_flags.py
git commit -m "feat(peft): put a wall on the run, not on the data

--max-steps, --save-steps, --resume and --no-4bit, all defaulting to today's
behaviour so the Kaggle notebooks are untouched. Data size predicts wall time;
a step limit guarantees it, and the free tier's session wall is not somewhere
you find out about by prediction."
```

---

### Task 3: Ortam sözleşmesi — `requirements-colab.txt` ve `preflight.py`

Colab'ın önyüklü `transformers`'ı Qwen3 için eski olabilir; Kaggle'da bu sessizce `unknown architecture` ile düşmüştü. Kart da garanti değil. İkisi de oturumun ilk saniyelerinde, model indirmeden önce doğrulanır.

**Files:**
- Create: `mf-inference/peft/colab/requirements-colab.txt`
- Create: `mf-inference/peft/colab/preflight.py`
- Test: `mf-inference/peft/colab/test_preflight.py`

**Interfaces:**
- Consumes: yok.
- Produces:
  - `preflight.version_at_least(found: str, floor: str) -> bool`
  - `preflight.check_capability(cap: tuple[int, int], allow_any: bool) -> tuple[bool, str]`
  - VM'de koşunca: uygunsa exit 0 ve `/content/out/preflight.json`; değilse exit 1.

- [ ] **Step 1: Write the failing test**

`mf-inference/peft/colab/test_preflight.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest discover -s mf-inference/peft/colab -p 'test_preflight.py' -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'preflight'`

- [ ] **Step 3: Write minimal implementation**

`mf-inference/peft/colab/requirements-colab.txt`:

```
# Pinned floors for the Colab VM. Colab ships its own torch built against the
# VM's driver — it is deliberately NOT listed here, because replacing it is how
# you turn a working CUDA runtime into a CPU-only one on a rented machine.
#
# transformers is the one that matters: Qwen3 needs 4.51, and an older release
# does not refuse the model, it fails to recognise the architecture in a
# message that reads like a corrupt download.
transformers>=4.51
peft>=0.11
bitsandbytes>=0.43
accelerate>=0.30
datasets>=2.19
```

`mf-inference/peft/colab/preflight.py`:

```python
#!/usr/bin/env python3
"""Fail in the first ten seconds of a Colab session, not the fortieth minute.

Two things about a free-tier VM are not knowable in advance: which card it
gives, and how old its preinstalled libraries are. Both failures, left
unchecked, arrive after the model download as errors about something else —
an "unknown architecture", or a cost curve that quietly belongs to a different
GPU. This runs before anything is downloaded.

    colab exec -s pilot -f colab/preflight.py
"""

from __future__ import annotations

import json
import os
import re
import sys

FLOORS = {"transformers": "4.51", "peft": "0.11",
          "bitsandbytes": "0.43", "accelerate": "0.30"}

OUT = "/content/out/preflight.json"


def version_at_least(found: str, floor: str) -> bool:
    """Numeric, component-wise comparison.

    Not a string compare: "4.9" sorts above "4.51" and that is exactly the
    stale transformers this gate exists to catch.
    """
    def parts(v: str) -> list[int]:
        return [int(p) for p in re.findall(r"\d+", v)]

    a, b = parts(found), parts(floor)
    a += [0] * (len(b) - len(a))
    b += [0] * (len(a) - len(b))
    return a >= b


def check_capability(cap: tuple[int, int], allow_any: bool) -> tuple[bool, str]:
    """The pilot's cost arithmetic is a T4's. Anything else is a different run."""
    sm = f"sm_{cap[0]}{cap[1]}"
    if cap == (7, 5):
        return True, f"{sm} — T4 as designed"
    if allow_any:
        return True, (f"{sm} is NOT a T4; PILOT_ALLOW_NON_T4 is set, so the run "
                      f"proceeds. Every projection it produces belongs to this "
                      f"card and must be labelled as such.")
    return False, (f"{sm} is not sm_75. The measured cost, the fp16/bf16 choice "
                   f"and the whole projection assume a T4. Release this VM and "
                   f"ask again, or set PILOT_ALLOW_NON_T4=1 to proceed knowing "
                   f"the numbers describe a different card.")


def main() -> int:
    import torch

    if not torch.cuda.is_available():
        print("no CUDA device — the session was created without --gpu T4")
        return 1

    cap = torch.cuda.get_device_capability(0)
    name = torch.cuda.get_device_name(0)
    total_gb = torch.cuda.get_device_properties(0).total_memory / 1024**3
    count = torch.cuda.device_count()
    print(f"device: {name}  sm_{cap[0]}{cap[1]}  {total_gb:.1f} GB  "
          f"(device_count {count})")

    ok, msg = check_capability(cap, bool(os.environ.get("PILOT_ALLOW_NON_T4")))
    print(msg)

    versions = {}
    stale = []
    for mod, floor in FLOORS.items():
        try:
            versions[mod] = __import__(mod).__version__
        except Exception as exc:  # noqa: BLE001 - report, do not raise
            versions[mod] = f"<missing: {exc}>"
            stale.append(f"{mod} not importable")
            continue
        if not version_at_least(versions[mod], floor):
            stale.append(f"{mod} {versions[mod]} < {floor}")
    print("  " + "  ".join(f"{k} {v}" for k, v in versions.items()))

    for s in stale:
        print(f"STALE: {s} — run `colab install -r colab/requirements-colab.txt`")

    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as fh:
        json.dump({"device": name, "sm": f"{cap[0]}{cap[1]}",
                   "total_gb": round(total_gb, 1), "device_count": count,
                   "versions": versions, "stale": stale, "capability_ok": ok},
                  fh, indent=2)

    return 0 if (ok and not stale) else 1


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest discover -s mf-inference/peft/colab -p 'test_*.py' -v`
Expected: PASS — 21 tests

- [ ] **Step 5: Commit**

```bash
git add mf-inference/peft/colab/requirements-colab.txt mf-inference/peft/colab/preflight.py mf-inference/peft/colab/test_preflight.py
git commit -m "feat(peft): check the card and the libraries before the download

A free-tier VM does not promise which GPU it hands you or how old its
transformers is. Both failures otherwise arrive after the 8 GB download,
wearing other masks."
```

---

### Task 4: Adım maliyeti ölçümü — `probe_step_cost.py`

Kaggle probe'unun Colab karşılığı, iki farkla: sorulan soru cihaz sayısı değil kuantizasyon rejimi, ve tepe bellek de kaydediliyor (fp16 kolunun sığıp sığmadığı ancak öyle bilinir).

**Files:**
- Create: `mf-inference/peft/colab/probe_step_cost.py`
- Test: `mf-inference/peft/colab/test_probe.py`

**Interfaces:**
- Consumes: `pilot_math.seconds_per_row`, `pilot_math.project_full_run_hours`, `train_qlora_qwen.py`'nin `--no-4bit` bayrağı (Task 2).
- Produces: `/content/out/probe.json` — `{"rows": int, "load_s": float, "regimes": {"4bit"|"fp16": {"wall_s", "s_per_row", "peak_mib", "returncode", "tail"}}}`. Task 5 bu dosyadan `s_per_row` okur.
- Produces: `probe.parse_smi_mib(text: str) -> int`

- [ ] **Step 1: Write the failing test**

`mf-inference/peft/colab/test_probe.py`:

```python
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
        # measurement with it. A missing memory number is worth less than a
        # missing s/row.
        self.assertEqual(probe.parse_smi_mib("N/A\n"), 0)
        self.assertEqual(probe.parse_smi_mib(""), 0)


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest discover -s mf-inference/peft/colab -p 'test_probe.py' -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'probe_step_cost'`

- [ ] **Step 3: Write minimal implementation**

`mf-inference/peft/colab/probe_step_cost.py`:

```python
#!/usr/bin/env python3
"""Measure what one row costs on this VM's card. Does not train anything.

The sibling of kaggle/probe/rubric-probe.ipynb, which asked whether the device
count set the step cost and answered no. The question here is the next suspect
on that list: bitsandbytes' NF4 dequant, which runs on every matmul and is the
plausible reason 28-35 s/row sits 6-8x above what a T4's FLOPs explain.

Two regimes, same script, same data, same everything else:

    4bit   what the cancelled Kaggle run did
    fp16   --no-4bit; ~8 GB of weights on a 16 GB card, so it may OOM,
           and an OOM is a result — it closes the branch honestly

It runs train_qlora_qwen.py as a subprocess rather than importing it: the
regimes differ in how CUDA is initialised, which one process cannot do twice.

    colab exec -s pilot -f colab/probe_step_cost.py --timeout 2400
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import threading
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import pilot_math  # noqa: E402

PEFT = "/content/peft"
PROBE_ROWS = 8
EVAL_ROWS = 4
# Model download + load, paid once per regime and identical between them.
# Subtracted so a short probe is not priced above the long run it predicts.
LOAD_GUESS_S = 240.0
# What the full run is: the merged 1600-row set, three epochs.
FULL_ROWS, FULL_EPOCHS = 1600, 3.0
SESSION_HOURS = 3.0  # a free-tier slice, per the design's quota note

OUT = "/content/out/probe.json"


def parse_smi_mib(text: str) -> int:
    """Largest MiB figure in an nvidia-smi memory query; 0 if unreadable.

    Never raises: this runs on a sampler thread, and losing the thread would
    lose the wall-clock measurement the probe actually exists for.
    """
    nums = [int(n) for n in re.findall(r"\d+", text or "")]
    return max(nums) if nums else 0


class PeakMemory:
    """Poll nvidia-smi while a child trains.

    torch.cuda.max_memory_allocated lives in the child's process and dies with
    it, and it would only report torch's allocator anyway — not the CUDA
    context or bitsandbytes' own buffers, which are exactly what is in question
    when asking whether fp16 fits.
    """

    def __init__(self, interval: float = 2.0):
        self.interval, self.peak, self._stop = interval, 0, threading.Event()
        self._thread = threading.Thread(target=self._run, daemon=True)

    def _run(self) -> None:
        cmd = ["nvidia-smi", "--query-gpu=memory.used",
               "--format=csv,noheader,nounits"]
        while not self._stop.is_set():
            try:
                out = subprocess.run(cmd, capture_output=True, text=True,
                                     timeout=10).stdout
                self.peak = max(self.peak, parse_smi_mib(out))
            except Exception:  # noqa: BLE001 - a lost sample is not a failure
                pass
            self._stop.wait(self.interval)

    def __enter__(self):
        self._thread.start()
        return self

    def __exit__(self, *exc):
        self._stop.set()
        self._thread.join(timeout=5)


def sample_rows() -> None:
    """8 train / 4 eval rows at a fixed stride.

    Stride, not head: sequence length drives the cost being measured and the
    front of the file is not a random sample of its length distribution.
    """
    for src, dst, n in (("data/pilot/rubric_train.jsonl",
                         "data/probe_train.jsonl", PROBE_ROWS),
                        ("data/pilot/rubric_eval.jsonl",
                         "data/probe_eval.jsonl", EVAL_ROWS)):
        rows = [l for l in open(os.path.join(PEFT, src), encoding="utf-8")
                if l.strip()]
        stride = max(1, len(rows) // n)
        picked = [rows[i * stride] for i in range(min(n, len(rows)))]
        with open(os.path.join(PEFT, dst), "w", encoding="utf-8") as fh:
            fh.writelines(picked)
        print(f"{dst}: {len(picked)} of {len(rows)} rows")


def run_regime(label: str, extra_args: list[str]) -> dict:
    args = [sys.executable, "train_qlora_qwen.py",
            "--train", "data/probe_train.jsonl",
            "--eval", "data/probe_eval.jsonl",
            "--max-seq-len", "2560",
            "--epochs", "1", "--grad-accum", "1",
            "--out-dir", f"out/probe_{label}"] + extra_args

    env = dict(os.environ, PYTORCH_CUDA_ALLOC_CONF="expandable_segments:True")
    print(f"\n===== regime {label} =====", flush=True)
    t0 = time.time()
    with PeakMemory() as mem:
        proc = subprocess.run(args, cwd=PEFT, env=env,
                              capture_output=True, text=True)
    wall = time.time() - t0

    tail = (proc.stdout or "")[-1500:] + (proc.stderr or "")[-1500:]
    print(tail, flush=True)

    per_row = (pilot_math.seconds_per_row(wall, PROBE_ROWS, LOAD_GUESS_S)
               if proc.returncode == 0 else 0.0)
    result = {"wall_s": round(wall, 1), "s_per_row": round(per_row, 1),
              "peak_mib": mem.peak, "returncode": proc.returncode,
              "tail": tail[-1500:]}

    if proc.returncode != 0:
        print(f"{label}: FAILED (exit {proc.returncode}) — no projection. "
              f"If this is the fp16 arm and the tail says OOM, that closes "
              f"the branch and is worth as much as a number.")
        return result

    hours = pilot_math.project_full_run_hours(per_row, FULL_ROWS, FULL_EPOCHS)
    print(f"{label}: wall {wall / 60:.1f} min   {per_row:.1f} s/row   "
          f"peak {mem.peak} MiB")
    print(f"  full run ({FULL_ROWS} rows x {FULL_EPOCHS:g}) -> {hours:.1f} h "
          f"= {pilot_math.sessions_needed(hours, SESSION_HOURS)} free-tier "
          f"sessions of {SESSION_HOURS:g} h")
    result["full_run_hours"] = round(hours, 1)
    result["sessions"] = pilot_math.sessions_needed(hours, SESSION_HOURS)
    return result


def main() -> int:
    sample_rows()
    regimes = {"4bit": run_regime("4bit", []),
               "fp16": run_regime("fp16", ["--no-4bit"])}

    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as fh:
        json.dump({"rows": PROBE_ROWS, "load_s": LOAD_GUESS_S,
                   "regimes": regimes}, fh, indent=2)
    print(f"\nwrote {OUT}")

    ok = [r for r in regimes.values() if r["returncode"] == 0]
    if not ok:
        print("both regimes failed — there is no measured cost, so there is "
              "no honest --max-steps to derive. Do not start training.")
        return 1

    cheapest = min(ok, key=lambda r: r["s_per_row"])
    print(f"\ncheapest regime: {cheapest['s_per_row']:.1f} s/row")
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest discover -s mf-inference/peft/colab -p 'test_*.py' -v`
Expected: PASS — 25 tests

- [ ] **Step 5: Commit**

```bash
git add mf-inference/peft/colab/probe_step_cost.py mf-inference/peft/colab/test_probe.py
git commit -m "feat(peft): measure the row before renting the hour

Two regimes — today's 4-bit NF4 and plain fp16 — on eight stride-sampled rows,
with peak memory, because whether fp16 fits a 16 GB card is not answerable
from arithmetic. The Kaggle probe killed the DataParallel hypothesis in
fifteen minutes; NF4 dequant is the next name on that list."
```

---

### Task 5: Mac tarafı sürücü — `run_pilot.sh`

Faz faz, çünkü `colab exec`'in varsayılan timeout'u 30 saniye ve ücretsiz tier pilotun ortasında kesebilir. Her faz tek başına yeniden koşturulabilir.

**Files:**
- Create: `mf-inference/peft/colab/run_pilot.sh`
- Test: `mf-inference/peft/colab/test_driver.sh`

**Interfaces:**
- Consumes: `colab` CLI; `colab/preflight.py`, `colab/probe_step_cost.py`, `colab/pilot_math.py`; `train_qlora_qwen.py`, `rubric_eval.py`; `data/pilot/rubric_{train,eval}.jsonl`.
- Produces: fazlar — `session preflight deps push data probe train watch pull eval stop all` — ve Mac'te `out/colab-pilot/`, `out/probe.json`, `out/pilot_eval.json`, `out/run_manifest.json`.

- [ ] **Step 1: Write the failing test**

`mf-inference/peft/colab/test_driver.sh`:

```bash
#!/usr/bin/env bash
# Driver checks that do not rent a VM: syntax, and that every phase in the
# documented list actually dispatches. A typo in the case arm is otherwise
# found with a GPU already running and the clock already spending.
set -uo pipefail

cd "$(dirname "$0")"
fail=0

check() {
  if eval "$2" >/dev/null 2>&1; then
    echo "ok   $1"
  else
    echo "FAIL $1"
    fail=1
  fi
}

check "bash syntax" "bash -n run_pilot.sh"
check "executable"  "test -x run_pilot.sh"

out=$(DRY_RUN=1 ./run_pilot.sh all 2>&1)

for phase in session preflight deps push data probe train watch pull eval stop; do
  check "phase '$phase' dispatches" "grep -q \"phase: $phase\" <<< \"\$out\""
done

check "never calls colab pay"        "! grep -q 'colab pay' run_pilot.sh"
check "asks for a T4"                "grep -q -- '--gpu T4' run_pilot.sh"
check "pilot out-dir, not rubric-v1" "grep -q 'out/colab-pilot' run_pilot.sh"
check "training runs detached"       "grep -q 'start_new_session' run_pilot.sh"
check "unknown phase is an error"    "! ./run_pilot.sh nonsense >/dev/null 2>&1"

exit $fail
```

`chmod +x mf-inference/peft/colab/test_driver.sh`

- [ ] **Step 2: Run test to verify it fails**

Run: `mf-inference/peft/colab/test_driver.sh`
Expected: FAIL — `bash: run_pilot.sh: No such file or directory`, birden çok `FAIL` satırı, exit 1

- [ ] **Step 3: Write minimal implementation**

`mf-inference/peft/colab/run_pilot.sh` (`chmod +x`):

```bash
#!/usr/bin/env bash
# Mac-side driver for the Colab pilot. The VM executes; this decides.
#
# Phases, not one command, for two reasons the free tier imposes:
#   * `colab exec` times out after 30 s by default, so training cannot block
#     inside it — it is detached on the VM and polled from here.
#   * a dynamic quota can cut the session at any point; every phase is
#     separately re-runnable against a fresh session.
#
# Usage, from mf-inference/peft/:
#   colab/run_pilot.sh session      # rent the T4
#   colab/run_pilot.sh preflight    # card + library gate, before any download
#   colab/run_pilot.sh deps push data
#   colab/run_pilot.sh probe        # ~25 min; writes out/probe.json
#   colab/run_pilot.sh train        # derives --max-steps from the probe
#   colab/run_pilot.sh watch        # poll until the adapter is written
#   colab/run_pilot.sh pull eval stop
#
# DRY_RUN=1 prints the plan without touching the network.
set -euo pipefail

SESSION="${SESSION:-pilot}"
REMOTE="/content/peft"
# The design's arithmetic: an hour minus model download, tokenisation, eval and
# checkpointing. This is the only number the pilot promises to hit.
BUDGET_S="${BUDGET_S:-2700}"
GRAD_ACCUM="${GRAD_ACCUM:-4}"   # 4, not 16: four times the optimizer steps for
                                # the same row-passes, so the loss curve has
                                # something in it to look at
BATCH_SIZE=1
SAVE_STEPS="${SAVE_STEPS:-5}"
OUT_DIR="out/colab-pilot"       # never out/rubric-v1 — this build does not ship
EVAL_LIMIT="${EVAL_LIMIT:-40}"

cd "$(dirname "$0")/.."         # mf-inference/peft
HERE="$(pwd)"

say() { printf '\n=== %s ===\n' "$*"; }
run() {
  if [[ -n "${DRY_RUN:-}" ]]; then printf '  [dry] %s\n' "$*"; else "$@"; fi
}

phase_session() {
  say "phase: session"
  run colab new -s "$SESSION" --gpu T4
  run colab status -s "$SESSION"
}

phase_preflight() {
  say "phase: preflight"
  run colab exec -s "$SESSION" -f colab/preflight.py --timeout 120
}

phase_deps() {
  say "phase: deps"
  run colab install -s "$SESSION" -r colab/requirements-colab.txt
  run colab exec -s "$SESSION" -f colab/preflight.py --timeout 120
}

phase_push() {
  say "phase: push"
  # Uploaded, not cloned: the pilot's own code is being written in the same
  # hours it runs, and a clone would run whatever was last pushed. The price
  # is that the run has no SHA, so the manifest records one.
  run colab exec -s "$SESSION" --timeout 60 <<< \
    "import os; os.makedirs('$REMOTE/data/pilot', exist_ok=True); os.makedirs('/content/out', exist_ok=True); print('dirs ready')"
  for f in train_qlora_qwen.py rubric_eval.py; do
    run colab upload -s "$SESSION" "$f" "$REMOTE/$f"
  done
  for f in colab/pilot_math.py colab/probe_step_cost.py colab/preflight.py; do
    run colab upload -s "$SESSION" "$f" "$REMOTE/$f"
  done
  mkdir -p out
  local sha dirty
  sha="$(git rev-parse HEAD)"
  dirty="$(git status --porcelain -- . | wc -l | tr -d ' ')"
  printf '{"sha":"%s","dirty_files":%s,"session":"%s","when":"%s"}\n' \
    "$sha" "$dirty" "$SESSION" "$(date -u +%FT%TZ)" > out/run_manifest.json
  echo "  manifest: $sha (${dirty} dirty file(s) in mf-inference/peft)"
}

phase_data() {
  say "phase: data"
  for f in rubric_train.jsonl rubric_eval.jsonl; do
    [[ -f "data/pilot/$f" ]] || { echo "data/pilot/$f missing — see plan Task 6"; exit 1; }
    run colab upload -s "$SESSION" "data/pilot/$f" "$REMOTE/data/pilot/$f"
  done
}

phase_probe() {
  say "phase: probe"
  # Two regimes, each a full model load: generous timeout, and the only phase
  # whose output the next one reads.
  run colab exec -s "$SESSION" -f colab/probe_step_cost.py --timeout 3000
  run colab download -s "$SESSION" /content/out/probe.json out/probe.json
  [[ -n "${DRY_RUN:-}" ]] || python3 - <<'PY'
import json
p = json.load(open("out/probe.json"))
for name, r in p["regimes"].items():
    print(f"  {name:5} exit {r['returncode']}  {r['s_per_row']:.1f} s/row  "
          f"peak {r['peak_mib']} MiB")
PY
}

phase_train() {
  say "phase: train"
  local steps
  if [[ -n "${DRY_RUN:-}" ]]; then
    steps="<from probe>"
  else
    steps="$(python3 - "$BUDGET_S" "$BATCH_SIZE" "$GRAD_ACCUM" <<'PY'
import json, sys
sys.path.insert(0, "colab")
import pilot_math
budget, batch, accum = float(sys.argv[1]), int(sys.argv[2]), int(sys.argv[3])
ok = [r for r in json.load(open("out/probe.json"))["regimes"].values()
      if r["returncode"] == 0]
if not ok:
    sys.exit("both probe regimes failed — no measured cost, no honest limit")
print(pilot_math.compute_max_steps(budget, min(r["s_per_row"] for r in ok),
                                   batch, accum))
PY
)"
  fi
  echo "  --max-steps $steps  (budget ${BUDGET_S}s, effective batch $((BATCH_SIZE * GRAD_ACCUM)))"

  # Detached with start_new_session=True: `colab exec` returns in 30 s, and a
  # child of the kernel would die with the websocket.
  run colab exec -s "$SESSION" --timeout 60 <<PY
import subprocess, os
os.makedirs("$REMOTE/$OUT_DIR", exist_ok=True)
log = open("$REMOTE/$OUT_DIR/train.log", "w")
p = subprocess.Popen(
    ["python3", "train_qlora_qwen.py",
     "--train", "data/pilot/rubric_train.jsonl",
     "--eval", "data/pilot/rubric_eval.jsonl",
     "--out-dir", "$OUT_DIR",
     "--grad-accum", "$GRAD_ACCUM",
     "--max-steps", "$steps",
     "--save-steps", "$SAVE_STEPS"],
    cwd="$REMOTE", stdout=log, stderr=subprocess.STDOUT,
    start_new_session=True)
print("training pid", p.pid)
PY
}

phase_watch() {
  say "phase: watch"
  run colab exec -s "$SESSION" --timeout 60 <<PY
import glob, os, subprocess
d = "$REMOTE/$OUT_DIR"
log = os.path.join(d, "train.log")
print(subprocess.run(["tail", "-n", "25", log], capture_output=True,
                     text=True).stdout)
print("checkpoints:", sorted(os.path.basename(p) for p in
                             glob.glob(os.path.join(d, "checkpoint-*"))))
print("adapter written:", os.path.exists(
    os.path.join(d, "adapter_model.safetensors")))
print("still running:", bool(subprocess.run(
    ["pgrep", "-f", "train_qlora_qwen.py"], capture_output=True).stdout))
PY
}

phase_pull() {
  say "phase: pull"
  mkdir -p "$OUT_DIR"
  for f in adapter_model.safetensors adapter_config.json train_metrics.json train.log; do
    run colab download -s "$SESSION" "$REMOTE/$OUT_DIR/$f" "$OUT_DIR/$f"
  done
  # Exit code 0 is not the check. A Kaggle run once recorded COMPLETE over an
  # empty output directory because `!`'s exit code went nowhere.
  if [[ -z "${DRY_RUN:-}" && ! -s "$OUT_DIR/adapter_model.safetensors" ]]; then
    echo "adapter_model.safetensors is missing or empty — the run did not finish"
    exit 1
  fi
}

phase_eval() {
  say "phase: eval"
  # Base and adapter in one process, one session, one set of library versions.
  # rubric_eval.py already does both and prints the delta.
  run colab exec -s "$SESSION" --timeout 60 <<PY
import subprocess
p = subprocess.Popen(
    ["python3", "rubric_eval.py",
     "--data", "data/pilot/rubric_eval.jsonl",
     "--limit", "$EVAL_LIMIT",
     "--adapter", "$OUT_DIR",
     "--out", "out/pilot_eval.json"],
    cwd="$REMOTE", stdout=open("$REMOTE/out/eval.log", "w"),
    stderr=subprocess.STDOUT, start_new_session=True)
print("eval pid", p.pid)
PY
  echo "  poll with: colab exec -s $SESSION --timeout 60 <<< \"print(open('$REMOTE/out/eval.log').read()[-3000:])\""
  echo "  then:      colab download -s $SESSION $REMOTE/out/pilot_eval.json out/pilot_eval.json"
}

phase_stop() {
  say "phase: stop"
  # An unstopped session keeps a VM assigned against a quota that is the whole
  # constraint here.
  run colab stop -s "$SESSION"
}

[[ $# -gt 0 ]] || { echo "usage: $0 <phase>...  (or 'all')"; exit 2; }
[[ "${1:-}" == "all" ]] && set -- session preflight deps push data probe train watch pull eval stop

for phase in "$@"; do
  case "$phase" in
    session|preflight|deps|push|data|probe|train|watch|pull|eval|stop)
      "phase_$phase" ;;
    *) echo "unknown phase: $phase" >&2; exit 2 ;;
  esac
done
```

- [ ] **Step 4: Run test to verify it passes**

Run: `chmod +x mf-inference/peft/colab/run_pilot.sh mf-inference/peft/colab/test_driver.sh && mf-inference/peft/colab/test_driver.sh`
Expected: PASS — her satır `ok`, exit 0

- [ ] **Step 5: Commit**

```bash
git add mf-inference/peft/colab/run_pilot.sh mf-inference/peft/colab/test_driver.sh
git commit -m "feat(peft): drive the pilot in phases, because the tier can cut

colab exec returns after 30 s and a free-tier quota can end a session at any
point, so training is detached on the VM and polled, and every phase re-runs
on its own against a fresh session."
```

---

### Task 6: Pilot veri seti — 400/40

Veri backend ayakta üretilir. Prompt'lar backend'den çekilir, elle kopyalanmaz: yerel bir kopya iki taraftan biri düzenlendiği an kayar ve hiçbir şeyin göndermediği bir prompt'a ayarlanmış adapter çıkar.

**Files:**
- Create (gitignored): `mf-inference/peft/data/pilot-investment/{train,eval}.jsonl`, `data/pilot-marketing/{train,eval}.jsonl`, `data/pilot/rubric_{train,eval}.jsonl`
- Read: `mf-inference/peft/build_dataset.py`, `merge_rubric_sets.py`

**Interfaces:**
- Consumes: çalışan backend (`:8090`), bir hesabın API token'ı.
- Produces: `data/pilot/rubric_train.jsonl` (400 satır), `data/pilot/rubric_eval.jsonl` (40 satır) — Task 5'in `phase_data`'sının yüklediği dosyalar.

- [ ] **Step 1: Backend'i ayağa kaldır**

```bash
cd mf-backend && PORT=8090 go run ./cmd/server &
curl -sS -o /dev/null -w '%{http_code}\n' http://localhost:8090/healthz
```
Expected: `200`

- [ ] **Step 2: İki alanı üret**

```bash
cd mf-inference/peft
export BASE_URL=http://localhost:8090 TOKEN=<bir hesabın token'ı>
python3 build_dataset.py --domain startup-investability \
    --out-dir data/pilot-investment --n 200 --n-eval 20
python3 build_dataset.py --domain digital-marketing \
    --out-dir data/pilot-marketing --n 200 --n-eval 20
```
Expected: her biri 200 train / 20 eval; üretecin ayrık bölme assert'i (train/eval örtüşmesi %0) hata vermeden geçer. Bölmenin ayrık olması satır sayısından bağımsız bir doğruluk koşulu — küçük sette de koşar ve koşmalıdır.

- [ ] **Step 3: Birleştir — `--out-dir` atlanamaz**

```bash
python3 merge_rubric_sets.py \
    --input data/pilot-investment --input data/pilot-marketing \
    --out-dir data/pilot
```
Expected: `data/pilot/rubric_train.jsonl: 400 rows — ~3000 findings, ~27% absent`

**`--out-dir data/pilot` atlanırsa** çıktı `data/rubric_train.jsonl` olur; bu tam setin tam olarak kendisidir, pilot onu sessizce ezer ve `data/` gitignore'da olduğu için geri alınacak bir sürüm yoktur.

- [ ] **Step 4: Doğrula**

```bash
wc -l data/pilot/rubric_train.jsonl data/pilot/rubric_eval.jsonl
wc -l data/rubric_train.jsonl data/rubric_eval.jsonl
```
Expected: pilot 400 / 40; tam set **hâlâ** 1600 / 200 (ezilmedi).

- [ ] **Step 5: Backend'i kapat**

```bash
kill %1
```
Commit yok — `data/` gitignore'da. Task 5'in yazdığı `out/run_manifest.json` üretimin hangi ağaçtan koştuğunu kaydeder.

---

### Task 7: Yeni profille kimlik ve oturum

Bu, kullanıcının tek elle yapması gereken adım: `colab` CLI şu an **hiç authenticate değil** (`~/.config/colab-cli/token.json` yok) ve akış bir tarayıcı onayı ile kod yapıştırma istiyor. Yeni Colab profiliyle onaylanması gereken yer burası; onaylandıktan sonra token cache'lenir ve kalan tüm fazlar etkileşimsiz koşar.

**Files:** yok (yerel durum: `~/.config/colab-cli/`)

**Interfaces:**
- Produces: `~/.config/colab-cli/token.json` (yeni hesap), ve `pilot` adında bir T4 oturumu.

- [ ] **Step 1: Kimliği yeni profille al — kullanıcı kendi terminalinde**

```bash
colab sessions
```
Bastığı URL'yi **yeni Colab profilinin açık olduğu** tarayıcıda aç, onayla, dönen kodu terminale yapıştır.

Expected: oturum listesi (muhtemelen boş) basılır, hata yok.

**Tuzak:** onay ekranı tarayıcının varsayılan profiliyle açılır. Eski hesapla onaylanırsa hata çıkmaz — yanlış hesabın kotası harcanır. Onay ekranındaki hesap adı okunmalı.

- [ ] **Step 2: Kimliği doğrula**

```bash
colab whoami
```
Expected: aktif e-posta **yeni** profilin adresi; scope listesinde `colaboratory` var (keep-alive bunsuz 403 verir ve VM ilk dakikada bırakılır).

- [ ] **Step 3: T4 oturumu aç**

```bash
cd mf-inference/peft && colab/run_pilot.sh session
```
Expected: `colab status` bir T4 gösterir.

**Tuzak:** tanınmayan bir `--gpu` değeri sessizce A100'e düşer, o da genelde bir sonraki adımda patlar. `--gpu T4` ile `400` gelirse hesabın o hızlandırıcı için hakkı yok demektir — ücretsiz tier'da T4 beklenen cevap, ama garanti değil.

- [ ] **Step 4: Kartı ve kütüphaneleri doğrula**

```bash
colab/run_pilot.sh preflight deps
```
Expected: `sm_75 — T4 as designed`, ve ikinci `preflight` çağrısında `STALE:` satırı yok.

sm_75 değilse koşu durur. Devam kararı bilinçliyse `PILOT_ALLOW_NON_T4=1` ile geçilir, **ve üretilen her projeksiyon o kartın projeksiyonu olarak etiketlenir.**

- [ ] **Step 5: Kod ve veriyi yükle**

```bash
colab/run_pilot.sh push data
```
Expected: `out/run_manifest.json` yazıldı; `colab ls -s pilot /content/peft` beş `.py` ve `data/pilot/` altında iki `.jsonl` gösterir.

---

### Task 8: Ölçüm — Colab'ın kendi s/satır sayısı

Pilotun asıl teslimatı bu sayı. Elimizdeki 28-35 s Kaggle'ın kartından; tam koşunun kaç oturum sürdüğü ancak buradan çıkar.

**Files:**
- Create: `mf-inference/peft/out/probe.json` (gitignored)

**Interfaces:**
- Consumes: Task 7'nin oturumu, Task 4'ün script'i.
- Produces: `out/probe.json` — Task 9 `s_per_row`'u buradan okur.

- [ ] **Step 1: Probe'u koş**

```bash
cd mf-inference/peft && colab/run_pilot.sh probe
```
Expected: ~25-40 dk. İki rejim için de `exit 0 | N s/row | peak M MiB` satırı, ya da fp16 kolu için OOM.

- [ ] **Step 2: Sonucu oku ve karar ver**

Expected çıktı biçimi:
```
  4bit  exit 0  31.4 s/row  peak 6820 MiB
  fp16  exit 0  11.2 s/row  peak 14930 MiB
```

- İki rejim de 0 döndüyse ucuz olan kazanır ve `phase_train` onu otomatik seçer.
- fp16 OOM ettiyse (`exit 1`, tail'de `CUDA out of memory`) bu bir sonuçtur ve öyle kaydedilir: 4-bit kolu kalır.
- **İkisi de düştüyse ölçülmüş maliyet yoktur, dolayısıyla dürüst bir `--max-steps` de yoktur.** Eğitim başlatılmaz; `out/probe.json`'daki `tail` alanları okunur.

- [ ] **Step 3: Sayıyı not et**

`out/probe.json`'daki `s_per_row`, `peak_mib`, `full_run_hours`, `sessions` — dördü de Task 11'de `colab/README.md`'ye geçecek. Kaggle'ın 28-35 s'i ile karşılaştır: fark kartın mı yoksa kütüphane sürümlerinin mi, README bunu söylemek zorunda değil ama sayıyı yan yana koymak zorunda.

- [ ] **Step 4: Oturum hâlâ ayakta mı**

```bash
colab status -s pilot
```
Expected: IDLE. Kesildiyse Task 7 Step 3-5 tekrarlanır; probe sonucu Mac'e indiği için ölçüm kaybolmaz.

---

### Task 9: Bir saatlik eğitim koşusu

**Files:**
- Create: `mf-inference/peft/out/colab-pilot/{adapter_model.safetensors,adapter_config.json,train_metrics.json,train.log}` (gitignored)

**Interfaces:**
- Consumes: `out/probe.json`, Task 2'nin bayrakları.
- Produces: Mac'te inmiş bir adapter — kabul kriteri 2.

- [ ] **Step 1: Eğitimi başlat**

```bash
cd mf-inference/peft && colab/run_pilot.sh train
```
Expected: `--max-steps N (budget 2700s, effective batch 4)` satırı ve `training pid <n>`. Komut 30 saniyeden önce döner.

`N`'in probe'tan geldiğini gör: 31 s/satır ölçüldüyse N ≈ 21. Elle yazılmış bir adım sayısı tahmindir ve bu repoda bir tahmin zaten 12 saate mal olmuştur.

- [ ] **Step 2: Yokla**

```bash
colab/run_pilot.sh watch
```
Her ~10 dakikada bir. Expected ilerleme: `checkpoints: ['checkpoint-5', 'checkpoint-10', ...]`, log'da düşen `loss`, ve sonunda `adapter written: True` ile `still running: False`.

**Loss'a bakma sebebi eğri değil, koşunun yaşadığının kanıtı.** ~20 optimizer adımı davranış değiştirmez; buradaki hiçbir sayı bir iddia değildir.

- [ ] **Step 3: Adapter'ı indir**

```bash
colab/run_pilot.sh pull
```
Expected: dört dosya `out/colab-pilot/` altına iner; `adapter_model.safetensors` boş değil.

Çıkış kodu 0 yetmez, dosyanın varlığı ayrıca kontrol edilir — sürücü bunu kendisi yapar. Kaggle'da bir koşu boş bir çıktı dizinini COMPLETE olarak kaydetmişti.

- [ ] **Step 4: Kota pilotu kestiyse**

```bash
colab/run_pilot.sh session preflight deps push data
colab download -s pilot /content/peft/out/colab-pilot/checkpoint-XX ...  # eldeki checkpoint
# yeni oturumda: --resume ile devam
```
`--save-steps 5` tam bu yüzden var. Checkpoint yoksa ve süre bittiyse, ölçüm (Task 8) yine de elde — pilotun asıl teslimatı o.

---

### Task 10: Eval — taban ve adapter aynı oturumda

Bölmenin asıl faydası buydu: aynı kütüphane sürümleri, karşılaştırılabilir sayılar. Contrast set pilotta **yok** — 120 çift × 2 model bütçenin çok üstünde, ve contrast'ın cevapladığı soru 20 adım eğitilmiş bir adapter'da anlamsız.

**Files:**
- Create: `mf-inference/peft/out/pilot_eval.json` (gitignored)

**Interfaces:**
- Consumes: VM'deki `out/colab-pilot/`, `data/pilot/rubric_eval.jsonl`.
- Produces: `out/pilot_eval.json` — `{"base": {...}, "adapter": {...}}`.

- [ ] **Step 1: Eval'i başlat**

```bash
cd mf-inference/peft && colab/run_pilot.sh eval
```
Expected: `eval pid <n>`. Türetilmiş bütçe ~16 dakika (Kaggle'ın ~880 üretim için ~3 saatinden oranlanmış, ~12 s/üretim). **Bu sayı türetilmiş, ölçülmemiş** — koşuda doğrulanacak.

- [ ] **Step 2: Yokla**

```bash
colab exec -s pilot --timeout 60 <<< "print(open('/content/peft/out/eval.log').read()[-3000:])"
```
Expected: önce `BASE (40 rows)` bloğu, sonra `ADAPTER (40 rows)` ve `delta`.

- [ ] **Step 3: İndir**

```bash
colab download -s pilot /content/peft/out/pilot_eval.json out/pilot_eval.json
```
Expected: iki taraf da dolu bir JSON.

- [ ] **Step 4: Oku — ve hiçbir şey iddia etme**

`rubric_eval.py` sonunda bir yayın kararı basar ("do not ship this adapter" vb.). **Pilotta o karar okunmaz.** ~20 optimizer adımı davranış değiştirmez; `present_score_mae`'de görülen herhangi bir hareket gürültüdür ve öyle raporlanır.

Bu adımın kanıtladığı tek şey kabul kriteri 3: eval yolu koşuyor ve taban ile adapter için aynı oturumda sayı üretiyor.

- [ ] **Step 5: Oturumu kapat**

```bash
colab/run_pilot.sh stop
```
Expected: `colab sessions` artık `pilot`'u listelemiyor. Kapatılmayan bir oturum, bu işin tek kısıtı olan kotayı yakmaya devam eder.

---

### Task 11: Ölçülen sayıyı yaz — `colab/README.md`

Kabul kriteri 4, ve pilotun asıl teslimatı. Diğer üçü onun koşabilmiş olmasının kanıtı.

**Files:**
- Create: `mf-inference/peft/colab/README.md`
- Modify: `docs/superpowers/specs/2026-07-30-colab-pilot-design.md:4` (durum satırı)
- Modify: `mf-inference/peft/README.md` (Colab hattına bir işaret)

**Interfaces:**
- Consumes: `out/probe.json`, `out/colab-pilot/train_metrics.json`, `out/pilot_eval.json`, `out/run_manifest.json`.
- Produces: yok — belge.

- [ ] **Step 1: README'yi ölçülen sayılarla yaz**

`mf-inference/peft/colab/README.md` — `kaggle/README.md`'nin kardeşi. İskelet, `<>` içindeki her yer **koşulan sayıyla** doldurulur:

```markdown
# Colab hattı — rubrik adapter'ı

Kaggle bu eğitimi koşamıyor: ölçülen maliyet satır başına 28-35 s, tam koşu
4800 satır-geçişi, yani 38-47 saat; bir Kaggle oturumu 12 saat. 29 Temmuz
koşusu 11 saat 24 dakikada 150 adımın 45'ine geldi ve oturum duvarı iptal etti.

Bu hat ücretsiz tier Colab'ın karşılığı. Tasarım:
`docs/superpowers/specs/2026-07-30-colab-pilot-design.md`.

## Ölçülen — <TARİH>, oturum `pilot`

| | 4-bit NF4 | fp16 (`--no-4bit`) |
|---|---|---|
| s/satır | <> | <> |
| tepe bellek | <> MiB | <> MiB |
| durum | <> | <> |

Kaggle'ın T4'ünde aynı script 28-35 s/satır ölçmüştü. <Fark varsa bir cümle.>

**Tam koşu projeksiyonu** (1600 satır × 3 epoch, ucuz rejimde): **<> saat**
= <> ücretsiz-tier oturumu (3 saatlik dilim varsayımıyla).

## Pilot koşusu

| | |
|---|---|
| kart | <> |
| `--max-steps` | <> (2700 s bütçeden türetildi, elle yazılmadı) |
| effective batch | 4 (`--grad-accum 4`, batch 1) |
| set | 400 / 40, `data/pilot/` |
| çıktı | `out/colab-pilot` |
| eval | 40 satır, taban + adapter aynı oturumda, ~<> dk |

**Bu adapter hiçbir sayı iddia etmiyor.** <> optimizer adımı bir LoRA'nın
davranışını değiştirmez; eval'deki her hareket gürültüdür. Pilotun teslimatı
yukarıdaki s/satır ve ondan çıkan projeksiyon.

## Kullanım

    colab/run_pilot.sh session preflight deps push data
    colab/run_pilot.sh probe        # ~30 dk
    colab/run_pilot.sh train watch  # watch tekrar tekrar
    colab/run_pilot.sh pull eval stop

Testler, GPU'suz ve torch'suz:

    python3 -m unittest discover -s colab -p 'test_*.py'
    colab/test_driver.sh

## Tuzaklar

- **`colab exec --timeout` varsayılanı 30 saniye.** Eğitim `exec` içinde
  bloklayarak koşamaz; `start_new_session=True` ile detach edilir, yoklanır.
- **`colab run` bitince VM'i yok eder.** Eğitim için `new`/`exec`/`stop`.
- **Kimlik tarayıcı onayı ister ve varsayılan profille açılır.** Yanlış hesapla
  onaylamak hata vermez — başka bir hesabın kotasını harcar.
- **Tanınmayan bir `--gpu` değeri sessizce A100'e düşer.**
- **Colab'ın önyüklü `transformers`'ı Qwen3 için eski olabilir**; Kaggle'da bu
  `unknown architecture` diye düşüyordu. `preflight.py` model inmeden bakar.
- **Çıkış kodu 0 adapter'ın yazıldığı anlamına gelmez.** Dosya ayrıca
  kontrol edilir; Kaggle bir kez boş bir çıktı dizinini COMPLETE kaydetti.
- **Kapatılmayan oturum kotayı yakar.** `run_pilot.sh stop`.
- **Script'ler upload ile gidiyor, clone ile değil** — koşulan kodun SHA'sı bu
  yüzden `out/run_manifest.json`'da, ve `dirty_files > 0` ise o koşu bir
  commit'ten yeniden üretilemez.

## Kapsam dışı — bilerek

`--max-seq-len` kırpmak ve vaka başına kriter sayısını azaltmak. İkisi de satırı
ucuzlatır ve ikisi de modele görmediği kanıta atıf yapmayı öğretir, normal
görünen bir loss'la. Kaggle hattı (`../kaggle/`) silinmedi, duruyor.

Tam koşunun oturum zinciri burada yok. `--save-steps` ve `--resume` onun için
gereken altyapıyı bıraktı; sürücüsü ayrı bir iş ve yukarıdaki projeksiyonla
planlanır.
```

- [ ] **Step 2: Tasarım belgesinin durumunu güncelle**

`docs/superpowers/specs/2026-07-30-colab-pilot-design.md:4`:

```markdown
**Durum:** pilot koşuldu — ölçülen sayılar `mf-inference/peft/colab/README.md`
```

- [ ] **Step 3: `peft/README.md`'ye işaret koy**

Kaggle'dan bahseden bölüme bir satır: eğitim artık Colab'da, `colab/README.md` ölçülen maliyeti ve tam koşu projeksiyonunu taşıyor, `kaggle/` duruyor.

- [ ] **Step 4: Kabul kriterlerini tek tek doğrula**

```bash
python3 -c "import json; d=json.load(open('mf-inference/peft/out/probe.json')); print(json.dumps(d['regimes'], indent=2))"
ls -l mf-inference/peft/out/colab-pilot/adapter_model.safetensors
python3 -c "import json; d=json.load(open('mf-inference/peft/out/pilot_eval.json')); print(list(d))"
grep -c '<>' mf-inference/peft/colab/README.md
```
Expected:
1. iki rejim için de `s_per_row` ve `peak_mib` (fp16 OOM ise `returncode != 0` ve `tail`'de sebep) — kriter 1
2. dosya var ve boyutu > 0 — kriter 2
3. çıktıda hem `base` hem `adapter` — kriter 3
4. `grep -c '<>'` → `0`, yani README'de doldurulmamış yer kalmadı — kriter 4

- [ ] **Step 5: Commit**

```bash
git add mf-inference/peft/colab/README.md mf-inference/peft/README.md \
        docs/superpowers/specs/2026-07-30-colab-pilot-design.md
git commit -m "docs(peft): record what a Colab hour actually measured

The pilot's deliverable is not the adapter — twenty optimizer steps move
nothing and the README says so. It is Colab's own s/row and the full-run
projection that follows from it, which is what the session chain could not be
planned without."
```

---

## Self-Review

**Spec coverage.** Tasarımın her bölümü bir task'a düşüyor: bileşen tablosu → Task 1/3/4/5 (`README.md` Task 11); `train_qlora_qwen.py`'nin dört bayrağı → Task 2; pilot konfigürasyonu (400/40, grad-accum 4, max-seq-len 2560, save-steps 5, `out/colab-pilot`) → Task 5 sabitleri + Task 6; veri üretimi ve `--out-dir` tuzağı → Task 6; eval (40 satır, contrast yok, aynı oturum) → Task 10; akış diyagramı → Task 5'in fazları; dört kabul kriteri → Task 11 Step 4; tuzak listesi → Task 11'in README'si.

**Bilinçli sapmalar, ikisi de gerekçeli:** git clone yerine `colab upload` (manifest ile telafi), ve sm_75 assert'ine `PILOT_ALLOW_NON_T4` kaçışı — tasarım kartın garanti olmadığını zaten yazıyordu, ama ne yapılacağını yazmıyordu.

**Tip tutarlılığı.** `pilot_math` imzaları Task 1'de tanımlanıp Task 4 (`seconds_per_row`, `project_full_run_hours`, `sessions_needed`) ve Task 5 (`compute_max_steps`) tarafından aynı adlarla çağrılıyor. `out/probe.json`'un şeması Task 4'te yazılıp Task 5'in `phase_train`'inde aynı anahtarlarla (`regimes`, `returncode`, `s_per_row`) okunuyor. Bayrak adları Task 2'nin testi ile Task 4/5'in komut satırları arasında birebir.
