#!/usr/bin/env bash
# Driver checks that do not rent a VM: syntax, and that every documented phase
# actually dispatches. A typo in the case arm is otherwise found with a GPU
# already running and the clock already spending.
set -uo pipefail

cd "$(dirname "$0")"
fail=0

check() {
  if eval "$2" >/dev/null 2>&1; then
    echo "ok   $1"
  else
    echo "FAIL $1"
    fail=1
  fi
}

check "bash syntax" "bash -n run_pilot.sh"
check "executable"  "test -x run_pilot.sh"

out=$(DRY_RUN=1 ./run_pilot.sh all 2>&1)

for phase in session preflight deps push data probe train guard eval stop; do
  check "phase '$phase' dispatches" "grep -q \"phase: $phase\" <<< \"\$out\""
done
# watch and pull are not in 'all' — guard supersedes them there — but both must
# still dispatch on their own.
for phase in watch pull; do
  check "phase '$phase' dispatches" \
    "grep -q \"phase: $phase\" <<< \"\$(DRY_RUN=1 ./run_pilot.sh $phase 2>&1)\""
done

check "never calls colab pay"        "! grep -q 'colab pay' run_pilot.sh"
check "asks for a T4"                "grep -q -- '--gpu T4' run_pilot.sh"
check "pilot out-dir, not rubric-v1" "grep -q 'out/colab-pilot' run_pilot.sh"
check "training runs detached"       "grep -q 'start_new_session' run_pilot.sh"
check "stops the session"            "grep -q 'colab stop' run_pilot.sh"
check "unknown phase is an error"    "! ./run_pilot.sh nonsense >/dev/null 2>&1"
# The two failures of the first run, as checks.
check "budget comes from the lease"  "! grep -q 'BUDGET_S:-2700' run_pilot.sh"
check "lease is in the arithmetic"   "grep -q 'training_budget_s' run_pilot.sh"
check "both evals are charged"       "grep -q 'EVALS:-2' run_pilot.sh"
check "guard pulls on sight"         "grep -q 'ADAPTER_PRESENT' run_pilot.sh"
check "'all' guards before eval"     "grep -qE 'train guard eval stop' run_pilot.sh"

exit $fail
