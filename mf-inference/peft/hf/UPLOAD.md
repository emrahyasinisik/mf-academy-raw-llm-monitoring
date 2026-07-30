# Publishing `rubric-dataset` to Hugging Face

Live at
[huggingface.co/datasets/Emrahisik/rubric-dataset](https://huggingface.co/datasets/Emrahisik/rubric-dataset)
— public, CC-BY-4.0, first published 2026-07-30 (`fe8b641`).

The upload is **run by hand, not scripted.** That was a deliberate choice against
writing a `push_hf.sh` sibling to `push.sh`, and the cost is written down here
rather than discovered later.

It is done with the `hf` CLI rather than the web UI. That was not the original
plan — the web route was chosen while nothing HF-related was installed on the Mac,
and `hf` arriving changed the cheaper option without changing the decision. Both
routes are below; neither is a script.

## What the unscripted route costs

The Kaggle side has [`../kaggle/push.sh`](../kaggle/push.sh): the data is staged,
the metadata is written by the script, and the upload is repeatable. This side has
none of that. Three things follow, and each one has a countermeasure below:

1. **The HF copy silently diverges** when the data is regenerated. Nothing
   detects it.
2. **The commit the data came from is lost** unless it is written into the card by
   hand.
3. **The row counts in the card become wrong** the first time `--n` changes.

The countermeasure is this file plus [`CARD.md`](CARD.md) being version-controlled
here. The card that is live on HF should be a copy of `CARD.md` at some commit of
this repo — never edited only in the HF web editor, because then the repo copy is
the stale one and there is no way to tell which is which.

## Prerequisites

The CLI is not part of the training environment. Install it isolated — the
`requirements.txt` stack pins a stock torch and MLC pins its own, and resolving
them together is how the MLC install broke once already:

```sh
pipx install huggingface_hub     # provides `hf`, `huggingface-cli`, `tiny-agents`
hf auth login                    # or export HF_TOKEN; needs *write*
hf auth whoami                   # => user=Emrahisik
```

`hf auth whoami` reports the user and nothing about the token's role, so a
read-only token looks identical to a write one until something fails. `hf repos
create` is the cheapest probe: it is step 1 anyway, and it 403s immediately
without write access.

The HF account is `Emrahisik`. This is **not** the Kaggle handle, which is
`emrahik` — the two differ, so neither can be pattern-matched from the other. The
HF namespace appears in `CARD.md`'s citation block and in the `load_dataset` calls
below.

## Publishing

```sh
hf repos create Emrahisik/rubric-dataset --repo-type dataset --public

STAGE=$(mktemp -d)
cp ../data/rubric_train.jsonl ../data/rubric_eval.jsonl \
   ../data/contrast_investment.jsonl ../data/contrast_marketing.jsonl "$STAGE/"
cp CARD.md "$STAGE/README.md"

hf upload Emrahisik/rubric-dataset "$STAGE" . --repo-type=dataset \
  --commit-message "rubric mix: investment + marketing, with card"
```

Three things about that, each of which cost a check to establish:

- **Stage it.** `CARD.md` has to arrive as the repo's `README.md`, and the data
  has to arrive without the rest of `../data/` — the Flutter-era and `persona_*`
  splits live there too.
- **One commit for all five files.** The `configs:` block in the card names data
  files, so a card that lands before its data makes the viewer build once against
  nothing and rebuild later. One commit, one build.
- **`hf upload` prints a misleading summary.** The successful first publication
  reported `5/5 files checked, 0/0 uploaded (0.00B transferred), 0 committed in 0
  commit(s)` — and then returned a commit URL, with all five files present on the
  Hub. Do not read those zeros as failure. Verify against the API instead.

`rubric_train.jsonl` is over the 10 MB line, so it lands in git LFS. That is
automatic and fine; it only means a `git clone` of the dataset needs LFS.

### Web UI, if the CLI is not available

**New → Dataset**, owner `Emrahisik`, name `rubric-dataset`, license `cc-by-4.0`,
**Public**. Then **Files → Add file → Upload files** for the four `.jsonl`, and
paste `CARD.md` into the repo's `README.md` **including the YAML frontmatter**.
Never edit the card only in HF's web editor — then the repo copy is the stale one
and there is no way to tell which is which.

## Verifying

The upload landing is not the same as the dataset working. Check both.

```sh
# files, visibility, and whether the frontmatter parsed
curl -sS https://huggingface.co/api/datasets/Emrahisik/rubric-dataset \
  | python3 -m json.tool | grep -E '"private"|rfilename|config_name|license'
```

`cardData.configs` appearing in that response is the real confirmation that the
frontmatter was understood — a malformed `configs:` block yields files that
uploaded fine, a page that looks fine, and no configs here.

```sh
# row counts, once the viewer has built
curl -sS "https://datasets-server.huggingface.co/size?dataset=Emrahisik/rubric-dataset"
```

Expect `rubric/train` 1600, `rubric/validation` 200, `contrast/investment` 60,
`contrast/marketing` 60.

That endpoint has **two** not-yet states, and the second one is a trap:

1. Immediately after the upload it answers `"The server is busier than usual and
   the response is not ready yet"`. Obvious enough.
2. Then it starts answering with a **fully-formed skeleton whose numbers are all
   zero**: `size.dataset.num_rows: 0`, `size.splits: []`, and the real status in a
   separate `pending` array listing each config still being processed. A readiness
   check that greps for `num_rows` matches here and reports success on an empty
   dataset.

So the condition is `pending == [] and size.splits != []`, and `failed` must be
checked too — a config that errors out will otherwise look like one that is still
working, forever:

```sh
curl -sS "https://datasets-server.huggingface.co/size?dataset=Emrahisik/rubric-dataset" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); \
print('failed' if d['failed'] else 'ready' if not d['pending'] and d['size']['splits'] else 'pending')"
```

This is the HF counterpart of Kaggle's "never push a kernel against a
still-processing dataset version". Poll; do not conclude.

Finally, confirm the loader agrees with the viewer:

```python
from datasets import load_dataset
r = load_dataset("Emrahisik/rubric-dataset", "rubric")
c = load_dataset("Emrahisik/rubric-dataset", "contrast")
assert len(r["train"]) == 1600 and len(r["validation"]) == 200
assert len(c["investment"]) == 60 and len(c["marketing"]) == 60
```

## Re-publishing after the data is regenerated

Do all of it, in order. Skipping any step leaves the card asserting something
that is no longer true, and the card's whole value is that its numbers are
checkable.

1. Regenerate — the commands are at the top of
   [`../kaggle/push.sh`](../kaggle/push.sh).
2. Recount, and update the tables in `CARD.md` if anything moved:

   ```sh
   wc -l ../data/rubric_train.jsonl ../data/rubric_eval.jsonl \
         ../data/contrast_investment.jsonl ../data/contrast_marketing.jsonl
   ```

   The composition table (findings, absent rate, score distribution) is measured,
   not estimated. Recompute it rather than adjusting it by eye.
3. Update the **commit** row in the card's *Provenance* table. It names the last
   commit to touch any of the three generators, not the day's `HEAD`:

   ```sh
   git log -1 --format=%h -- ../build_dataset.py ../merge_rubric_sets.py ../build_contrast_set.py
   ```

   Land the generator change **before** reading that SHA. A SHA read from a dirty
   tree describes a state nobody can check out, which is worse than no SHA.
4. Re-run the staging and `hf upload` block above, minus `hf repos create`. HF
   replaces by filename, and the card goes up in the same commit as the data for
   the same reason it did the first time.
5. Re-verify with both endpoints, then re-run the `load_dataset` assertions with
   the new counts.

## Not uploaded, on purpose

`train_qlora_qwen.py` and `rubric_eval.py` travel *inside* the Kaggle dataset,
because a Kaggle notebook that lived only in Kaggle's version history was once
recovered with its training code gone. That reason does not apply here: the code
lives in this repository, and the card links to it.

Also excluded: everything else in `../data/` — the Flutter-era `train.jsonl` /
`eval.jsonl` and the `persona_*` splits belong to earlier lines of work and are
not part of this dataset.
