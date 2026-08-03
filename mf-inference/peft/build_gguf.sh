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
#   peft/build_gguf.sh --base --hf-base Qwen/Qwen3-4B-Instruct-2507
#                                             # once: base model -> models/gguf/base.gguf
#   peft/build_gguf.sh --adapter ../models/adapter-v1
#                                             # base read from the adapter itself
#   peft/build_gguf.sh --adapter ... --name tuned-v2 --adapter-id <uuid>
#
set -euo pipefail

# Empty by default, and resolved below from the adapter's own adapter_config.json.
# It used to be google/gemma-2-2b-it, correct on 24 Jul 2026 when every line here
# was Gemma-2-2B and stale from 28 Jul when the product moved to Qwen3-4B — that
# migration updated the model mlc serves and never came back for this script.
# Flipping it to Qwen would move the problem to the Gemma line rather than
# remove it; the adapter already records what it was fitted to.
#
# --base has no adapter to read, so that mode still requires --hf-base.
HF_BASE=""
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

# Resolve the base before anything reads it. In --adapter mode it comes from the
# adapter unless the caller overrode it; in --base mode there is no adapter, so
# it has to be given.
if [ -z "$HF_BASE" ]; then
    if [ "$DO_BASE" = 1 ]; then
        fail "--base needs --hf-base <repo id>: there is no adapter to read the
     base from, and guessing it is what this script stopped doing.
     For the current product line:  --hf-base Qwen/Qwen3-4B-Instruct-2507"
    fi
    [ -f "$ADAPTER/adapter_config.json" ] || fail \
        "$ADAPTER has no adapter_config.json, so the base cannot be read; pass --hf-base"
    HF_BASE=$(jq -r '.base_model_name_or_path // empty' "$ADAPTER/adapter_config.json")
    [ -n "$HF_BASE" ] || fail \
        "$ADAPTER/adapter_config.json records no base_model_name_or_path; pass --hf-base"
    say "base read from the adapter: $HF_BASE"
fi

# The adapter is only meaningful against the base it was trained on. A mismatch
# does not error at serve time, it just degrades output — the hardest kind of
# failure to attribute — so it is refused rather than warned about.
#
# Checked *here*, before the cache step, because this is the cheapest check in
# the file and the one that saves the most: resolving the base can end in
# "download 5 GB first", and being told to fetch a model the adapter was never
# going to match is the wrong order to learn things in. When HF_BASE came from
# the adapter above, this is trivially true and costs nothing; it earns its
# place on the --hf-base override.
if [ "$DO_BASE" != 1 ] && [ -f "$ADAPTER/adapter_config.json" ]; then
    TRAINED_ON=$(jq -r '.base_model_name_or_path // empty' "$ADAPTER/adapter_config.json")
    if [ -n "$TRAINED_ON" ] && [ "$TRAINED_ON" != "$HF_BASE" ]; then
        fail "adapter was trained on '$TRAINED_ON' but --hf-base says '$HF_BASE'.
     Convert against the right base, or retrain."
    fi
fi

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
