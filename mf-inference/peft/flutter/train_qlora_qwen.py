#!/usr/bin/env python3
"""QLoRA fine-tune of Qwen3-4B for the Flutter screen generator (v8).

The sibling of ../train_qlora.py, which trains Gemma-2 for the rubric schema.
This is a separate file rather than a `--base-model` flag on that one because
three of its choices are Gemma-specific in ways that fail *silently* when
pointed at another architecture: eager attention (for Gemma-2's logit
soft-capping, which Qwen3 does not have), a chat template with no system role,
and an embedding left un-adapted because Gemma's 256k vocabulary makes it a
fifth of the model. Passing --base-model Qwen/... to that script produces a run
that completes, reports a plausible loss, and has trained the wrong function.

What is the same, and for the same reasons
------------------------------------------
* **fp16, not bf16.** Kaggle's T4 is Turing (sm_75) — the same compute
  capability as the GTX 1660 Ti this project serves from, and equally without
  bf16. GradScaler is load-bearing, not boilerplate.

* **Completion-only loss.** v8's prompt is ~1200 tokens of evidence and brief
  against ~600 tokens of answer. Training on both would spend most of the
  gradient teaching the model to reproduce evidence blocks — the one thing the
  backend generates and the model must never write.

What differs
------------
* **sdpa attention.** Qwen3 has no soft-capping, so the fused kernel computes
  the right function and is faster. flash-attention 2 is not an option: it does
  not support sm_75.

* **A real system role.** Qwen3's template has one, so the system prompt is
  passed as a system message rather than folded into the user turn. This must
  match what the inference server sends — mlc_llm serve passes an OpenAI-style
  system message straight through to the template, so it does.

* **max-seq-len 2560, measured.** v8's longest row is ~2130 tokens; 2048 clips
  one row and 1536 clips 15% of them. Clipping happens from the left here, so a
  clipped row loses evidence rather than answer — but a row whose answer is
  gone teaches nothing, which the guard below turns into an error rather than a
  quiet drop in quality.

Usage (on the Kaggle T4):
    python3 train_qlora_qwen.py \
        --train data/flutter_screens_train_v8.jsonl \
        --eval  data/flutter_screens_eval_v8.jsonl \
        --out-dir out/flutter-v8
"""

from __future__ import annotations

import argparse
import json
import os
import sys

try:
    import torch
    from datasets import Dataset
    from peft import LoraConfig, get_peft_model, prepare_model_for_kbit_training
    from transformers import (AutoModelForCausalLM, AutoTokenizer,
                              BitsAndBytesConfig, DataCollatorForSeq2Seq,
                              Trainer, TrainingArguments)
except ImportError as exc:  # pragma: no cover - environment guard
    sys.exit(f"missing dependency: {exc}\npip install -r ../requirements.txt")

IGNORE_INDEX = -100


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser()
    ap.add_argument("--train", default="data/flutter_screens_train_v8.jsonl")
    ap.add_argument("--eval", default="data/flutter_screens_eval_v8.jsonl")
    # The non-thinking instruct release. Qwen/Qwen3-4B is the hybrid-reasoning
    # model and emits <think> blocks its template opens by default; for a
    # single-turn code task those are wasted tokens at best and leak into the
    # ```dart fence at worst. enable_thinking=False is passed below anyway, so
    # pointing this at the hybrid model still works.
    ap.add_argument("--base-model", default="Qwen/Qwen3-4B-Instruct-2507")
    ap.add_argument("--out-dir", default="out/flutter-v8")
    ap.add_argument("--rank", type=int, default=16)
    ap.add_argument("--alpha", type=int, default=0,
                    help="defaults to 2*rank, the usual pairing")
    ap.add_argument("--dropout", type=float, default=0.05)
    ap.add_argument("--max-seq-len", type=int, default=2560)
    # 115 training rows. At grad-accum 16 that is 7 optimizer steps per epoch,
    # and three epochs would be 21 steps total — too few for a LoRA to converge.
    # 8 and 6 give ~86 steps, which is the smallest number that trains without
    # inviting the overfitting a 115-row set makes easy.
    ap.add_argument("--epochs", type=float, default=6.0)
    ap.add_argument("--grad-accum", type=int, default=8)
    ap.add_argument("--batch-size", type=int, default=1)
    ap.add_argument("--lr", type=float, default=1e-4)
    ap.add_argument("--target-mlp", action="store_true",
                    help="also adapt gate/up/down_proj. More capacity, and more "
                         "to overfit with on a set this small — off by default")
    ap.add_argument("--seed", type=int, default=20260727)
    return ap.parse_args()


