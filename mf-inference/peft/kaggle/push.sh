#!/usr/bin/env bash
# Publish the rubric training set and notebook to Kaggle.
#
# Two uploads, because Kaggle separates them: the *dataset* carries the jsonl
# splits AND the scripts the notebook runs, and the *kernel* carries only the
# notebook. Putting the scripts in the dataset rather than pasting them into
# cells is what keeps the run reproducible from this repo — the Flutter v7 run
# lived only in Kaggle's version history, and recovering it later produced a
# notebook with a single inference cell and no training code at all.
#
# Credentials come from ../.env (gitignored). Nothing sources it for you:
#
#   source ../.env && ./push.sh
#
# The jsonl files are not committed (peft/.gitignore excludes data/) because the
# generator plus a fixed seed is the reproducible artefact and a copied file is
# not. Regenerate them with:
#
#   PORT=8090 go run ./cmd/server &          # from mf-backend/
#   export BASE_URL=http://localhost:8090 TOKEN=<a token>
#   python3 build_dataset.py --domain startup-investability --out-dir data/investment
#   python3 build_dataset.py --domain digital-marketing     --out-dir data/marketing
#   python3 merge_rubric_sets.py --input data/investment --input data/marketing
#   python3 build_contrast_set.py --domain startup-investability \
#       --out data/contrast_investment.jsonl
#   python3 build_contrast_set.py --domain digital-marketing \
#       --out data/contrast_marketing.jsonl
set -euo pipefail

DATASET="emrahik/rubric-dataset"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PEFT="$(dirname "$HERE")"
STAGE="$HERE/.stage"

if ! command -v kaggle >/dev/null 2>&1; then
  echo "kaggle CLI not found. Install it with:  pipx install kaggle" >&2
  exit 1
fi

if [ -z "${KAGGLE_API_TOKEN:-}" ] && [ ! -f "$HOME/.kaggle/access_token" ] \
   && [ ! -f "$HOME/.kaggle/kaggle.json" ]; then
  echo "No Kaggle credentials. Either:" >&2
  echo "  source ../.env          # sets KAGGLE_API_TOKEN" >&2
  echo "  kaggle auth login       # browser OAuth, caches its own token" >&2
  exit 1
fi

for f in rubric_train.jsonl rubric_eval.jsonl \
         contrast_investment.jsonl contrast_marketing.jsonl; do
  if [ ! -f "$PEFT/data/$f" ]; then
    echo "missing $PEFT/data/$f — see the regeneration commands at the top" >&2
    exit 1
  fi
done

# Staged rather than uploaded in place: `kaggle datasets version` uploads the
# whole directory, and pointing it at peft/ would push the repo's scripts,
# out/ and any adapter sitting there.
rm -rf "$STAGE"
mkdir -p "$STAGE"
cp "$PEFT/data/rubric_train.jsonl" "$PEFT/data/rubric_eval.jsonl" \
   "$PEFT/data/contrast_investment.jsonl" "$PEFT/data/contrast_marketing.jsonl" "$STAGE/"
cp "$PEFT/train_qlora_qwen.py" "$PEFT/rubric_eval.py" "$STAGE/"

cat > "$STAGE/dataset-metadata.json" <<JSON
{
  "title": "rubric-dataset",
  "id": "$DATASET",
  "licenses": [{"name": "other"}]
}
JSON

echo "staged:"
ls -la "$STAGE"

if kaggle datasets status "$DATASET" >/dev/null 2>&1; then
  kaggle datasets version -p "$STAGE" -m "rubric mix: investment + marketing" --dir-mode zip
else
  kaggle datasets create -p "$STAGE" --dir-mode zip
fi

echo
echo "Wait for the dataset version to finish processing before pushing a"
echo "kernel — a notebook that starts against a still-processing version sees"
echo "no mount at all, and the failure reads as a plain FileNotFoundError."
echo
echo "Then, in order:"
echo "  kaggle kernels push -p $HERE/train    # trains, ~5.5 h"
echo "  # ...wait for it to finish and Save Version..."
echo "  kaggle kernels push -p $HERE/eval     # measures, ~3 h"
echo
echo "The eval kernel takes the adapter from the training kernel's output via"
echo "kernel_sources, so it cannot run until that run has a saved version."
