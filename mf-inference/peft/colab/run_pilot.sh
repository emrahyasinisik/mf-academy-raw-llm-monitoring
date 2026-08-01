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
#   colab/run_pilot.sh train        # --max-steps from the probe and the lease
#   colab/run_pilot.sh guard        # poll, and pull the adapter on sight
#   colab/run_pilot.sh eval stop
#
# `watch` prints the log once and returns; `guard` is the one that saves the
# artifact. Use watch to look, guard to leave running.
#
# DRY_RUN=1 prints the plan without touching the network.
set -euo pipefail

SESSION="${SESSION:-pilot}"
REMOTE="${REMOTE:-/content/peft}"
# The lease, not the loop. BUDGET_S used to be a hardcoded 2700 and the run
# still overran: the session was leased for 3608 s (created 12:50:13,
# terminated 13:50:21 — the keep-alive never errored, the hour simply ran out)
# and the training loop alone spent 2943 s of it. The budget is now derived
# from these five, in pilot_math.training_budget_s, and every one is measured.
LEASE_S="${LEASE_S:-3600}"      # what the free tier granted. Dynamic: it had
                                # already given 1h49m earlier the same day, so
                                # treat this as the pessimistic case.
LOAD_S="${LOAD_S:-250}"         # download + tokenise before step 1
EVAL_S="${EVAL_S:-361}"         # one pass over the eval set
EVALS="${EVALS:-2}"             # one inside train_runtime, one after it
RESERVE_S="${RESERVE_S:-180}"   # the window that has to be left for the pull
# 4, not the line's 16: four times the optimizer steps for the same row-passes,
# so the loss curve has something in it to look at.
GRAD_ACCUM="${GRAD_ACCUM:-4}"
BATCH_SIZE="${BATCH_SIZE:-1}"
SAVE_STEPS="${SAVE_STEPS:-5}"
GUARD_POLL_S="${GUARD_POLL_S:-60}"
GUARD_MAX_S="${GUARD_MAX_S:-4200}"
OUT_DIR="out/colab-pilot"   # never out/rubric-v1 — this build does not ship
EVAL_LIMIT="${EVAL_LIMIT:-40}"

cd "$(dirname "$0")/.."     # mf-inference/peft

say() { printf '\n=== %s ===\n' "$*"; }
run() {
  if [[ -n "${DRY_RUN:-}" ]]; then printf '  [dry] %s\n' "$*"; else "$@"; fi
}
# Run a heredoc of Python on the VM.
#
# Via a temp file and -f, never a pipe. `colab exec` fed on stdin hangs
# indefinitely and ignores its own --timeout: it was waiting on a read that
# never returns, and the phase sat there for eleven minutes with a T4 on the
# meter. -f is also what the CLI's own documentation calls the preferred path.
run_stdin() {
  local timeout="${1:-60}" tmp
  tmp="$(mktemp -t colab_pilot).py"
  cat > "$tmp"
  if [[ -n "${DRY_RUN:-}" ]]; then
    printf '  [dry] colab exec -s %s -f <%d bytes of python>\n' \
      "$SESSION" "$(wc -c < "$tmp")"
  else
    colab exec -s "$SESSION" -f "$tmp" --timeout "$timeout"
  fi
  rm -f "$tmp"
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

  # torchao is removed, not upgraded. peft's LoRA dispatcher asks
  # is_torchao_available() for every non-quantised Linear it wraps, and that
  # function *raises* on an incompatible version rather than returning False —
  # Colab ships 0.10.0, peft wants >0.16.0, and the fp16 arm died on it after
  # the model had loaded. Absent, the same call returns False and the ordinary
  # Linear dispatcher runs. Nothing here trains a torchao-quantised model, so
  # the dependency has no work to do; upgrading it would drag a second CUDA
  # kernel package onto an sm_75 card for a code path we never take.
  #
  # The 4-bit arm never hit this: bitsandbytes matches its Linear4bit first.
  run_stdin 300 <<'PY'
import subprocess, sys
r = subprocess.run([sys.executable, "-m", "pip", "uninstall", "-y", "torchao"],
                   capture_output=True, text=True)
print(r.stdout.strip().splitlines()[-1] if r.stdout.strip() else "torchao absent")
PY

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
    steps="$(python3 - "$LEASE_S" "$LOAD_S" "$EVAL_S" "$EVALS" "$RESERVE_S" \
                      "$BATCH_SIZE" "$GRAD_ACCUM" <<'PY'
import json, sys
sys.path.insert(0, "colab")
import pilot_math
lease, load, ev_s = float(sys.argv[1]), float(sys.argv[2]), float(sys.argv[3])
evals, reserve = int(sys.argv[4]), float(sys.argv[5])
batch, accum = int(sys.argv[6]), int(sys.argv[7])
ok = [r for r in json.load(open("out/probe.json"))["regimes"].values()
      if r["returncode"] == 0]
if not ok:
    sys.exit("both probe regimes failed — no measured cost, no honest limit")
budget = pilot_math.training_budget_s(lease, load, ev_s, evals, reserve)
print(pilot_math.compute_max_steps(budget, min(r["s_per_row"] for r in ok),
                                   batch, accum))
PY
)"
  fi
  echo "  --max-steps $steps  (lease ${LEASE_S}s less ${LOAD_S}s load, "\
