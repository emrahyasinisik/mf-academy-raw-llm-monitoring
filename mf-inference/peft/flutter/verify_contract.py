#!/usr/bin/env python3
"""Prove the Flutter adapter is being asked the question it was trained on.

The adapter learned one system prompt and one user-message layout. Both are
written down twice — once in the training set, once in the frontend that talks
to the served model — and nothing in the build makes the two agree. This script
is that missing half.

It checks three things, in the order they cost:

  1. system prompt   The frontend pins a sha256 (SYSTEM_PROMPT_SHA256). Until
                     this script existed the pin had nothing to compare against,
                     because the dataset lived only on Kaggle. Now it compares.

  2. line labels     Every label the frontend can emit must appear in the data,
                     and every label the data contains should be reachable from
                     the frontend. A label in the data that the UI cannot
                     produce is a trained capability nobody can invoke.

  3. State values    The frontend sends a closed set of State strings. Each one
                     must appear verbatim in the training set. This is the check
                     that fails loudest today: all three drifted, and the model
                     has been answering off-distribution prompts in production
                     without any symptom a reader would notice.

Run it whenever either side changes:

    python3 verify_contract.py                   # against data/…_v7.jsonl
    python3 verify_contract.py --dataset data/flutter_screens_train_v8.jsonl

Exit status is 1 on any mismatch, so it can gate a build.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_DATASET = os.path.join(HERE, "data", "flutter_screens_train_v7.jsonl")
PROMPT_FILE = os.path.join(HERE, "system_prompt.txt")
CONTRACT_TS = os.path.normpath(
    os.path.join(HERE, "..", "..", "..", "mf-frontend", "src", "lib", "flutterContract.ts"))


def load_rows(path: str) -> list[dict]:
    if not os.path.exists(path):
        sys.exit(f"dataset not found: {path}\n"
                 f"data/ is gitignored — run ./fetch_dataset.sh to pull it from Kaggle.")
    with open(path, encoding="utf-8") as fh:
        return [json.loads(line) for line in fh if line.strip()]


def read_prompt_file() -> str:
    """The single source of truth for the system prompt.

    Trailing newlines are stripped rather than preserved: the prompt is one line
    and every editor appends a newline to a text file, so keeping it would make
    the file unable to round-trip through an editor without changing its hash —
    which is the exact failure this file exists to prevent.
    """
    with open(PROMPT_FILE, encoding="utf-8") as fh:
        return fh.read().rstrip("\n")


def frontend_state_wires() -> list[str]:
    """The State strings the UI can send, read out of flutterContract.ts.

    Parsed from the TypeScript rather than duplicated here, because a copy would
    be one more thing to drift — and drift between these two files is precisely
    what the script is looking for.

    buildBrief appends a period to the wire ("State: ${state.wire}."), so the
    period is added here too. That trailing character is not cosmetic: it is
    part of the string the model matches against.
    """
    if not os.path.exists(CONTRACT_TS):
        return []
    src = open(CONTRACT_TS, encoding="utf-8").read()
    block = re.search(r"STATE_CHOICES\s*=\s*\[(.*?)\]\s*as const", src, re.S)
    if not block:
        return []
    return [w + "." for w in re.findall(r'wire:\s*"([^"]+)"', block.group(1))]


def frontend_sha() -> str | None:
    if not os.path.exists(CONTRACT_TS):
        return None
    src = open(CONTRACT_TS, encoding="utf-8").read()
    m = re.search(r'SYSTEM_PROMPT_SHA256\s*=\s*\n?\s*"([0-9a-f]{64})"', src)
    return m.group(1) if m else None


EVIDENCE_HEADER = "KAYNAKLAR"

# A brief line is `Label: value` with a short, capitalised label. Anchoring on the
# shape matters from v8 on: the evidence block is prose, and prose contains
# colons, so splitting every line on ":" reports half the corpus as a label.
BRIEF_LINE = re.compile(r"^([A-ZÇĞİÖŞÜ][\wÇĞİÖŞÜçğıöşü/]{0,20}):")


def brief_of(message: str) -> str:
    """The brief, without the evidence block or the turn instruction.

    Sections are separated by blank lines and assembled in that order, so this
    holds for v7 (brief only) and v8 (evidence, brief, instruction) alike.
    """
    chunks = [c for c in message.split("\n\n") if c.strip()]
    if not chunks:
        return ""
    if chunks[0].startswith(EVIDENCE_HEADER):
        return chunks[1] if len(chunks) > 1 else ""
    return chunks[0]


def labels_of(message: str) -> list[str]:
    out = []
    for line in brief_of(message).split("\n"):
        m = BRIEF_LINE.match(line)
        if m:
            out.append(m.group(1) + ":")
    return out


def state_of(message: str) -> str:
    for line in brief_of(message).split("\n"):
        if line.startswith("State:"):
            return line.split(":", 1)[1].strip()
    return ""


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--dataset", default=DEFAULT_DATASET)
    args = ap.parse_args()

    rows = load_rows(args.dataset)
    users = [r["messages"][1]["content"] for r in rows]
    failures: list[str] = []

    print(f"dataset: {os.path.basename(args.dataset)} ({len(rows)} rows)\n")

    # --- 1. system prompt ---------------------------------------------------
    data_prompts = {r["messages"][0]["content"] for r in rows}
    if len(data_prompts) != 1:
        failures.append(f"dataset has {len(data_prompts)} distinct system prompts; expected 1")
    data_prompt = sorted(data_prompts)[0]
    data_sha = hashlib.sha256(data_prompt.encode()).hexdigest()
    file_sha = hashlib.sha256(read_prompt_file().encode()).hexdigest()
    ts_sha = frontend_sha()

    print("system prompt")
    print(f"  dataset            {data_sha}")
    print(f"  system_prompt.txt  {file_sha}  {'ok' if file_sha == data_sha else 'MISMATCH'}")
    print(f"  flutterContract.ts {ts_sha}  {'ok' if ts_sha == data_sha else 'MISMATCH'}")
    if file_sha != data_sha:
        failures.append("system_prompt.txt does not match the dataset")
    if ts_sha != data_sha:
        failures.append("SYSTEM_PROMPT_SHA256 in flutterContract.ts does not match the dataset")

    # --- 2. line labels -----------------------------------------------------
    seen: dict[str, int] = {}
    for m in users:
        for lab in labels_of(m):
            seen[lab] = seen.get(lab, 0) + 1
    # What buildBrief can emit. Hard-coded because it is assembled from literals
    # in the TypeScript rather than declared as data there. `Bileşen:` joined the
    # set when SUBJECT_KINDS landed; before that it was 32 trained rows the form
    # had no way to ask for.
    emitted = {"Ekran:", "Bileşen:", "Açıklama:", "Alanlar/İçerik:", "State:"}

    print("\nline labels")
    for lab, n in sorted(seen.items(), key=lambda kv: -kv[1]):
        mark = "ok" if lab in emitted else "UNREACHABLE from the UI"
        print(f"  {lab:18s} {n:4d}x  {mark}")
    for lab in sorted(emitted - set(seen)):
        print(f"  {lab:18s}    0x  NOT IN DATA — the UI sends a label never trained on")
        failures.append(f"UI emits {lab!r}, absent from the training set")

    # --- 3. State values ----------------------------------------------------
    states = [state_of(m) for m in users]
    wires = frontend_state_wires()
    print("\nState values the UI can send")
    if not wires:
        print("  (could not read STATE_CHOICES from flutterContract.ts)")
    for w in wires:
        n = states.count(w)
        print(f"  {w!r:34s} {n:4d}x  {'ok' if n else 'NOT IN DATA'}")
        if not n:
            failures.append(f"UI State value {w!r} never appears in the training set")

    # --- verdict ------------------------------------------------------------
    print()
    if failures:
        print(f"FAIL — {len(failures)} mismatch(es):")
        for f in failures:
            print(f"  - {f}")
        sys.exit(1)
    print("OK — the served contract matches the trained one.")


if __name__ == "__main__":
    main()
