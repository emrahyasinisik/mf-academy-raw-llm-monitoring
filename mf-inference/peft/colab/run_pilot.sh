#!/usr/bin/env bash
# Mac-side driver for the Colab pilot. The VM executes; this decides.
#
# Phases rather than one command, for two reasons the free tier imposes:
#   * `colab exec` times out after 30 s by default, so training cannot block
#     inside it — it is detached on the VM and polled from here.
#   * a dynamic quota can cut the session at any point; every phase is
#     separately re-runnable against a fresh session.
#
# Usage, from mf-inference/peft/:
#   colab/run_pilot.sh session      # rent the T4
#   colab/run_pilot.sh preflight    # card + library gate, before any download
#   colab/run_pilot.sh deps push data
#   colab/run_pilot.sh probe        # ~30 min; writes out/probe.json
#   colab/run_pilot.sh train        # derives --max-steps from the probe
#   colab/run_pilot.sh watch        # poll until the adapter is written
#   colab/run_pilot.sh pull eval stop
#
# DRY_RUN=1 prints the plan without touching the network.
set -euo pipefail

SESSION="${SESSION:-pilot}"
REMOTE="${REMOTE:-/content/peft}"
# The design's arithmetic: an hour minus model download, tokenisation, eval and
# checkpointing. This is the only number the pilot promises to hit.
BUDGET_S="${BUDGET_S:-2700}"
# 4, not the line's 16: four times the optimizer steps for the same row-passes,
# so the loss curve has something in it to look at.
GRAD_ACCUM="${GRAD_ACCUM:-4}"
BATCH_SIZE="${BATCH_SIZE:-1}"
SAVE_STEPS="${SAVE_STEPS:-5}"
OUT_DIR="out/colab-pilot"   # never out/rubric-v1 — this build does not ship
EVAL_LIMIT="${EVAL_LIMIT:-40}"

cd "$(dirname "$0")/.."     # mf-inference/peft

say() { printf '\n=== %s ===\n' "$*"; }
run() {
  if [[ -n "${DRY_RUN:-}" ]]; then printf '  [dry] %s\n' "$*"; else "$@"; fi
}
# Heredoc-fed exec: `run` cannot carry stdin through, so dry-run prints instead.
run_stdin() {
  local code
  code="$(cat)"
  if [[ -n "${DRY_RUN:-}" ]]; then
    printf '  [dry] colab exec -s %s <<< %d bytes of python\n' "$SESSION" "${#code}"
  else
    printf '%s' "$code" | colab exec -s "$SESSION" --timeout "${1:-60}"
  fi
}

phase_session() {
  say "phase: session"
  # An unrecognised --gpu value silently falls back to A100, which then fails
  # the next step; T4 is spelled exactly as the CLI lists it.
  run colab new -s "$SESSION" --gpu T4
  run colab status -s "$SESSION"
}

phase_preflight() {
  say "phase: preflight"
  run colab exec -s "$SESSION" -f colab/preflight.py --timeout 120
}

phase_deps() {
  say "phase: deps"
  run colab install -s "$SESSION" -r colab/requirements-colab.txt
  # Again after installing: the gate is only worth anything on the versions
  # that will actually load the model.
  run colab exec -s "$SESSION" -f colab/preflight.py --timeout 120
}

phase_push() {
  say "phase: push"
  # Uploaded, not cloned: the pilot's own code is being written in the same
  # hours it runs, and a clone would run whatever was last pushed. The price is
  # that the run has no SHA of its own, so the manifest records one.
  run_stdin 60 <<PY
import os
for d in ("$REMOTE/data/pilot", "$REMOTE/colab", "$REMOTE/out", "/content/out"):
    os.makedirs(d, exist_ok=True)
print("dirs ready")
PY
  for f in train_qlora_qwen.py rubric_eval.py \
           colab/pilot_math.py colab/probe_step_cost.py colab/preflight.py; do
    run colab upload -s "$SESSION" "$f" "$REMOTE/$f"
  done

  mkdir -p out
  local sha dirty
  sha="$(git rev-parse HEAD)"
  dirty="$(git status --porcelain -- . | wc -l | tr -d ' ')"
  printf '{"sha":"%s","dirty_files":%s,"session":"%s","when":"%s"}\n' \
    "$sha" "$dirty" "$SESSION" "$(date -u +%FT%TZ)" > out/run_manifest.json
  echo "  manifest: $sha (${dirty} dirty file(s) under mf-inference/peft)"
  [[ "$dirty" == "0" ]] || echo "  NOTE: uncommitted changes — this run is not reproducible from a commit"
}

phase_data() {
  say "phase: data"
  for f in rubric_train.jsonl rubric_eval.jsonl; do
    if [[ -z "${DRY_RUN:-}" && ! -f "data/pilot/$f" ]]; then
      echo "data/pilot/$f missing — generate the pilot set first (plan Task 6)." >&2
      echo "Do NOT omit --out-dir data/pilot: the default writes data/rubric_train.jsonl," >&2
      echo "which is the full set, and data/ is gitignored." >&2
      exit 1
    fi
    run colab upload -s "$SESSION" "data/pilot/$f" "$REMOTE/data/pilot/$f"
  done
}