"${EVALS}x${EVAL_S}s eval, ${RESERVE_S}s reserve; effective batch $((BATCH_SIZE * GRAD_ACCUM)))"

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

phase_guard() {
  say "phase: guard"
  # The adapter is written at train_qlora_qwen.py:343, *before* the final
  # trainer.evaluate() — so it lands on the VM minutes before the process
  # exits. Last run it sat there for five minutes and died with the lease,
  # because the driver was waiting for the process instead of the file.
  # Poll, and pull on sight.
  if [[ -n "${DRY_RUN:-}" ]]; then
    printf '  [dry] poll every %ss (max %ss) for %s/%s/adapter_model.safetensors, then pull\n' \
      "$GUARD_POLL_S" "$GUARD_MAX_S" "$REMOTE" "$OUT_DIR"
    return
  fi
  local waited=0 out
  while (( waited < GUARD_MAX_S )); do
    if ! out="$(run_stdin 60 <<PY
import os
print("ADAPTER_PRESENT" if os.path.exists(
    "$REMOTE/$OUT_DIR/adapter_model.safetensors") else "waiting")
PY
)"; then
      echo "  session gone before the adapter appeared (${waited}s in)" >&2
      return 1
    fi
    if grep -q ADAPTER_PRESENT <<< "$out"; then
      echo "  adapter on the VM after ${waited}s — pulling now"
      phase_pull
      return
    fi
    sleep "$GUARD_POLL_S"
    waited=$(( waited + GUARD_POLL_S ))
  done
  echo "  adapter never appeared within ${GUARD_MAX_S}s" >&2
  return 1
}

phase_pull() {
  say "phase: pull"
  mkdir -p "$OUT_DIR"
  # Tolerant per file, strict on the one that matters: guard calls this while
  # the run is still going, and train_metrics.json is only written after the
  # final eval. A missing metrics file must not abort the download of weights
  # that are already on disk and about to be deleted with the VM.
  for f in adapter_model.safetensors adapter_config.json train_metrics.json train.log; do
    run colab download -s "$SESSION" "$REMOTE/$OUT_DIR/$f" "$OUT_DIR/$f" \
      || echo "  not yet on the VM: $f"
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
  # The adapter usually already exists on the VM that trained it — but a lease
  # that ends between train and eval is the normal case, not the exception, and
  # the second session starts with an empty /content. Upload what the local
  # pull saved; rubric_eval.py reads the base from the hub and only needs the
  # PEFT pair from us.
  if [[ -z "${DRY_RUN:-}" && -s "$OUT_DIR/adapter_model.safetensors" ]]; then
    for f in adapter_model.safetensors adapter_config.json; do
      colab upload -s "$SESSION" "$OUT_DIR/$f" "$REMOTE/$OUT_DIR/$f"
    done
  fi
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
[[ "${1:-}" == "all" ]] && set -- session preflight deps push data probe train guard eval stop

for phase in "$@"; do
  case "$phase" in
    session|preflight|deps|push|data|probe|train|watch|guard|pull|eval|stop)
      "phase_$phase" ;;
    *) echo "unknown phase: $phase" >&2; exit 2 ;;
  esac
done
