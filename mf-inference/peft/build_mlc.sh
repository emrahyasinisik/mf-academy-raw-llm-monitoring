#!/usr/bin/env bash
#
# Quantise a merged model to q4f16_1 and generate the config mlc_llm serves.
#
# Runs the conversion *inside* the mlc container, because that is where mlc_llm
# lives. The merge before it runs outside, because it needs transformers, which
# the mlc image deliberately does not carry — the two dependency sets fought
# over setuptools the first time they shared an environment, which is why the
# image uses a conda-managed Python in the first place.
#
# Usage, from mf-inference/:
#   peft/build_mlc.sh                       # models/merged-fp16 -> models/<name>-<quant>-MLC
#   peft/build_mlc.sh --name tuned-v2
#   peft/build_mlc.sh --conv chatml         # override the conversation template
#   peft/build_mlc.sh --ctx 4096            # shrink the KV cache further
#   peft/build_mlc.sh --adapter-id <uuid>   # also reports progress to the panel
#
# The conversation template is derived from the merged model's own config.json
# (model_type), not from whatever happens to be in the MLC cache. Getting it
# wrong produces a build that loads and then emits its turn markers as output —
# a failure that reads as a bad fine-tune, so an unrecognised architecture stops
# the build instead of guessing.
#
set -euo pipefail

NAME="tuned-v1"
QUANT="q4f16_1"
MERGED="models/merged-fp16"
CONV=""
# Qwen3-4B declares 32768, whose KV cache is ~4.6 GB at 147 KB/token — more than
# the card has left after 2.3 GB of weights and the Windows desktop. Inheriting
# the model's own default is how a build that converts cleanly fails to serve.
CTX="8192"
ADAPTER_ID=""
BASE_URL="${BASE_URL:-http://localhost:8090}"
TOKEN="${TOKEN:-}"

while [ $# -gt 0 ]; do
    case "$1" in
        --name)       NAME="$2"; shift 2 ;;
        --quant)      QUANT="$2"; shift 2 ;;
        --merged)     MERGED="$2"; shift 2 ;;
        --conv)       CONV="$2"; shift 2 ;;
        --ctx)        CTX="$2"; shift 2 ;;
        --adapter-id) ADAPTER_ID="$2"; shift 2 ;;
        -h|--help)    sed -n '2,23p' "$0"; exit 0 ;;
        *) echo "unknown argument: $1"; exit 1 ;;
    esac
done

cd "$(dirname "${BASH_SOURCE[0]}")/.."   # mf-inference/

say()  { printf '\n\033[1m%s\033[0m\n' "$*"; }
fail() { echo "ERROR: $*" >&2; report failed "$*"; exit 1; }

# Progress is reported to the adapter registry when --adapter-id is given, so
# the panel's status column tracks a build running on another machine. Failures
# here are never fatal to the build itself: losing a status update must not
# throw away twenty minutes of GPU work.
report() {
    [ -n "$ADAPTER_ID" ] && [ -n "$TOKEN" ] || return 0
    local status="$1" err="${2:-}"
    curl -sf -X PATCH "$BASE_URL/admin/adapters/$ADAPTER_ID/status" \
        -H "Authorization: Bearer $TOKEN" \
        -H 'Content-Type: application/json' \
        -d "$(jq -nc --arg s "$status" --arg e "$err" --arg m "$MODEL_ID" \
              '{status:$s} + (if $e=="" then {} else {error:$e} end)
                          + (if $m=="" then {} else {mlc_model_id:$m} end)')" \
        >/dev/null || echo "  (status update failed; continuing)"
}

MODEL_ID=""
OUT="models/${NAME}-${QUANT}-MLC"

