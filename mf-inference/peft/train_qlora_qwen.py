#!/usr/bin/env python3
"""QLoRA fine-tune of Qwen3 for this project's chat-shaped training sets.

The architecture sibling of train_qlora.py, which trains Gemma-2. Split by
*base model*, not by task: both files read the same `messages` rows, so this one
trains the rubric schema, the investment persona or anything else emitted in
that shape. What cannot be shared is the handful of decisions below, three of
which are Gemma-specific in ways that fail *silently* when pointed elsewhere —
eager attention (for Gemma-2's logit soft-capping, which Qwen3 does not have), a
chat template with no system role, and an embedding left un-adapted because
Gemma's 256k vocabulary makes it a fifth of the model. Passing
--base-model Qwen/... to that script produces a run that completes, reports a
plausible loss, and has trained the wrong function.

What is the same as the Gemma line, and for the same reasons
------------------------------------------------------------
* **fp16, not bf16.** Kaggle's T4 is Turing (sm_75) — the same compute
  capability as the GTX 1660 Ti this project serves from, and equally without
  bf16. GradScaler is load-bearing, not boilerplate.

* **Completion-only loss.** The rubric prompt is a long case plus nine criteria
  against a few hundred tokens of JSON answer. Training on both would spend most
  of the gradient teaching the model to reproduce its own instructions, and the
  schema discipline this exists to teach would be a rounding error.

What differs from the Gemma line
--------------------------------
* **sdpa attention.** Qwen3 has no soft-capping, so the fused kernel computes
  the right function and is faster. flash-attention 2 is not an option: it does
  not support sm_75.

* **A real system role.** Qwen3's template has one, so the system prompt is
  passed as a system message rather than folded into the user turn. This must
  match what the inference server sends — mlc_llm serve passes an OpenAI-style
  system message straight through to the template, so it does.

On the hyperparameters below
----------------------------
They are set for the rubric line's ~1600 rows and NOT inherited from the Flutter
v8 run, whose 6 epochs at grad-accum 8 were chosen for 115 rows. Applying those
here would be ~1200 optimizer steps over a set thirteen times larger: the run
finishes, the loss looks fine, and the adapter has memorised the fragment bank
the generator assembles cases from. Whenever the training set changes size,
these two numbers are the ones to re-derive, and `--max-seq-len` is the one to
re-measure — measure_tokens.py exists for exactly that and the number is not
transferable between tokenisers.

Usage (on the Kaggle T4):
    python3 train_qlora_qwen.py \
        --train data/rubric_train.jsonl \
        --eval  data/rubric_eval.jsonl \
        --out-dir out/rubric-v1
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time

try:
    import torch
    from datasets import Dataset
    from peft import LoraConfig, get_peft_model, prepare_model_for_kbit_training
    from transformers import (AutoModelForCausalLM, AutoTokenizer,
                              BitsAndBytesConfig, DataCollatorForSeq2Seq,
                              Trainer, TrainerCallback, TrainingArguments)
except ImportError as exc:  # pragma: no cover - environment guard
    sys.exit(f"missing dependency: {exc}\npip install -r ../requirements.txt")

IGNORE_INDEX = -100


class DeadlineStop(TrainerCallback):
    """Stop the training loop after a wall-clock budget, cleanly.

    `should_training_stop` is the same lever Trainer's own early stopping pulls,
    which matters more than it sounds: the loop unwinds normally and control
    returns to the caller, so the adapter gets saved. Killing the process at the
    same moment — which is what a session wall does — loses the run entirely.

    The check is on step end rather than on a timer, so the deadline is honoured
    to within one optimizer step. At the measured ~30 s/row that granularity is
    minutes, and the caller is expected to leave room for it (plus the final
    save and eval) rather than budget the session down to the last second.
    """

    def __init__(self, minutes: float) -> None:
        self.limit_s = minutes * 60.0
        self.started = 0.0
        self.fired = False

    def on_train_begin(self, args, state, control, **kw):
        self.started = time.monotonic()

    def on_step_end(self, args, state, control, **kw):
        if self.fired:
            return control
        elapsed = time.monotonic() - self.started
        if elapsed >= self.limit_s:
            self.fired = True
            control.should_training_stop = True
            print(f"\n[deadline] {elapsed/60:.1f} dk doldu (sinir "
                  f"{self.limit_s/60:.0f} dk) — adim {state.global_step}'de "
                  f"duruluyor. Agirliklar yazilacak.", flush=True)
        return control


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser()
    ap.add_argument("--train", default="data/rubric_train.jsonl")
    ap.add_argument("--eval", default="data/rubric_eval.jsonl")
    # The non-thinking instruct release. Qwen/Qwen3-4B is the hybrid-reasoning
    # model and emits <think> blocks its template opens by default; for a
    # single-turn code task those are wasted tokens at best and leak into the
    # ```dart fence at worst. enable_thinking=False is passed below anyway, so
    # pointing this at the hybrid model still works.
    ap.add_argument("--base-model", default="Qwen/Qwen3-4B-Instruct-2507")
    ap.add_argument("--out-dir", default="out/rubric-v1")
    ap.add_argument("--rank", type=int, default=16)
    ap.add_argument("--alpha", type=int, default=0,
                    help="defaults to 2*rank, the usual pairing")
    ap.add_argument("--dropout", type=float, default=0.05)
    # Measured with measure_tokens.py against Qwen3's tokeniser on the 1600-row
    # mix, not carried over from the Gemma line: the same text tokenises to a
    # different length, so a number copied between tokenisers is a guess wearing
    # a measurement's clothes.
    #
    #   prompt mean 1297 / max 1761      answer mean 467 / max 712
    #   total  p50 1744  p95 2192  p99 2318  p100 2439
    #
    # So 2560 clips nothing and anything larger only spends KV cache the T4
    # needs elsewhere. 2048 would clip 249 rows — 16% of the set — and clipping
    # is from the left, so those rows keep their answer and lose the front of
    # their case: they would train the model to cite evidence it was never
    # shown, at a normal-looking loss.
    ap.add_argument("--max-seq-len", type=int, default=2560)
    # ~1600 rows at effective batch 16 is 100 steps per epoch; three epochs give
    # ~300, which is what the Gemma rubric run used at the same effective batch.
    # These are the two numbers to re-derive when the set changes size.
    ap.add_argument("--epochs", type=float, default=3.0)
    ap.add_argument("--grad-accum", type=int, default=16)
    ap.add_argument("--batch-size", type=int, default=1)
    # Below the Gemma line's 2e-4. A 4B model has more to disturb than a 2B one,
    # and the failure this adapter must not introduce is a drop in `stddev` —
    # the base already scores identical inputs identically, and that is the one
    # measured number there is nothing to gain on and everything to lose.
    ap.add_argument("--lr", type=float, default=1e-4)
    ap.add_argument("--target-mlp", action="store_true",
                    help="also adapt gate/up/down_proj. More capacity, and more "
                         "to overfit with on a set this small — off by default")
    # The Colab line's four flags. Every default below is the pre-pilot
    # behaviour, so the Kaggle notebooks keep running unchanged.
    #
    # A step limit rather than a smaller dataset: data size *predicts* wall
    # time, max_steps *guarantees* it, and a free-tier session wall is not
    # somewhere you find out about by prediction.
    ap.add_argument("--max-steps", type=int, default=0,
                    help=">0 replaces --epochs with a hard optimizer-step "
                         "limit; derive it from a measured s/row, never type it")
    # A step limit still has to be *converted* from a measured s/row, and that
    # conversion is where the budgets have gone wrong: rubric-curve computed 200
    # steps from a rows/step figure that was half the truth and died at step 197
    # of 200 on the session wall, with exit 137 and no adapter written. The step
    # count was the estimate; the clock was the constraint. So state the clock.
    #
    # This stops the loop the way Trainer's own early stopping does — by asking
    # it to — so trainer.train() *returns* and every line after it still runs:
    # save_pretrained, the eval, train_metrics.json. That is the whole point. A
    # session wall gives none of them, and --save-steps checkpoints are only a
    # salvage path, not a result (rubric-curve's had to be re-uploaded by hand
    # as a separate dataset before anything could read it).
    ap.add_argument("--max-minutes", type=float, default=0.0,
                    help=">0 stops training gracefully after this many minutes "
                         "of the training loop and saves; the budget guarantee "
                         "that --max-steps only estimates")
    ap.add_argument("--save-steps", type=int, default=0,
                    help=">0 switches to step-based checkpointing, which the "
                         "free tier's dynamic quota makes load-bearing")
    ap.add_argument("--resume", action="store_true",
                    help="continue from the last checkpoint in --out-dir")
    # 28-35 s/row is roughly 6-8x slower than the T4's FLOP budget explains,
    # and the first suspect is bitsandbytes' NF4 dequant on every matmul. This
    # flag is how that hypothesis gets measured instead of argued about — the
    # same discipline that killed the DataParallel hypothesis in 15 minutes.
    ap.add_argument("--no-4bit", action="store_true",
                    help="load the base in fp16 instead of 4-bit NF4; may OOM "
                         "on a 16 GB card, which is itself a result")
    ap.add_argument("--seed", type=int, default=20260727)
    return ap.parse_args()


def load_jsonl(path: str) -> list[dict]:
    if not os.path.exists(path):
        sys.exit(f"{path} not found — run build_dataset.py first, or "
                 f"kaggle/fetch_dataset.sh if the set was published")
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

    if args.no_4bit:
        # ~8 GB of fp16 weights on a 16 GB T4, with gradient checkpointing,
        # batch 1 and LoRA only on attention. prepare_model_for_kbit_training
        # is skipped because there are no k-bit layers to prepare; the two
        # things it does that still matter here — turning checkpointing on,
        # and making the frozen embedding pass gradients through to the
        # adapters — are done explicitly instead. Without the second, every
        # LoRA gradient under a checkpointed block is None and the run trains
        # nothing while reporting a loss.
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

    # Step-based saving and epoch-based eval cannot both be the reference for
    # "best": Trainer requires the two strategies to match when it is asked to
    # reload the best checkpoint. Under a step limit there is usually no epoch
    # boundary at all, so there would be no checkpoint to reload and the run
    # would die at the very end having trained perfectly well.
    save_strategy = "steps" if args.save_steps > 0 else "epoch"
    load_best = not (args.save_steps > 0 or args.max_steps > 0)

    trainer = Trainer(
        model=model,
        train_dataset=train_ds,
        eval_dataset=eval_ds,
        args=TrainingArguments(
            output_dir=args.out_dir,
            num_train_epochs=args.epochs,
            # -1 is Trainer's "no limit"; args default 0 means the same thing
            # here, and 0 passed through would be read as "stop immediately".
            max_steps=args.max_steps if args.max_steps > 0 else -1,
            per_device_train_batch_size=args.batch_size,
            gradient_accumulation_steps=args.grad_accum,
            per_device_eval_batch_size=args.batch_size,
            eval_accumulation_steps=1,
            learning_rate=args.lr,
            lr_scheduler_type="cosine",
            warmup_ratio=0.05,
            logging_steps=5,
            eval_strategy="epoch",
            save_strategy=save_strategy,
            save_steps=args.save_steps if args.save_steps > 0 else 500,
            save_total_limit=2,
            load_best_model_at_end=load_best,
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

    deadline = None
    if args.max_minutes > 0:
        deadline = DeadlineStop(args.max_minutes)
        trainer.add_callback(deadline)

    # world_size is not decoration. `machine_shape: NvidiaTeslaT4` hands over a
    # two-card machine and Trainer puts DataParallel on it without being asked,
    # so per_device_train_batch_size is charged once per card: the real rows per
    # step is batch × accum × cards. Printing batch × accum alone has now cost
    # three runs — rubric-curve budgeted 800 row passes from this line, got 1600,
    # and died at step 197 of 200 on the 12 h session wall. The number below is
    # what every budget is written from, so it is the number that has to be true.
    world = max(1, torch.cuda.device_count())
    rows_per_step = args.batch_size * args.grad_accum * world
    steps = (args.max_steps if args.max_steps > 0 else
             max(1, int(len(train_ds) * args.epochs // rows_per_step)))
    limit = " (--max-steps)" if args.max_steps > 0 else ""
    print(f"\neffective batch: {rows_per_step} rows/step"
          f"  ({args.batch_size} x {args.grad_accum} accum x {world} gpu)"
          f"  ~{steps} optimizer steps{limit}"
          f"  = {steps * rows_per_step} row passes")
    if deadline is not None:
        print(f"deadline: {args.max_minutes:.0f} dk — hangisi once gelirse. "
              f"Sinir dolarsa egitim adim sinirina varmadan durur ve "
              f"agirliklar yine yazilir.")
    train_result = trainer.train(resume_from_checkpoint=args.resume or None)

    model.save_pretrained(args.out_dir)
    tokenizer.save_pretrained(args.out_dir)

    metrics = trainer.evaluate()
    # train_runtime is the Trainer's own clock over the training loop alone —
    # not the model download, not tokenisation, not the final eval. It is the
    # only cost figure here that is measured rather than inferred, and the
    # thing a probe has to read: subtracting a guessed load time from wall
    # clock priced two identical runs at 20.6 and 10.3 s/row, because the
    # guess, not the work, was what differed between them.
    rows_seen = train_result.metrics.get("train_samples_per_second", 0.0) * \
        train_result.metrics.get("train_runtime", 0.0)
    with open(os.path.join(args.out_dir, "train_metrics.json"), "w") as fh:
        json.dump({"eval": metrics, "train": train_result.metrics,
                   "row_passes": round(rows_seen),
                   "trainable_params": trainable,
                   "rank": args.rank, "alpha": args.alpha,
                   "base_model": args.base_model,
                   "max_seq_len": args.max_seq_len,
                   "max_steps": args.max_steps, "grad_accum": args.grad_accum,
                   # Which limit actually ended the run. Without this the only
                   # difference between "trained as planned" and "ran out of
                   # clock at 60% of plan" is a line of stdout in a log nobody
                   # keeps, and the two produce adapters that are not comparable.
                   "max_minutes": args.max_minutes,
                   "stopped_on_deadline": bool(deadline and deadline.fired),
                   "batch_size": args.batch_size, "four_bit": not args.no_4bit,
                   "target_modules": targets}, fh, indent=2)

    print(f"\nadapter written to {args.out_dir}")
    print(f"final eval loss: {metrics.get('eval_loss'):.4f}")
    print("\nThe loss is not the result. What this adapter has to move is "
          "present_score_mae — the base rates evidence it agrees is there nearly "
          "a band off, 0.77 on a 1-5 scale — and the 1.3% of quotes it invents. "
          "absent_rate and schema_valid are floors, not targets: the base "
          "already scores 0.89 and 0.95 there, and an adapter that trades them "
          "away for a better MAE does not ship. Only rubric_eval.py measures "
          "any of it. Run it against the held-out set, and against the base in "
          "the same session, before shipping anything.")


if __name__ == "__main__":
    main()
