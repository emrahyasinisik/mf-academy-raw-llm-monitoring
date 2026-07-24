#!/usr/bin/env bash
#
# Publish a trained adapter to the hot-swap runtime — without merging it and
# without recompiling anything.
#
# This is the counterpart to build_mlc.sh, and the difference between the two is
# the whole reason both exist:
#
#   build_mlc.sh   merge adapter into the base -> quantise -> compile kernels
#                  ~20 minutes, produces a second complete model, and switching
#                  to it means loading a different model.
#
#   build_gguf.sh  convert the adapter alone -> a ~30 MB file
#                  seconds, produces nothing but the delta, and switching to it
#                  is one HTTP request against a server that keeps running.
#
# Both are kept. The compiled build is faster per token and is what production
# serves; this one is what makes the panel's activate button take effect while
# somebody is watching.
#
# Usage, from mf-inference/:
#   peft/build_gguf.sh --base                 # once: base model -> models/gguf/base.gguf
#   peft/build_gguf.sh --adapter ../models/adapter-v1
#   peft/build_gguf.sh --adapter ... --name tuned-v2 --adapter-id <uuid>
#
set -euo pipefail

HF_BASE="google/gemma-2-2b-it"
ADAPTER=""
NAME=""
QUANT="Q4_K_M"
DO_BASE=0
ADAPTER_ID=""
BASE_URL="${BASE_URL:-http://localhost:8090}"
TOKEN="${TOKEN:-}"
IMAGE="${LLAMACPP_TOOLS_IMAGE:-ghcr.io/ggml-org/llama.cpp:full-cuda}"

while [ $# -gt 0 ]; do
    case "$1" in
        --base)       DO_BASE=1; shift ;;
        --hf-base)    HF_BASE="$2"; shift 2 ;;
        --adapter)    ADAPTER="$2"; shift 2 ;;
        --name)       NAME="$2"; shift 2 ;;
        --quant)      QUANT="$2"; shift 2 ;;
        --adapter-id) ADAPTER_ID="$2"; shift 2 ;;
        -h|--help)    sed -n '2,30p' "$0"; exit 0 ;;
        *) echo "unknown argument: $1"; exit 1 ;;
    esac
done

cd "$(dirname "${BASH_SOURCE[0]}")/.."   # mf-inference/

say()  { printf '\n\033[1m%s\033[0m\n' "$*"; }
fail() { echo "ERROR: $*" >&2; report failed "$*"; exit 1; }

# Same contract as build_mlc.sh: a lost status update must never fail a build.
report() {
    [ -n "$ADAPTER_ID" ] && [ -n "$TOKEN" ] || return 0
    local status="$1" err="${2:-}"
    curl -sf -X PATCH "$BASE_URL/admin/adapters/$ADAPTER_ID/status" \
        -H "Authorization: Bearer $TOKEN" \
        -H 'Content-Type: application/json' \
        -d "$(jq -nc --arg s "$status" --arg e "$err" --arg g "${GGUF_ID:-}" \
              '{status:$s} + (if $e=="" then {} else {error:$e} end)
                          + (if $g=="" then {} else {gguf_adapter:$g} end)')" \
        >/dev/null || echo "  (status update failed; continuing)"
}

GGUF_ID=""
mkdir -p models/gguf/adapters

[ "$DO_BASE" = 1 ] || [ -n "$ADAPTER" ] || fail "give --base or --adapter <dir>"
command -v jq >/dev/null || fail "jq is required"
command -v python3 >/dev/null || fail "python3 is required"

# The base model has to be on disk as a directory of safetensors, because
# convert_hf_to_gguf.py reads files, not repo ids. It is already downloaded —
# training and merging both pulled it — so this resolves the existing cache
# entry rather than fetching a second copy of a 5 GB model.
say "0/3  resolving the base model in the local Hugging Face cache"
SNAPSHOT=$(python3 - "$HF_BASE" <<'PY' || true
import sys
try:
    from huggingface_hub import snapshot_download
    print(snapshot_download(sys.argv[1], local_files_only=True))
except Exception:
    pass
PY
)
[ -n "$SNAPSHOT" ] && [ -d "$SNAPSHOT" ] || fail \
    "$HF_BASE is not in the local cache. Run the training or merge step first, or:
       python3 -c \"from huggingface_hub import snapshot_download; snapshot_download('$HF_BASE')\""