phase_probe() {
  say "phase: probe"
  # Two regimes, each paying a full model load: generous timeout. The only
  # phase whose output the next one reads.
  run colab exec -s "$SESSION" -f colab/probe_step_cost.py --timeout 3000
  run colab download -s "$SESSION" /content/out/probe.json out/probe.json
  [[ -n "${DRY_RUN:-}" ]] || python3 - <<'PY'
import json
for name, r in json.load(open("out/probe.json"))["regimes"].items():
    print(f"  {name:5} exit {r['returncode']}  {r['s_per_row']:6.1f} s/row  "
          f"peak {r['peak_mib']} MiB")
PY
}

phase_train() {
  say "phase: train"
  local steps
  if [[ -n "${DRY_RUN:-}" ]]; then
    steps="<derived from out/probe.json>"
  else
    steps="$(python3 - "$BUDGET_S" "$BATCH_SIZE" "$GRAD_ACCUM" <<'PY'
import json, sys
sys.path.insert(0, "colab")
import pilot_math
budget, batch, accum = float(sys.argv[1]), int(sys.argv[2]), int(sys.argv[3])
ok = [r for r in json.load(open("out/probe.json"))["regimes"].values()
      if r["returncode"] == 0]
if not ok:
    sys.exit("both probe regimes failed — no measured cost, no honest limit")
print(pilot_math.compute_max_steps(budget, min(r["s_per_row"] for r in ok),
                                   batch, accum))
PY
)"
  fi
  echo "  --max-steps $steps  (budget ${BUDGET_S}s, effective batch $((BATCH_SIZE * GRAD_ACCUM)))"

  # start_new_session=True: `colab exec` returns within 30 s, and a plain child
  # of the kernel would go down with the websocket.
  run_stdin 60 <<PY
import subprocess, os
os.makedirs("$REMOTE/$OUT_DIR", exist_ok=True)
log = open("$REMOTE/$OUT_DIR/train.log", "w")
p = subprocess.Popen(
    ["python3", "train_qlora_qwen.py",
     "--train", "data/pilot/rubric_train.jsonl",
     "--eval", "data/pilot/rubric_eval.jsonl",
     "--out-dir", "$OUT_DIR",
     "--batch-size", "$BATCH_SIZE",
     "--grad-accum", "$GRAD_ACCUM",
     "--max-steps", "$steps",
     "--save-steps", "$SAVE_STEPS"],
    cwd="$REMOTE", stdout=log, stderr=subprocess.STDOUT,
    start_new_session=True)
print("training pid", p.pid)
PY
}

phase_watch() {
  say "phase: watch"
  run_stdin 60 <<PY
import glob, os, subprocess
d = "$REMOTE/$OUT_DIR"
print(subprocess.run(["tail", "-n", "25", os.path.join(d, "train.log")],
                     capture_output=True, text=True).stdout)
print("checkpoints:", sorted(os.path.basename(p)
                             for p in glob.glob(os.path.join(d, "checkpoint-*"))))
print("adapter written:", os.path.exists(
    os.path.join(d, "adapter_model.safetensors")))
print("still running:", bool(subprocess.run(
    ["pgrep", "-f", "train_qlora_qwen.py"], capture_output=True).stdout))
PY
}

phase_pull() {
  say "phase: pull"
  mkdir -p "$OUT_DIR"
  for f in adapter_model.safetensors adapter_config.json train_metrics.json train.log; do
    run colab download -s "$SESSION" "$REMOTE/$OUT_DIR/$f" "$OUT_DIR/$f"
  done
  # Exit code 0 is not the check. A Kaggle run once recorded COMPLETE over an
  # empty output directory, because `!`'s exit code went nowhere.
  if [[ -z "${DRY_RUN:-}" && ! -s "$OUT_DIR/adapter_model.safetensors" ]]; then
    echo "adapter_model.safetensors is missing or empty — the run did not finish" >&2
    exit 1
  fi
}

phase_eval() {
  say "phase: eval"
  # Base and adapter in one process, one session, one set of library versions —
  # which was the whole point of splitting eval out. rubric_eval.py does both
  # and prints the delta itself.
  run_stdin 60 <<PY
import subprocess
p = subprocess.Popen(
    ["python3", "rubric_eval.py",
     "--data", "data/pilot/rubric_eval.jsonl",
     "--limit", "$EVAL_LIMIT",
     "--adapter", "$OUT_DIR",
     "--out", "out/pilot_eval.json"],
    cwd="$REMOTE", stdout=open("$REMOTE/out/eval.log", "w"),
    stderr=subprocess.STDOUT, start_new_session=True)
print("eval pid", p.pid)
PY
  echo "  poll: colab exec -s $SESSION --timeout 60 <<< \"print(open('$REMOTE/out/eval.log').read()[-3000:])\""
  echo "  then: colab download -s $SESSION $REMOTE/out/pilot_eval.json out/pilot_eval.json"
}

phase_stop() {
  say "phase: stop"
  # An unstopped session keeps a VM assigned against the quota that is this
  # whole exercise's only constraint.
  run colab stop -s "$SESSION"
}

[[ $# -gt 0 ]] || { echo "usage: $0 <phase>...  (or 'all')" >&2; exit 2; }
[[ "${1:-}" == "all" ]] && set -- session preflight deps push data probe train watch pull eval stop

for phase in "$@"; do
  case "$phase" in
    session|preflight|deps|push|data|probe|train|watch|pull|eval|stop)
      "phase_$phase" ;;
    *) echo "unknown phase: $phase" >&2; exit 2 ;;
  esac
done
