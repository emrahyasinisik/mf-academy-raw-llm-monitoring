#!/usr/bin/env bash
# Publish the persona training set and the scripts that consume it to Kaggle.
#
# The sibling of push.sh, and separate from it on purpose: `emrahik/rubric-dataset`
# is an input to four kernels, and adding files to it would cut a new version
# that every one of them then has to be re-pointed at. Two products, two
# datasets.
#
# Credentials come from the repo's env files. Nothing sources them for you:
#
#   source ../.env && ./push_persona.sh
#
# NOTE on the credential name: the Kaggle CLI reads KAGGLE_API_TOKEN, and
# mf-backend/.env spells the same secret KAGGLE_API_KEY. This script accepts
# either, because having the token on disk and the tool not finding it has
# already cost a session.
#
# The jsonl files are not committed (peft/.gitignore excludes data/) because the
# generator plus a fixed seed is the reproducible artefact and a copied file is
# not. Regenerate them with:
#
#   PORT=8090 go run ./cmd/server &          # from mf-backend/
#   export BASE_URL=http://localhost:8090 TOKEN=<a token>
#   python3 build_persona_dataset.py --n 800 --n-eval 100 --clarify-share 0.3
set -euo pipefail

DATASET="emrahik/persona-dataset"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PEFT="$(dirname "$HERE")"
STAGE="$HERE/.stage-persona"

if ! command -v kaggle >/dev/null 2>&1; then
  echo "kaggle CLI not found. Install it with:  pipx install kaggle" >&2
  exit 1
fi

# One secret, two spellings. Accept the backend's name and re-export it under
# the one the CLI actually reads.
if [ -z "${KAGGLE_API_TOKEN:-}" ] && [ -n "${KAGGLE_API_KEY:-}" ]; then
  export KAGGLE_API_TOKEN="$KAGGLE_API_KEY"
fi

if [ -z "${KAGGLE_API_TOKEN:-}" ] && [ ! -f "$HOME/.kaggle/access_token" ] \
   && [ ! -f "$HOME/.kaggle/kaggle.json" ]; then
  echo "No Kaggle credentials. Either:" >&2
  echo "  source ../.env                      # sets KAGGLE_API_TOKEN" >&2
  echo "  export KAGGLE_API_TOKEN=\$(grep ^KAGGLE_API_KEY= ../../mf-backend/.env | cut -d= -f2-)" >&2
  echo "  kaggle auth login                   # browser OAuth" >&2
  exit 1
fi

# The meta files are not optional extras. persona_eval.py scores against the
# ground truth the generator recorded there, and it exits if the two lengths
# disagree — so shipping the eval set without its meta produces a dataset that
# trains fine and cannot be measured at all.
for f in persona_train.jsonl persona_eval.jsonl \
         persona_train_meta.jsonl persona_eval_meta.jsonl; do
  if [ ! -f "$PEFT/data/$f" ]; then
    echo "missing $PEFT/data/$f — see the regeneration command at the top" >&2
    exit 1
  fi
done

# Staged rather than uploaded in place: `kaggle datasets version` uploads the
# whole directory, and pointing it at peft/ would push the repo's scripts, out/
# and any adapter sitting there.
rm -rf "$STAGE"
mkdir -p "$STAGE"
for f in persona_train.jsonl persona_eval.jsonl \
         persona_train_meta.jsonl persona_eval_meta.jsonl; do
  cp "$PEFT/data/$f" "$STAGE/"
done
# Scripts travel inside the dataset rather than pasted into notebook cells: the
# Flutter v7 run lived only in Kaggle's version history, and recovering it later
# produced a notebook with a single inference cell and no training code at all.
cp "$PEFT/train_qlora_qwen.py" "$PEFT/persona_eval.py" "$STAGE/"

cat > "$STAGE/dataset-metadata.json" <<JSON
{
  "title": "persona-dataset",
  "id": "$DATASET",
  "licenses": [{"name": "other"}]
}
JSON

echo "staged:"
ls -la "$STAGE"

if kaggle datasets status "$DATASET" >/dev/null 2>&1; then
  kaggle datasets version -p "$STAGE" -m "investment persona: train + eval + meta" --dir-mode zip
else
  kaggle datasets create -p "$STAGE" --dir-mode zip
fi

echo
echo "Wait for the dataset version to finish processing before pushing the"
echo "kernel — a notebook that starts against a still-processing version sees"
echo "no mount at all, and the failure reads as a plain FileNotFoundError."
echo
echo "  kaggle datasets status $DATASET     # until it says ready"
echo "  kaggle kernels push -p $HERE/persona"
