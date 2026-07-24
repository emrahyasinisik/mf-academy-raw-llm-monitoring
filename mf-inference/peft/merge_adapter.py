#!/usr/bin/env python3
"""Merge a trained LoRA into the base weights and write a plain HF model.

Why a merge is needed at all
----------------------------
MLC compiles a model ahead of time: the graph is lowered to machine code for a
specific GPU architecture and the weights are quantised and packed offline.
There is no runtime slot to plug a LoRA into — mlc-ai/mlc-llm#2625 has been open
since 2024, and the runtime-support PR #3281 was closed unmerged in March 2026.

So "loading an adapter" here means folding B·A back into W and rebuilding.
That is why the panel's one-click activation takes minutes: this script and the
compile that follows it are what the click actually queues.

The double quantisation cost
----------------------------
The adapter was trained against a 4-bit NF4 base. Merging requires the base in
fp16, so the weights are dequantised, B·A is added, and the result is quantised
again to q4f16_1 by the compile step. Two lossy conversions, and the LoRA was
fitted to the error profile of the first one.

The effect is real and not measurable from the loss curve, which is why the base
model is loaded here in fp16 rather than 4-bit: merging into the quantised copy
would compound the error a third time for no benefit. It is also why compare.py
exists — the only honest check is what the merged build does on the real case.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys

try:
    import torch
    from peft import PeftModel
    from transformers import AutoModelForCausalLM, AutoTokenizer
except ImportError as e:  # pragma: no cover
    sys.exit(f"missing dependency: {e}\ninstall with: pip install -r requirements.txt")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--adapter", default="out/adapter",
                    help="directory written by train_qlora.py")
    ap.add_argument("--base-model", default="google/gemma-2-2b-it")
    # Written under mf-inference/models/ because that directory is bind-mounted
    # into the mlc container, and the quantisation step runs inside it. Anywhere
    # else and the container cannot see the file it has to convert.
    ap.add_argument("--out", default="../models/merged-fp16",
                    help="destination for the merged fp16 model; must sit under "
                         "mf-inference/models/ so the container can read it")
    ap.add_argument("--device", default="cpu",
                    help="cpu keeps the 6 GB card free; the merge is arithmetic, "
                         "not training, so it is only slower, not worse")
    args = ap.parse_args()

    if not os.path.isdir(args.adapter):
        sys.exit(f"{args.adapter} not found — run train_qlora.py first")

    cfg_path = os.path.join(args.adapter, "adapter_config.json")
    if not os.path.exists(cfg_path):
        sys.exit(f"{cfg_path} missing; {args.adapter} is not a PEFT adapter directory")
    with open(cfg_path) as fh:
        adapter_cfg = json.load(fh)

    trained_against = adapter_cfg.get("base_model_name_or_path")
    if trained_against and trained_against != args.base_model:
        # Merging into a different base than the adapter was fitted to produces
        # a model that loads cleanly and answers badly, with nothing in any log
        # to say why. Worth refusing rather than warning.
        sys.exit(f"adapter was trained against {trained_against!r} but --base-model "
                 f"is {args.base_model!r}; refusing to merge mismatched weights")

    print(f"base:    {args.base_model}")
    print(f"adapter: {args.adapter}  (r={adapter_cfg.get('r')}, "
          f"alpha={adapter_cfg.get('lora_alpha')}, "
          f"targets={adapter_cfg.get('target_modules')})")

    # fp16 on CPU is supported for storage and for the add; the multiply that
    # merge_and_unload performs is done in the adapter's dtype. Loading in fp32
    # would double the RAM for no accuracy that survives the requantisation.
    print("\nloading base in fp16 (not 4-bit — see the module docstring)")
    model = AutoModelForCausalLM.from_pretrained(
        args.base_model,
        torch_dtype=torch.float16,
        device_map={"": args.device},
        attn_implementation="eager",
    )

    print("applying adapter")
    model = PeftModel.from_pretrained(model, args.adapter, device_map={"": args.device})

    print("merging")
    model = model.merge_and_unload()

    if os.path.isdir(args.out):
        shutil.rmtree(args.out)
    os.makedirs(args.out, exist_ok=True)

    print(f"writing {args.out}")
    # safe_serialization writes .safetensors, which is what mlc_llm
    # convert_weight expects to find; a .bin-only directory makes it fail with
    # a confusing "no weights found".
    model.save_pretrained(args.out, safe_serialization=True)

    # The tokenizer must travel with the weights. mlc_llm gen_config reads it
    # from the same directory, and a merged model without one produces a build
    # that serves gibberish rather than failing.
    tokenizer = AutoTokenizer.from_pretrained(args.base_model)
    tokenizer.save_pretrained(args.out)

    # Record what produced this, so a model directory found six weeks later can
    # still be traced back to its adapter and its training run.
    provenance = {
        "base_model": args.base_model,
        "adapter_dir": os.path.abspath(args.adapter),
        "lora_rank": adapter_cfg.get("r"),
        "lora_alpha": adapter_cfg.get("lora_alpha"),
        "target_modules": adapter_cfg.get("target_modules"),
    }
    metrics_path = os.path.join(args.adapter, "train_metrics.json")
    if os.path.exists(metrics_path):
        with open(metrics_path) as fh:
            provenance["training"] = json.load(fh)
    with open(os.path.join(args.out, "mf_provenance.json"), "w") as fh:
        json.dump(provenance, fh, indent=2)

    size_gb = sum(
        os.path.getsize(os.path.join(args.out, f)) for f in os.listdir(args.out)
    ) / 1024**3
    print(f"\nmerged model written: {size_gb:.2f} GB")
    print("next: build_mlc.sh, which quantises this to q4f16_1 and compiles it")


if __name__ == "__main__":
    main()
