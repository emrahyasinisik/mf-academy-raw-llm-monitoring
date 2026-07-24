#!/usr/bin/env python3
"""QLoRA fine-tune of Gemma-2-2b for the rubric-analysis schema.

Runs on the 6 GB GTX 1660 Ti this project's inference host is built around.
Full fine-tuning of a 2.6B model needs roughly 40 GB once optimiser state is
counted, so the base weights are frozen and quantised to 4-bit and only a
~6M-parameter LoRA is trained on top: about 0.24% of the model.

Hardware-specific choices, all of which matter here and would not elsewhere
-----------------------------------------------------------------------------
* **fp16, not bf16.** Turing (sm_75) has no bf16. Gemma-2 was trained in bf16,
  which has the same exponent range as fp32; fp16 does not, so activations that
  were comfortable in training can overflow. GradScaler is therefore not
  optional here the way it often is elsewhere.

* **eager attention.** Gemma-2 applies logit soft-capping in attention, which
  the fused SDPA and flash-attention kernels do not implement. They do not
  error — they silently compute something else, and the model trains on the
  wrong function. This is the single easiest way to waste a run.

* **No LoRA on the embedding.** Gemma's vocabulary is 256k against a hidden
  size of 2304, making the embedding matrix roughly a fifth of the whole model.
  Adapting it would spend most of the memory budget that makes this possible.

* **Completion-only loss.** The prompt is thousands of tokens and identical in
  every example; the answer is a few hundred. Training on both would spend most
  of the gradient signal teaching the model to reproduce its own instructions,
  and the schema discipline this exists to teach would be a rounding error.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from dataclasses import dataclass

try:
    import torch
    from datasets import Dataset
    from peft import LoraConfig, get_peft_model, prepare_model_for_kbit_training
    from transformers import (
        AutoModelForCausalLM,
        AutoTokenizer,
        BitsAndBytesConfig,
        DataCollatorForSeq2Seq,
        Trainer,
        TrainingArguments,
    )
except ImportError as e:  # pragma: no cover - environment problem, not logic
    sys.exit(f"missing dependency: {e}\ninstall with: pip install -r requirements.txt")


# Masked positions. -100 is what PyTorch's cross-entropy treats as "no target",
# and it is how the prompt is excluded from the loss.
IGNORE_INDEX = -100


@dataclass
class Args:
    train: str
    eval: str
    base_model: str
    out_dir: str
    rank: int
    alpha: int
    dropout: float
    max_seq_len: int
    epochs: float
    lr: float
    batch_size: int
    grad_accum: int
    seed: int


def parse_args() -> Args:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--train", default="data/train.jsonl")
    ap.add_argument("--eval", default="data/eval.jsonl")
    ap.add_argument("--base-model", default="google/gemma-2-2b-it")
    ap.add_argument("--out-dir", default="out/adapter")
    ap.add_argument("--rank", type=int, default=16)
    ap.add_argument("--alpha", type=int, default=0,
                    help="defaults to 2*rank, which keeps the alpha/rank scale "
                         "constant when rank changes")
    ap.add_argument("--dropout", type=float, default=0.05)
    # The prompt alone is long: the rubric lists nine criteria with guidance,
    # and Turkish tokenises poorly under a mostly-English vocabulary. Anything
    # much below this silently truncates the answer out of the example, so the
    # model would be trained on inputs whose targets were cut off.
    ap.add_argument("--max-seq-len", type=int, default=2560)
    ap.add_argument("--epochs", type=float, default=3.0)
    ap.add_argument("--lr", type=float, default=2e-4)
    ap.add_argument("--batch-size", type=int, default=1)
    ap.add_argument("--grad-accum", type=int, default=16)
    ap.add_argument("--seed", type=int, default=20260724)
    a = ap.parse_args()
    return Args(a.train, a.eval, a.base_model, a.out_dir, a.rank,
                a.alpha or 2 * a.rank, a.dropout, a.max_seq_len, a.epochs,
                a.lr, a.batch_size, a.grad_accum, a.seed)


def load_jsonl(path: str) -> list[dict]:
    if not os.path.exists(path):
        sys.exit(f"{path} not found — run build_dataset.py first")
    with open(path, encoding="utf-8") as fh:
        return [json.loads(line) for line in fh if line.strip()]


def build_tokenize_fn(tokenizer, max_len: int):
    """Return a function turning one chat example into masked training ids.

    Gemma-2 has no system role in its chat template, so the system content is
    prepended to the user turn — which is exactly what the inference server does
    with an OpenAI-style system message, so the two agree. Getting this wrong is
    invisible: training succeeds and the adapter is simply tuned for a prompt
    layout that never occurs.
    """

    def fn(example: dict) -> dict:
        msgs = example["messages"]
        system = next(m["content"] for m in msgs if m["role"] == "system")
        user = next(m["content"] for m in msgs if m["role"] == "user")
        target = next(m["content"] for m in msgs if m["role"] == "assistant")

        prompt = tokenizer.apply_chat_template(
            [{"role": "user", "content": f"{system}\n\n{user}"}],
            tokenize=False,
            add_generation_prompt=True,
        )

        prompt_ids = tokenizer(prompt, add_special_tokens=False)["input_ids"]
        # The end-of-turn token is part of the target: without it the model
        # never learns to stop, and generation runs to the token limit every
        # time — which is the truncation failure this project already paid for
        # once, arriving by a different route.
        answer_ids = tokenizer(target + "<end_of_turn>",
                               add_special_tokens=False)["input_ids"]

        input_ids = prompt_ids + answer_ids
        labels = [IGNORE_INDEX] * len(prompt_ids) + answer_ids[:]

        if len(input_ids) > max_len:
            # Truncating from the left keeps the answer intact and drops the
            # oldest part of the case. Cutting from the right would remove the
            # target and leave an example that teaches nothing while still
            # contributing a loss.
            overflow = len(input_ids) - max_len
            input_ids = input_ids[overflow:]
            labels = labels[overflow:]

        return {"input_ids": input_ids, "labels": labels,
                "attention_mask": [1] * len(input_ids)}

    return fn


def main() -> None:
    args = parse_args()

    if not torch.cuda.is_available():
        sys.exit("no CUDA device visible; this script is meant for the GPU host")
    name = torch.cuda.get_device_name(0)
    cap = torch.cuda.get_device_capability(0)
    total_gb = torch.cuda.get_device_properties(0).total_memory / 1024**3
    print(f"device: {name}  sm_{cap[0]}{cap[1]}  {total_gb:.1f} GB")

    supports_bf16 = torch.cuda.is_bf16_supported()
    if not supports_bf16:
        print("bf16 unsupported on this card; training in fp16 with loss scaling")

    torch.manual_seed(args.seed)

    tokenizer = AutoTokenizer.from_pretrained(args.base_model)
    # Right padding. Gemma is trained with it, and a left-padded batch would
    # shift every position relative to what the model saw in pretraining.
    tokenizer.padding_side = "right"

    quant = BitsAndBytesConfig(
        load_in_4bit=True,
        # NF4 places its quantisation levels on the quantiles of a normal
        # distribution, which is roughly how neural network weights are
        # distributed — measurably better than plain int4 at the same width.
        bnb_4bit_quant_type="nf4",
        # Quantises the quantisation constants too, saving ~0.37 bits per
        # parameter. On a 6 GB card that is real headroom, not bookkeeping.
        bnb_4bit_use_double_quant=True,
        bnb_4bit_compute_dtype=torch.float16,
    )

    model = AutoModelForCausalLM.from_pretrained(
        args.base_model,
        quantization_config=quant,
        device_map={"": 0},
        # See the module docstring: the fused kernels do not implement Gemma-2's
        # logit soft-capping and fail silently rather than loudly.
        attn_implementation="eager",
        torch_dtype=torch.float16,
    )
    model.config.use_cache = False
    model = prepare_model_for_kbit_training(model, use_gradient_checkpointing=True)

    lora = LoraConfig(
        r=args.rank,
        lora_alpha=args.alpha,
        lora_dropout=args.dropout,
        bias="none",
        task_type="CAUSAL_LM",
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj"],
    )
    model = get_peft_model(model, lora)
    trainable = sum(p.numel() for p in model.parameters() if p.requires_grad)
    total = sum(p.numel() for p in model.parameters())
    print(f"trainable: {trainable:,} of {total:,} ({100*trainable/total:.3f}%)")

    tok_fn = build_tokenize_fn(tokenizer, args.max_seq_len)
    train_ds = Dataset.from_list(load_jsonl(args.train)).map(
        tok_fn, remove_columns=["messages"], desc="tokenising train")
    eval_ds = Dataset.from_list(load_jsonl(args.eval)).map(
        tok_fn, remove_columns=["messages"], desc="tokenising eval")

    # A quick sanity check that is cheap and catches the worst silent failure:
    # an example whose answer was truncated away has no supervised positions at
    # all and teaches nothing.
    empty = sum(1 for r in train_ds if all(v == IGNORE_INDEX for v in r["labels"]))
    if empty:
        sys.exit(f"{empty} training examples have no unmasked target — "
                 f"raise --max-seq-len above {args.max_seq_len}")

    lengths = [len(r["input_ids"]) for r in train_ds]
    print(f"sequence length: max {max(lengths)}, mean {sum(lengths)//len(lengths)}")

    trainer = Trainer(
        model=model,
        train_dataset=train_ds,
        eval_dataset=eval_ds,
        args=TrainingArguments(
            output_dir=args.out_dir,
            num_train_epochs=args.epochs,
            per_device_train_batch_size=args.batch_size,
            gradient_accumulation_steps=args.grad_accum,
            learning_rate=args.lr,
            lr_scheduler_type="cosine",
            warmup_ratio=0.03,
            logging_steps=10,
            eval_strategy="epoch",
            save_strategy="epoch",
            # Two checkpoints of a 13 MB adapter cost nothing, and keeping the
            # best by eval loss means a run that overfits in its last epoch
            # still yields something usable.
            save_total_limit=2,
            load_best_model_at_end=True,
            metric_for_best_model="eval_loss",
            fp16=not supports_bf16,
            bf16=supports_bf16,
            gradient_checkpointing=True,
            # Required with PEFT: without it the checkpointed segments have no
            # input requiring grad and the backward pass silently computes
            # nothing for the adapter.
            gradient_checkpointing_kwargs={"use_reentrant": False},
            optim="paged_adamw_8bit",
            max_grad_norm=0.3,
            report_to=[],
            seed=args.seed,
        ),
        data_collator=DataCollatorForSeq2Seq(
            tokenizer, padding=True, label_pad_token_id=IGNORE_INDEX),
    )

    print(f"\neffective batch: {args.batch_size * args.grad_accum} examples/step")
    trainer.train()

    model.save_pretrained(args.out_dir)
    tokenizer.save_pretrained(args.out_dir)

    metrics = trainer.evaluate()
    with open(os.path.join(args.out_dir, "train_metrics.json"), "w") as fh:
        json.dump({"eval": metrics, "trainable_params": trainable,
                   "rank": args.rank, "alpha": args.alpha,
                   "base_model": args.base_model}, fh, indent=2)

    print(f"\nadapter written to {args.out_dir}")
    print(f"final eval loss: {metrics.get('eval_loss'):.4f}")
    print("\nA lower loss is not the result. The result is what the schema and "
          "absent-evidence rates do on the real deck — merge, compile, then run "
          "compare.py.")


if __name__ == "__main__":
    main()