def load_jsonl(path: str) -> list[dict]:
    if not os.path.exists(path):
        sys.exit(f"{path} not found — run build_flutter_dataset_v8.py first")
    with open(path, encoding="utf-8") as fh:
        return [json.loads(line) for line in fh if line.strip()]


def build_tokenize_fn(tokenizer, max_len: int):
    """Turn one chat row into ids whose loss covers the answer only."""

    def render_prompt(system: str, user: str) -> str:
        msgs = [{"role": "system", "content": system},
                {"role": "user", "content": user}]
        try:
            # Hybrid Qwen3 templates open a <think> block unless told not to;
            # the -Instruct- releases do not accept the argument at all.
            return tokenizer.apply_chat_template(
                msgs, tokenize=False, add_generation_prompt=True,
                enable_thinking=False)
        except TypeError:
            return tokenizer.apply_chat_template(
                msgs, tokenize=False, add_generation_prompt=True)

    # Qwen3 closes every turn with <|im_end|>. Including it in the target is what
    # teaches the model to stop; without it generation runs to the token limit
    # and truncated widgets — this model's first failure mode — come back by a
    # different route.
    stop = "<|im_end|>"

    def fn(example: dict) -> dict:
        msgs = example["messages"]
        system = next(m["content"] for m in msgs if m["role"] == "system")
        user = next(m["content"] for m in msgs if m["role"] == "user")
        target = next(m["content"] for m in msgs if m["role"] == "assistant")

        prompt_ids = tokenizer(render_prompt(system, user),
                               add_special_tokens=False)["input_ids"]
        answer_ids = tokenizer(target + stop, add_special_tokens=False)["input_ids"]

        input_ids = prompt_ids + answer_ids
        labels = [IGNORE_INDEX] * len(prompt_ids) + answer_ids[:]

        if len(input_ids) > max_len:
            # From the left: the answer must survive. What is lost is the front
            # of the evidence block, which costs the row some grounding but
            # leaves it teaching the right output.
            overflow = len(input_ids) - max_len
            input_ids = input_ids[overflow:]
            labels = labels[overflow:]

        return {"input_ids": input_ids, "labels": labels,
                "attention_mask": [1] * len(input_ids)}

    return fn


