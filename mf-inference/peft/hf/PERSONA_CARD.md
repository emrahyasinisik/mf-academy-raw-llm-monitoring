---
pretty_name: "Investment Persona — Grounded Verdicts and Clarifying Questions"
license: cc-by-4.0
language:
  - tr
task_categories:
  - text-generation
annotations_creators:
  - machine-generated
source_datasets:
  - original
size_categories:
  - n<1K
tags:
  - investment
  - evidence-grounding
  - citation
  - clarifying-question
  - agent
  - synthetic
  - qlora
  - persona
configs:
  - config_name: persona
    data_files:
      - split: train
        path: persona_train.jsonl
      - split: validation
        path: persona_eval.jsonl
  - config_name: meta
    data_files:
      - split: train
        path: persona_train_meta.jsonl
      - split: validation
        path: persona_eval_meta.jsonl
---

# Investment Persona — Grounded Verdicts and Clarifying Questions

The conversational sibling of
[`Emrahisik/rubric-dataset`](https://huggingface.co/datasets/Emrahisik/rubric-dataset).
That one teaches a model to fill in a rubric. This one teaches an agent that has
just researched a subject on the web to do two things a small base model does
badly:

1. **Cite what it was given, and only that.** Handed five numbered sources, the
   base writes a fluent verdict drawn largely from its own pretraining and cites
   nothing — indistinguishable, to a reader, from a sourced one. Worse, it will
   invent a citation number the evidence does not contain.
2. **Ask instead of guessing.** When the decisive fact is missing — the stage,
   the revenue, the budget — the base still commits to a verdict rather than
   asking the one question that would change it. A confident verdict on absent
   evidence is the failure the product exists to avoid.

**The evidence, verdicts and questions are in Turkish.** The verdict labels and
the `KARAR / SKOR / GEREKÇE` block are part of the format the UI parses, so they
are Turkish too.

---

## The shape of a row

Every row is `[system, user, assistant]`. The system prompt and the turn
instruction are **fetched from the running backend** at generation time, not
copied into the generator — so the training distribution is byte-identical to
what inference sends. A local copy drifts the moment either side is edited, and
what comes out is an adapter tuned for a prompt nothing sends: the run finishes,
the loss looks fine, and nothing says otherwise.

The user turn is an evidence block in the exact layout the agent's own `gather`
step produces — a mix of web sources (with URLs) and DeepKwiki passages
(without), numbered, in shuffled order.

Two answer modes, and the split between them is the point:

| mode | the assistant answers with | what it teaches |
|---|---|---|
| `decide` | a verdict in `KARAR / SKOR / GEREKÇE` form, every clause carrying `[n]` | grounding, and a parseable shape |
| `clarify` | **one** question, and stops | not guessing when the deciding fact is absent |

`clarify` rows carry no verdict at all. A model that answers them with a verdict
has failed the row even if the verdict is defensible.

## Composition

Measured from the published files, not estimated.

| | train | validation |
|---|---:|---:|
| rows | 800 | 100 |
| `decide` | 544 | 72 |
| `clarify` | 256 (32%) | 28 (28%) |
| sources per row | 2–5 | 2–5 |

Verdict labels among `decide` rows:

| | train | validation |
|---|---:|---:|
| Yatırılabilir | 305 | 37 |
| Temkinli | 217 | 29 |
| Yatırılamaz | 22 | 6 |

`Yatırılamaz` is thin on purpose and thin by accident both: the generator reaches
it only when most dimensions land low at once. Treat per-label accuracy on it as
an anecdote at these counts.

Token lengths, measured with Qwen3's tokeniser:

```
prompt  mean  784   max  892
answer  mean  108   max  194
total   mean  892   p95 1015   p99 1031   max 1054
```

So a 1280-token sequence limit clips nothing. This matters more than it sounds:
clipping is from the left, so a shorter limit removes the *front of the evidence
block* and trains the model to cite sources it was never shown — at a
normal-looking loss.

## The `meta` config is the ground truth

`persona_*_meta.jsonl` lines up row-for-row with the data and carries what the
generator chose before it wrote the text:

| field | |
|---|---|
| `mode` | `decide` or `clarify` |
| `label` | the verdict the evidence implies (`decide` only) |
| `n_sources` | how many numbered sources the row contains |
| `score` | the weighted dimension score behind the label |

`n_sources` is what makes a citation checkable: an answer citing `[6]` in a
five-source row has invented it, and that is decidable without a judge model.
The evaluation harness in the repo scores four numbers off this file —
`citation_valid`, `grounded_format`, `asked_when_thin`, `decision_match`.

**Load them together or not at all.** The two configs are separate only because
HF configs cannot mix schemas; a `meta` split read against a differently-sized
data split is silently misaligned, and every number computed from it is wrong in
a way nothing reports.

## Known limits

Read these before trusting a number computed on this data.

- **The labels are constructed, not collected.** Each dimension's quality is
  chosen first, the evidence is assembled to say exactly that, and the verdict
  follows from the score. The label therefore cannot be wrong about the text —
  but it also encodes one view of what a given piece of evidence is worth, and
  that view has not been reviewed by a domain owner.
- **Evidence sentences repeat across the splits.** The combinations are nearly
  disjoint — 1 of 100 validation rows shares its evidence set with a training row
  — but **93% of the distinct evidence sentences in validation also appear in
  training**, because they are drawn from a bank of ten fragments across five
  dimensions. So `decision_match` can be partly satisfied by recalling which
  sentence carries which score, rather than by weighing it.

  `citation_valid` and `asked_when_thin` are far more robust to this, and that is
  why they are read first: both are **structural**. Citation numbers depend on
  the shuffled source order of that particular row, and the ask/decide split
  depends on whether the deciding evidence is present — neither can be answered
  from a memorised sentence-to-score table.
- **Real silence is messier.** `clarify` rows are silent on a dimension because
  the generator withheld it cleanly. A real founder's answer is evasive rather
  than absent, and this data does not contain that.
- **Live research is not in here.** At inference the agent searches the web, so
  the evidence differs every run. This set holds the evidence fixed on purpose —
  a metric that moves because the web moved measures the web, not the model.

## Provenance

| | |
|---|---|
| generator | [`build_persona_dataset.py`](https://github.com/emrahyasinisik/mf-academy-raw-llm-monitoring/blob/main/mf-inference/peft/build_persona_dataset.py) |
| generator commit | `ba99c4b` |
| prompt source | the backend's `GET /decision/prompt`, fetched at generation time |
| seed | `20260724` |
| arguments | `--n 800 --n-eval 100 --clarify-share 0.3` |

Fixed seed and a fetched prompt mean the same commit reproduces the same rows.
The generator is the reproducible artefact; these files are a convenience.

## Citation

```bibtex
@misc{isik2026persona,
  title  = {Investment Persona: Grounded Verdicts and Clarifying Questions},
  author = {I{\c{s}}{\i}k, Emrah Yasin},
  year   = {2026},
  howpublished = {\url{https://huggingface.co/datasets/Emrahisik/persona-dataset}}
}
```

Licensed CC-BY-4.0.
