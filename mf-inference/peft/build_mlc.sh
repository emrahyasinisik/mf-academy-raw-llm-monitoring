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
#   peft/build_mlc.sh --adapter-id <uuid>   # also reports progress to the panel
#
set -euo pipefail

NAME="tuned-v1"
QUANT="q4f16_1"
MERGED="models/merged-fp16"
ADAPTER_ID=""
BASE_URL="${BASE_URL:-http://localhost:8090}"
TOKEN="${TOKEN:-}"

while [ $# -gt 0 ]; do
    case "$1" in
        --name)       NAME="$2"; shift 2 ;;
        --quant)      QUANT="$2"; shift 2 ;;
        --merged)     MERGED="$2"; shift 2 ;;
        --adapter-id) ADAPTER_ID="$2"; shift 2 ;;
        -h|--help)    sed -n '2,20p' "$0"; exit 0 ;;
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

# The conversation template is read from the base model MLC already cached
# rather than hard-coded. Template names have changed across MLC releases and a
# wrong one produces a build that loads and then answers with the turn markers
# visible in its output — a failure that looks like a bad fine-tune.
say "1/4  resolving the conversation template from the cached base model"
CONV=$(docker compose exec -T mlc bash -lc '
    cfg=$(find /cache -name mlc-chat-config.json -path "*gemma*" 2>/dev/null | head -1)
    [ -n "$cfg" ] && python3 -c "import json,sys;print(json.load(open(sys.argv[1]))[\"conv_template\"][\"name\"])" "$cfg" 2>/dev/null
' | tr -d '\r' | tail -1)

if [ -z "$CONV" ]; then
    CONV="gemma2_instruction"
    echo "  no cached gemma config found; falling back to $CONV"
    echo "  (if the built model echoes turn markers, this is the line to check)"
else
    echo "  $CONV  (read from the cached base build)"
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
        --quantization '$QUANT' --conv-template '$CONV' -o '/tmp/$NAME'
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

Serve it:
  MLC_MODEL=/models/$MODEL_ID docker compose up -d --force-recreate mlc

Then measure it against the base before trusting it:
  cd peft && python3 compare.py --after $MODEL_ID --token "\$TOKEN"

Do not activate it in the panel until compare.py says candidate. A build that
fixes formatting while still inventing ratings for absent criteria is worse
than no build.
EOF