def main() -> None:
    args = parse_args()
    if args.alpha == 0:
        args.alpha = 2 * args.rank

    if not torch.cuda.is_available():
        sys.exit("no CUDA device visible; enable the GPU accelerator")
    name = torch.cuda.get_device_name(0)
    cap = torch.cuda.get_device_capability(0)
    total_gb = torch.cuda.get_device_properties(0).total_memory / 1024**3
    print(f"device: {name}  sm_{cap[0]}{cap[1]}  {total_gb:.1f} GB")

    supports_bf16 = torch.cuda.is_bf16_supported()
    if not supports_bf16:
        print("bf16 unsupported on this card; training in fp16 with loss scaling")

    torch.manual_seed(args.seed)

    tokenizer = AutoTokenizer.from_pretrained(args.base_model)
    tokenizer.padding_side = "right"
    if tokenizer.pad_token is None:
        # Qwen ships <|endoftext|> as pad; if a variant does not, reusing eos is
        # safe because the collator masks padded label positions anyway.
        tokenizer.pad_token = tokenizer.eos_token

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
    model = prepare_model_for_kbit_training(model, use_gradient_checkpointing=True)

    targets = ["q_proj", "k_proj", "v_proj", "o_proj"]
    if args.target_mlp:
        targets += ["gate_proj", "up_proj", "down_proj"]

    model = get_peft_model(model, LoraConfig(
        r=args.rank, lora_alpha=args.alpha, lora_dropout=args.dropout,
        bias="none", task_type="CAUSAL_LM", target_modules=targets))
    trainable = sum(p.numel() for p in model.parameters() if p.requires_grad)
    total = sum(p.numel() for p in model.parameters())
    print(f"trainable: {trainable:,} of {total:,} ({100*trainable/total:.3f}%)")

    tok_fn = build_tokenize_fn(tokenizer, args.max_seq_len)
    train_ds = Dataset.from_list(load_jsonl(args.train)).map(
        tok_fn, remove_columns=["messages"], desc="tokenising train")
    eval_ds = Dataset.from_list(load_jsonl(args.eval)).map(
        tok_fn, remove_columns=["messages"], desc="tokenising eval")

    empty = sum(1 for r in train_ds if all(v == IGNORE_INDEX for v in r["labels"]))
    if empty:
        sys.exit(f"{empty} training rows have no unmasked target — "
                 f"raise --max-seq-len above {args.max_seq_len}")

    lengths = [len(r["input_ids"]) for r in train_ds]
    clipped = sum(1 for n in lengths if n >= args.max_seq_len)
    print(f"sequence length: max {max(lengths)}, mean {sum(lengths)//len(lengths)}"
          f"  ({clipped} row(s) clipped)")

    trainer = Trainer(
        model=model,
        train_dataset=train_ds,
        eval_dataset=eval_ds,
        args=TrainingArguments(
            output_dir=args.out_dir,
            num_train_epochs=args.epochs,
            per_device_train_batch_size=args.batch_size,
            gradient_accumulation_steps=args.grad_accum,
            per_device_eval_batch_size=args.batch_size,
            eval_accumulation_steps=1,
            learning_rate=args.lr,
            lr_scheduler_type="cosine",
            warmup_ratio=0.05,
            logging_steps=5,
            eval_strategy="epoch",
            save_strategy="epoch",
            save_total_limit=2,
            load_best_model_at_end=True,
            metric_for_best_model="eval_loss",
            fp16=not supports_bf16,
            bf16=supports_bf16,
            gradient_checkpointing=True,
            gradient_checkpointing_kwargs={"use_reentrant": False},
            optim="paged_adamw_8bit",
            max_grad_norm=0.3,
            report_to=[],
            seed=args.seed,
        ),
        data_collator=DataCollatorForSeq2Seq(
            tokenizer, padding=True, label_pad_token_id=IGNORE_INDEX),
    )

    steps = max(1, int(len(train_ds) * args.epochs //
                       (args.batch_size * args.grad_accum)))
    print(f"\neffective batch: {args.batch_size * args.grad_accum} rows/step"
          f"  ~{steps} optimizer steps")
    trainer.train()

    model.save_pretrained(args.out_dir)
    tokenizer.save_pretrained(args.out_dir)

    metrics = trainer.evaluate()
    with open(os.path.join(args.out_dir, "train_metrics.json"), "w") as fh:
        json.dump({"eval": metrics, "trainable_params": trainable,
                   "rank": args.rank, "alpha": args.alpha,
                   "base_model": args.base_model,
                   "max_seq_len": args.max_seq_len,
                   "target_modules": targets}, fh, indent=2)

    print(f"\nadapter written to {args.out_dir}")
    print(f"final eval loss: {metrics.get('eval_loss'):.4f}")
    print("\nThe loss is not the result. v8 exists to make the model follow "
          "evidence it has never seen before, and only flutter_eval.py measures "
          "that — run it against the held-out migrations before shipping.")


if __name__ == "__main__":
    main()
