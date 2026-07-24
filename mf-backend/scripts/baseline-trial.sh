#!/usr/bin/env bash
#
# Runs the same case through the analysis engine N times and reports how stable
# the result was.
#
# This is the measurement the product claim rests on. "Consistent scoring" is
# worth exactly the number behind it, and this script produces that number:
#
#   schema_valid_rate  how often the model held the output schema unaided
#   stddev_score       how far the overall score moved across identical inputs
#
# Run it once against the base model to get a baseline, then again after a PEFT
# build with ANALYSIS_MODEL set to the adapter's model id. The difference
# between the two runs is what the fine-tuning was worth — asserted by nobody,
# measured here.
#
# Usage:
#   scripts/baseline-trial.sh                        # defaults, sample deck
#   TRIALS=10 scripts/baseline-trial.sh              # more repetitions
#   CASE_FILE=my-deck.txt scripts/baseline-trial.sh  # your own case
#   ANALYSIS_MODEL=finetuned-v1 scripts/baseline-trial.sh
#
# set -u catches an unset variable rather than silently sending an empty one;
# pipefail stops a failing curl from being masked by a succeeding jq.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
EMAIL="${EMAIL:-baseline@example.com}"
PASSWORD="${PASSWORD:-baseline-pass-12345}"
DOMAIN="${DOMAIN:-startup-investability}"
TRIALS="${TRIALS:-5}"
ANALYSIS_MODEL="${ANALYSIS_MODEL:-}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CASE_FILE="${CASE_FILE:-$script_dir/../testdata/ornek-deck.txt}"

command -v jq >/dev/null || { echo "jq is required"; exit 1; }
[ -f "$CASE_FILE" ] || { echo "case file not found: $CASE_FILE"; exit 1; }

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }

# --- 1. reachable? ---------------------------------------------------------
say "1/4  $BASE_URL"
if ! curl -sf -m 5 "$BASE_URL/health" >/dev/null; then
    echo "   backend is not answering. Start it with: go run ./cmd/server"
    exit 1
fi
echo "   ok"

# --- 2. token --------------------------------------------------------------
#
# Register, and fall back to login when the account already exists. Doing it in
# this order means the script is runnable on a fresh database without a manual
# setup step, and idempotent on every run after that.
say "2/4  authenticating as $EMAIL"
token=$(curl -s -X POST "$BASE_URL/auth/register" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg e "$EMAIL" --arg p "$PASSWORD" \
        '{email:$e,password:$p,name:"Baseline Harness"}')" \
    | jq -r '.access_token // empty')

if [ -z "$token" ]; then
    token=$(curl -s -X POST "$BASE_URL/auth/login" \
        -H 'Content-Type: application/json' \
        -d "$(jq -nc --arg e "$EMAIL" --arg p "$PASSWORD" '{email:$e,password:$p}')" \
        | jq -r '.access_token // empty')
fi
[ -n "$token" ] || { echo "   could not obtain a token"; exit 1; }
echo "   ok"

# --- 3. rubric present? ----------------------------------------------------
#
# Checked separately so a missing rubric reports itself clearly. The usual cause
# is a backend that has not restarted since migration 005 was added, and that is
# a much less obvious diagnosis from a 404 on the trial call.
say "3/4  rubric '$DOMAIN'"
domains=$(curl -s "$BASE_URL/analysis/domains" -H "Authorization: Bearer $token")
if ! echo "$domains" | jq -e --arg d "$DOMAIN" '.domains[]? | select(.slug==$d)' >/dev/null; then
    echo "   not found. Available:"
    echo "$domains" | jq -r '.domains[]?.slug // "  (none — has the backend restarted since migration 005?)"'
    exit 1
fi
echo "$domains" | jq -r --arg d "$DOMAIN" \
    '.domains[] | select(.slug==$d) | "   \(.name) — v\(.version), \(.criteria|length) criteria"'

# --- 4. the measurement ----------------------------------------------------
#
# Sequential on the server side: the replicas share one 6 GB card, so parallel
# trials would measure contention rather than the model. Expect roughly
# TRIALS × single-generation latency.
say "4/4  running $TRIALS trials (sequential — this takes a while)"
payload=$(jq -nc \
    --arg d "$DOMAIN" \
    --arg t "$(basename "$CASE_FILE")" \
    --rawfile s "$CASE_FILE" \
    --argjson n "$TRIALS" \
    --arg m "$ANALYSIS_MODEL" \
    '{domain:$d, subject_title:$t, subject:$s, trials:$n}
     + (if $m == "" then {} else {model:$m} end)')

start=$(date +%s)
result=$(curl -s -X POST "$BASE_URL/analysis/trial" \
    -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' \
    --max-time 1800 \
    -d "$payload")
elapsed=$(( $(date +%s) - start ))

if ! echo "$result" | jq -e '.trial_group' >/dev/null 2>&1; then
    echo "   failed:"
    echo "$result" | jq . 2>/dev/null || echo "$result"
    exit 1
fi

say "RESULT  (${elapsed}s wall clock)"
echo "$result" | jq -r '
  "  trials completed     \(.trials)",
  "  schema_valid_rate    \(.schema_valid_rate)   <- the headline number",
  "  mean_score           \(.mean_score // "n/a")",
  "  stddev_score         \(.stddev_score // "n/a")   <- consistency claim",
  "  min / max            \(.min_score // "n/a") / \(.max_score // "n/a")",
  "  mean_coverage        \(.mean_coverage)",
  "  mean_latency_ms      \(.mean_latency_ms)",
  "  trial_group          \(.trial_group)"
'

# Per-criterion spread localises instability. An overall stddev of 8 says
# "something moved"; this says which criterion, and therefore whose guidance
# text in the rubric needs rewriting.
if echo "$result" | jq -e '.per_criterion_stddev | length > 0' >/dev/null; then
    say "PER-CRITERION SPREAD  (0..1 normalised, worst first)"
    echo "$result" | jq -r '.per_criterion_stddev | to_entries
        | sort_by(-.value) | .[] | "  \(.value)   \(.key)"'
fi

say "Re-read later with:"
echo "  curl -s $BASE_URL/analysis/trials/$(echo "$result" | jq -r .trial_group) \\"
echo "    -H 'Authorization: Bearer <token>' | jq ."
