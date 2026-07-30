# Publishing `rubric-dataset` to Hugging Face

The upload is **manual, through the web UI**. That was a deliberate choice over
scripting it, and the cost is written down here rather than discovered later.

## What the manual route costs

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

## First publication

1. Create the dataset repo: **huggingface.co → New → Dataset**
   - Owner: the `Emrahisik` account. Note this is **not** the Kaggle handle,
     which is `emrahik`. The two differ, so neither one can be pattern-matched
     from the other; the HF namespace appears in `CARD.md`'s citation block and
     in the `load_dataset` calls below.
   - Name: `rubric-dataset`
   - License: `cc-by-4.0`
   - Visibility: **Public**
2. **Files → Add file → Upload files.** Drag all four from `../data/`:

   ```
   rubric_train.jsonl          10.1 MB   1600 rows
   rubric_eval.jsonl            1.3 MB    200 rows
   contrast_investment.jsonl    0.9 MB     60 pairs
   contrast_marketing.jsonl     0.7 MB     60 pairs
   ```

   `rubric_train.jsonl` is over the 10 MB line, so it will be tracked with git
   LFS. That is fine and automatic; it only means a `git clone` of the dataset
   needs LFS installed.
3. **Edit the repo's `README.md`** and paste [`CARD.md`](CARD.md) verbatim,
   including the YAML frontmatter. The frontmatter is not decoration — the
   `configs:` block is what makes the dataset viewer show two configs and four
   splits.
4. Wait for the viewer to build, then **open the dataset page and confirm all
   four splits render.** A malformed `configs:` block fails here and nowhere
   else: the files upload fine, the page looks fine, and the viewer shows an
   error or a single unnamed split. This is the HF counterpart of Kaggle's
   "never push a kernel against a still-processing dataset version".
5. Verify the config layout from code as well, because the viewer can be
   convincing while the loader is not:

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
4. Re-upload the four files (HF replaces by filename) and re-paste the card.
5. Re-run the `load_dataset` assertions above with the new counts.

## Not uploaded, on purpose

`train_qlora_qwen.py` and `rubric_eval.py` travel *inside* the Kaggle dataset,
because a Kaggle notebook that lived only in Kaggle's version history was once
recovered with its training code gone. That reason does not apply here: the code
lives in this repository, and the card links to it.

Also excluded: everything else in `../data/` — the Flutter-era `train.jsonl` /
`eval.jsonl` and the `persona_*` splits belong to earlier lines of work and are
not part of this dataset.
