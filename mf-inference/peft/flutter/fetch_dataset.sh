#!/usr/bin/env bash
# Pull the Flutter training set down from Kaggle into data/.
#
# The jsonl files are not committed — peft/.gitignore excludes data/ for the
# same reason the rubric and persona sets are excluded: the generator plus a
# fixed seed is the reproducible artefact, a copied file is not. Here the
# "generator" is a Kaggle Dataset rather than a script, so this fetches it.
#
# Credentials come from ../.env (gitignored). Nothing sources that file for you:
#
#   source ../.env && ./fetch_dataset.sh
#
set -euo pipefail

DATASET="emrahik/flutter-dataset"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEST="$HERE/data"

if ! command -v kaggle >/dev/null 2>&1; then
  echo "kaggle CLI not found. Install it with:  pipx install kaggle" >&2
  exit 1
fi

if [ -z "${KAGGLE_API_TOKEN:-}" ] && [ ! -f "$HOME/.kaggle/access_token" ]; then
  echo "No Kaggle credentials. Either:" >&2
  echo "  source ../.env          # sets KAGGLE_API_TOKEN" >&2
  echo "  kaggle auth login       # browser OAuth, caches its own token" >&2
  exit 1
fi

mkdir -p "$DEST"
kaggle datasets download "$DATASET" -p "$DEST" --unzip
ls -la "$DEST"

echo
echo "Now prove the served contract still matches the trained one:"
echo "  python3 $HERE/verify_contract.py"
