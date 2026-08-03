#!/bin/sh
#
# Start llama-server with every adapter in /models/gguf/adapters preloaded.
#
# Why a script instead of a plain `command:` in the compose file: llama-server
# takes adapters only as startup flags, and the set of adapters is not known
# when the compose file is written — it grows each time the PEFT pipeline
# publishes one. Globbing at boot is what lets a newly built adapter become
# swappable without anybody editing YAML.
#
# This is also the honest boundary of "hot-swap" here, and it is worth stating
# plainly because the two halves are often conflated:
#
#   switching between adapters already loaded  ->  instant, no restart
#                                                  (POST /lora-adapters)
#   making a brand-new adapter available       ->  restart of this container
#
# The restart is cheap — a 2B Q4 base is memory-mapped and comes back in a few
# seconds — but it is a restart, and the panel says so rather than implying the
# file appears in the running process by itself.

set -eu

MODEL="${LLAMA_MODEL:-/models/gguf/base.gguf}"
ADAPTER_DIR="${LLAMA_ADAPTER_DIR:-/models/gguf/adapters}"

if [ ! -f "$MODEL" ]; then
    echo "llamacpp: no base model at $MODEL" >&2
    echo "llamacpp: build one with peft/build_gguf.sh --base   (see peft/README.md)" >&2
    exit 1
fi

# --lora-init-without-apply loads each adapter but leaves every scale at 0, so
# the container comes up serving the untuned base. Without it llama-server
# applies all of them at once at scale 1, which would stack every adapter ever
# built on top of each other — a state no operator asked for and one that would
# silently corrupt the first request after any publish.
set -- -m "$MODEL" --lora-init-without-apply

count=0
if [ -d "$ADAPTER_DIR" ]; then
    for f in "$ADAPTER_DIR"/*.gguf; do
        [ -e "$f" ] || continue          # unmatched glob stays literal in sh
        set -- "$@" --lora "$f"
        count=$((count + 1))
    done
fi

echo "llamacpp: base=$MODEL adapters=$count"

# --ctx-size is set above what a single analysis needs (tokenBudget tops out at
# 4096 completion tokens on top of the prompt) because llama.cpp splits the
# context across parallel slots, and a context sized to exactly one request
# leaves no room for the second.
#
# The adapter control endpoints (GET/POST /lora-adapters) need no flag to be
# reachable — they exist as soon as at least one --lora is loaded.
exec /app/llama-server \
    "$@" \
    --host 0.0.0.0 \
    --port 8000 \
    --ctx-size "${LLAMA_CTX:-8192}" \
    --n-gpu-layers "${LLAMA_NGL:-99}" \
    --alias "${LLAMA_ALIAS:-qwen3-4b-instruct-gguf}"
