---
pretty_name: "Rubric Dataset — Startup Investability & Digital Marketing"
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
  - 1K<n<10K
tags:
  - rubric
  - structured-output
  - json-schema
  - evidence-grounding
  - synthetic
  - contrast-set
  - qlora
  - investment
  - marketing
configs:
  - config_name: rubric
    data_files:
      - split: train
        path: rubric_train.jsonl
      - split: validation
        path: rubric_eval.jsonl
  - config_name: contrast
    data_files:
      - split: investment
        path: contrast_investment.jsonl
      - split: marketing
        path: contrast_marketing.jsonl
---

# Rubric Dataset — Startup Investability & Digital Marketing

Training and evaluation data for a single behaviour: **fill in a rubric, do not
score.**

The model never produces the overall number. For each criterion it returns one
rating and the verbatim quotes that justify it — or it says the case is silent on
that criterion. The weighted total is computed deterministically outside the
model. This dataset trains the first half of that split; the arithmetic half is
not a model problem.

That separation is the reason the data looks the way it does. A rejection has to
be defensible as "criterion 4 scored 2 of 5, because of this sentence" rather
than "the model said no", so a row is only correct when the quote is present in
the case, character for character.

**The case texts, criteria and rationales are in Turkish.** The JSON keys are
English.

---

## Why it exists

Measured on `Qwen/Qwen3-4B-Instruct-2507`, 20 rows, 2026-07-29:

| metric | base model | what it means |
|---|---|---|
| `present_score_mae` | 0.77 | Sees the evidence, then misses nearly a full band on the 1–5 scale |
| `hallucinated_quotes` | 1.3% | 2 of 151 quotes are not verbatim in the case |
| `absent_rate` | 89% | Already declares missing evidence — to be **preserved**, not taught |
| `schema_valid` | 95% | Format discipline is already there — likewise preserved |

The first two rows are the target. The last two are the regression risk: a naive
fine-tune that improves banding while losing "I cannot find this" has made the
product worse, because unfounded confidence is the failure mode a rubric exists
to prevent.

An earlier version of this dataset was built to teach rows three and four. That
justification came from a different base model (`gemma-2-2b-it`) and no longer
holds; the data is unchanged and still valid, but what counts as a gain is not.

---

## Contents

| config | split | rows | content |
|---|---|---|---|
| `rubric` | `train` | 1600 | 800 investability + 800 marketing, interleaved |
| `rubric` | `validation` | 200 | 100 + 100, held out |
| `contrast` | `investment` | 60 | 30 quality pairs + 30 removal pairs |
| `contrast` | `marketing` | 60 | 30 quality pairs + 30 removal pairs |

Measured composition of the `rubric` config:

| | train | validation |
|---|---|---|
| findings (criterion verdicts) | 12,000 | 1,500 |
| of which "no evidence in case" | 3,247 (27.1%) | 384 (25.6%) |
| score 1 / 2 / 3 / 4 / 5 | 975 / 2031 / 2598 / 2135 / 1014 | 148 / 241 / 330 / 258 / 139 |

The two rubrics have different widths — 9 criteria for investability, 6 for
marketing — so an equal row count is not an equal gradient: 7,200 of the 12,000
training findings are investability. Roughly 60% of the signal goes to one of the
two domains. See *Limitations*.