# The container sees mf-inference/models/ as /models. Deriving the in-container
# path from the host one keeps --merged working instead of silently converting
# whatever happens to sit at the hard-coded path.
case "$MERGED" in
    models/*) IN_CONTAINER="/models/${MERGED#models/}" ;;
    *) echo "ERROR: --merged must be under models/ so the container can read it" >&2; exit 1 ;;
esac

say "0/4  checks"
[ -d "$MERGED" ] || fail "$MERGED not found — run: cd peft && python3 merge_adapter.py"
command -v jq >/dev/null || fail "jq is required"
docker compose ps mlc --status running --quiet | grep -q . \
    || fail "the mlc container is not running; start it with: docker compose up -d mlc"
CID=$(docker compose ps mlc --quiet | head -1)
echo "  merged model: $MERGED"
echo "  container:    ${CID:0:12}"

# Resolved from the model being converted, not from the MLC cache.
#
# The previous version globbed the cache for a path matching "*gemma*" and fell
# back to gemma2_instruction when it found nothing. That was silently wrong for
# any other architecture, and worse than wrong when a gemma base happened to be
# cached: it then read a confident answer for a model it was not describing.
#
# config.json sits in the directory being converted and names the architecture
# outright, so there is nothing to infer and nothing to glob.
say "1/4  resolving the conversation template"
if [ -n "$CONV" ]; then
    echo "  $CONV  (given with --conv)"
else
    [ -f "$MERGED/config.json" ] || fail \
        "$MERGED/config.json is missing, so the architecture cannot be read.
     Pass the template explicitly: --conv chatml"
    ARCH=$(jq -r '.model_type // empty' "$MERGED/config.json")
    case "$ARCH" in
        # Qwen2 and Qwen3 both serve plain ChatML. Qwen3's thinking mode is a
        # template variant, not a different template: with no <think> block in
        # the turn — which is what a fine-tune on bare completions produces —
        # chatml is what the training prompt looked like.
        qwen2|qwen2_moe|qwen3|qwen3_moe|qwen35) CONV="chatml" ;;
        gemma2)                                 CONV="gemma2_instruction" ;;
        "") fail "config.json has no model_type; pass --conv explicitly" ;;
        *)  fail "no known conversation template for architecture '$ARCH'.
     Look up the name in mlc_llm/conversation_template/ and pass it:
       peft/build_mlc.sh --conv <name>" ;;
    esac
    echo "  $CONV  (from model_type=$ARCH)"
fi

report training >/dev/null 2>&1 || true
report merging

say "2/4  quantising weights to $QUANT"
docker compose exec -T mlc bash -lc "
    set -e
    rm -rf '/tmp/$NAME' && mkdir -p '/tmp/$NAME'
    python3 -m mlc_llm convert_weight "$IN_CONTAINER" \
        --quantization '$QUANT' -o '/tmp/$NAME'
" 2>&1 | tail -20 || fail "convert_weight failed"

report compiling

say "3/4  generating the serving config"
docker compose exec -T mlc bash -lc "
    set -e
    python3 -m mlc_llm gen_config "$IN_CONTAINER" \
        --quantization '$QUANT' --conv-template '$CONV' \
        --context-window-size '$CTX' -o '/tmp/$NAME'
" 2>&1 | tail -20 || fail "gen_config failed"

say "4/4  copying the build out of the container"
rm -rf "$OUT" && mkdir -p "$OUT"
docker compose cp "mlc:/tmp/$NAME/." "$OUT/" >/dev/null || fail "could not copy the build out"

[ -f "$OUT/mlc-chat-config.json" ] || fail "$OUT has no mlc-chat-config.json; the build is incomplete"
SIZE=$(du -sh "$OUT" | cut -f1)

# The id the backend must ask for. Deliberately the directory name: mlc_llm
# serve is pointed at the path, and the model field in an OpenAI-style request
# has to match what the server registered, which is the last path segment.
MODEL_ID="${NAME}-${QUANT}-MLC"
report ready

cat <<EOF

$(printf '\033[1mBUILD READY\033[0m')
  path      $OUT   ($SIZE)
  model id  $MODEL_ID
  template  $CONV
  context   $CTX tokens

Serve it — on its own, not alongside llamacpp. Both services reserve the same
card, and a 4B build plus any GGUF base does not fit in 6 GB:
  MLC_MODEL=/models/$MODEL_ID docker compose up -d --force-recreate mlc gateway

Confirm the template took before reading anything into the output. Turn markers
(<|im_start|>, <start_of_turn>) appearing in a completion mean this build has the
wrong one, and no amount of extra training data will fix that.

Then measure it. compare.py scores the rubric schema — it is the right evaluator
only for the rubric adapters:
  cd peft && python3 compare.py --after $MODEL_ID --token "\$TOKEN"
EOF