echo "  $SNAPSHOT"

# One docker run per step, with the model cache and the output directory mounted.
# --entrypoint is overridden because the full image's default entrypoint is a
# task dispatcher (tools.sh) that does not expose the LoRA converter.
in_tools() {
    docker run --rm \
        -v "$SNAPSHOT:/base:ro" \
        -v "$PWD/models:/out" \
        ${ADAPTER:+-v "$(cd "$(dirname "$ADAPTER")" && pwd)/$(basename "$ADAPTER"):/adapter:ro"} \
        --entrypoint /bin/bash "$IMAGE" -lc "$1"
}

if [ "$DO_BASE" = 1 ]; then
    say "1/3  converting the base model to GGUF (f16)"
    in_tools "
        set -e
        python3 /app/convert_hf_to_gguf.py /base --outtype f16 --outfile /out/gguf/base-f16.gguf
    " 2>&1 | tail -15 || fail "convert_hf_to_gguf.py failed"

    say "2/3  quantising to $QUANT"
    # Quantised because the card holds two engines at once. f16 for a 2B is
    # ~5 GB, which alone leaves nothing for mlc; Q4_K_M is ~1.7 GB. The adapter
    # is applied on top at runtime and stays f16-ish, so the loss here is the
    # base's, not the fine-tune's.
    in_tools "
        set -e
        /app/llama-quantize /out/gguf/base-f16.gguf /out/gguf/base.gguf $QUANT
    " 2>&1 | tail -8 || fail "llama-quantize failed"

    rm -f models/gguf/base-f16.gguf
    say "3/3  done"
    echo "  models/gguf/base.gguf   ($(du -h models/gguf/base.gguf | cut -f1))"
    echo
    echo "Bring the runtime up:  docker compose up -d llamacpp"
    exit 0
fi

# ---- adapter path ----

[ -d "$ADAPTER" ] || fail "$ADAPTER is not a directory"
[ -f "$ADAPTER/adapter_config.json" ] || fail \
    "$ADAPTER has no adapter_config.json — that is a merged model, not an adapter.
     This script converts the adapter itself; use build_mlc.sh for merged models."
[ -f models/gguf/base.gguf ] || fail \
    "models/gguf/base.gguf is missing. Run: peft/build_gguf.sh --base"

# The adapter is only meaningful against the base it was trained on. Checking
# here rather than letting the converter emit tensors that load but mean nothing
# — a mismatched adapter does not error at serve time, it just degrades output,
# which is the hardest kind of failure to attribute.
TRAINED_ON=$(jq -r '.base_model_name_or_path // empty' "$ADAPTER/adapter_config.json")
if [ -n "$TRAINED_ON" ] && [ "$TRAINED_ON" != "$HF_BASE" ]; then
    fail "adapter was trained on '$TRAINED_ON' but the runtime serves '$HF_BASE'.
     Convert against the right base with --hf-base, or retrain."
fi

[ -n "$NAME" ] || NAME=$(basename "$ADAPTER")
GGUF_ID="${NAME}.gguf"
report compiling

say "1/2  converting the adapter to GGUF"
in_tools "
    set -e
    python3 /app/convert_lora_to_gguf.py /adapter --base /base --outtype f16 \
        --outfile /out/gguf/adapters/$GGUF_ID
" 2>&1 | tail -15 || fail "convert_lora_to_gguf.py failed"

[ -f "models/gguf/adapters/$GGUF_ID" ] || fail "the converter produced no file"
SIZE=$(du -h "models/gguf/adapters/$GGUF_ID" | cut -f1)
report ready

cat <<EOF

$(printf '\033[1mADAPTER PUBLISHED\033[0m')
  file  models/gguf/adapters/$GGUF_ID   ($SIZE)

llama-server takes adapters as startup flags, so this one becomes swappable
after the runtime picks it up:

  docker compose restart llamacpp

From then on switching to it is instant and needs no restart — that is what the
panel's activate button does. Confirm it is loaded:

  curl -s -H "X-API-Key: \$LLM_API_KEY" http://localhost:8080/rt/lora-adapters | jq

Measure before trusting it:
  cd peft && python3 compare.py --after $NAME --token "\$TOKEN"
EOF