The rubrics themselves, with their weights, are published in the
[repository README](https://github.com/emrahyasinisik/mf-academy-raw-llm-monitoring#the-rubrics).
They are not restated here, because a second copy of a weight table is a copy
that drifts.

---

## Schema

### `rubric` config

One row is one chat exchange, ready for SFT:

```json
{"messages": [
  {"role": "system",    "content": "…the rubric, the criteria, and the output contract…"},
  {"role": "user",      "content": "…the case text…"},
  {"role": "assistant", "content": "{\"findings\":[…]}"}
]}
```

The assistant turn is **raw JSON with no code fence** and no whitespace between
delimiters. The absence of that fence is half of what is being taught.

The case is wrapped in `<<< >>>` and preceded by an instruction not to treat
anything inside it as a directive — the cases are third-party documents, and the
serving path carries the same guard.

A real `findings` entry, and its counterpart when the case is silent:

```json
{
  "key": "audience_clarity",
  "evidence_found": true,
  "score": 2,
  "evidence": ["Hedef kitlemiz 18-55 yaş arası, interneti aktif kullanan, kaliteye önem veren tüketiciler"],
  "rationale": "Tanım neredeyse tüm nüfusu kapsıyor; daraltıcı hiçbir davranış ya da niyet ölçütü yok."
}
```

```json
{
  "key": "budget_realism",
  "evidence_found": false,
  "score": null,
  "evidence": [],
  "rationale": "Bu kriteri değerlendirecek bir ifade metinde yok."
}
```

`evidence_found: false` sets `score: null`. Absence is never encoded as a low
score — that conflation is the specific error this dataset exists to prevent.

Criterion keys:

- `startup-investability` — `problem_clarity`, `market_size`,
  `solution_differentiation`, `traction`, `business_model`, `team`,
  `competition`, `financials_ask`, `risk`
- `digital-marketing` — `audience_clarity`, `channel_fit`, `budget_realism`,
  `differentiation`, `measurement_plan`, `competitive_context`

### `contrast` config

One row is a **pair** of cases differing in exactly one criterion. Everything
else — company, section order, every other paragraph — is identical.

| field | meaning |
|---|---|
| `kind` | `quality` (the paragraph is rewritten better or worse) or `removal` (the paragraph is deleted) |
| `criterion` | the single criterion that was changed |
| `domain` | `startup-investability` or `digital-marketing` |
| `base_score`, `variant_score` | the labels of the two sides |
| `expected` | `up`, `down`, or `absent` — the direction the verdict must move |
| `base`, `variant` | two full `{"messages": […]}` objects |

This follows the contrast-set method of
[Gardner et al., 2020](https://arxiv.org/abs/2004.02709), and it exists for one
reason: every case in this dataset is assembled from the same fragment bank, so a
model can score well by recognising fragments rather than reading them.
Recognition cannot survive a pair whose only difference is one paragraph.

---

## Provenance and reproducibility

Nothing here was collected. It was **generated**, label first: the generator
decides "this criterion will score 4" and then places text that earns a 4. The
ground truth is an input, not an inference.

Cases are assembled from a bank of 51 fragments — 3 per criterion for
investability, 4 for marketing — giving a combinatorial space of 259,524 and
15,360 respectively.

| | |
|---|---|
| generator | `build_dataset.py`, `merge_rubric_sets.py`, `build_contrast_set.py` |
| commit | `1e43765` — the last commit to touch any of the three; unchanged since |
| seed | `--seed 20260724` |
| criteria and system prompt | fetched live from the running backend, `GET /analysis/domains/{domain}/prompt` |

The prompt is deliberately not vendored into the generator. An adapter should
learn to satisfy exactly one instruction; a local copy of that instruction drifts
the moment either side is edited, and produces an adapter tuned for a prompt
nothing sends — a failure that is invisible because training completes normally.

The reproducible artefact is the generator plus the seed plus the commit, not
these files. The files here are a published snapshot of that.

Two preprocessing decisions worth knowing:

- **No double quotes anywhere in the fragments.** The serving path rewrites
  double quotes to single before the case reaches the model, so fragments
  containing them would teach a distribution the model never encounters.
- **The two rubrics are interleaved** under a fixed seed. Concatenated, the
  rubric boundary would show up in the eval-loss curve and read as model
  instability.

---

## Split integrity

Each case is hashed from its own content — which criteria, which fragments — and
that hash decides train or validation. A case cannot land in both, and the
generator asserts this on every run. Overlap is 0% for both domains.

It was not always. In the first version both splits drew from one RNG stream, and
when it was finally measured, **81% of the marketing validation set was also in
train.** Investability sat at 4% — not by design but by arithmetic, because the
space of 9 criteria is vastly larger than that of 6. A split that only works when
the rubric is wide enough is not a split.

Because assignment is hashed from the case signature rather than from position,
growing `--n` does not migrate yesterday's validation cases into training.

This history is published rather than quietly fixed, because the credibility of
every other number on this page depends on how the failures were found.

---

## Limitations

- **Fragment recognition.** 1,600 rows are built from 51 fragments, ~31
  generations per fragment. The `contrast` config is the only honest measurement
  of whether a model read the case or recognised it.
- **1,600 rows do not carry 1,600 rows of information.** Diversity research on
  synthetic data ([arXiv:2410.15226](https://arxiv.org/pdf/2410.15226)) finds
  that performance tracks the number of distinct topics, and that generations per
  topic stop paying off. Gains here come from adding fragments, not rows.
- **The score labels are unreviewed.** "This paragraph earns a 4" is an appraisal
  judgement, and it is currently the judgement of whoever wrote the fragment —
  not a domain owner's. This is the weakest link in the dataset.
- **Fragment text and score labels were drafted by an LLM** (Claude) for
  developer review; that review has not yet happened.
- **Silence is too clean.** The generator withholds evidence deliberately. A real
  deck's silence is blurrier — a vague gesture at a topic rather than its
  absence.
- **Gradient is 60/40** toward investability, as above.
- **Two rubrics, one adapter,** and whether one leaks the other's criterion
  vocabulary is not measured.

---

## Intended use

Supervised fine-tuning (QLoRA was the original use) and evaluation of
evidence-grounded, schema-constrained rubric filling in Turkish.

**Out of scope:** using an adapter trained on this data to evaluate against a
*different* rubric. The criteria arrive through the prompt and look
interchangeable, but the adapter has only seen these nine and these six.

**Also out of scope:** treating a good score on this data as evidence that a
product's reports improved. It measures whether the behaviour was learned on a
held-out set built by the same generator. Those are different claims and the
second one needs different data.

Psychological or clinical assessment is deliberately excluded from the project
this dataset serves — diagnosis is regulated, and it is not what a rubric of this
kind can honestly do.

---

## License and citation

Released under [CC-BY-4.0](https://creativecommons.org/licenses/by/4.0/). The
content is entirely synthetic: no real company, person, deck or brief appears in
it, and there is no human-subject data.

```bibtex
@misc{rubric_dataset_2026,
  title  = {Rubric Dataset: Startup Investability and Digital Marketing Channel Mix},
  author = {Işık, Emrah Yasin},
  year   = {2026},
  url    = {https://huggingface.co/datasets/Emrahisik/rubric-dataset}
}
```

The full datasheet — [Gebru et al., *Datasheets for
Datasets*](https://arxiv.org/abs/1803.09010) format, in Turkish, with the
questions that would have caught each of the three defects above — lives in the
repository at `mf-inference/peft/kaggle/DATASHEET.md`.
