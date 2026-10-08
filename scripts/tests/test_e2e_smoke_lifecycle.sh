#!/usr/bin/env bash
# Behavioral checks for the e2e-smoke trap and receipt-only stop path.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SMOKE="$ROOT/scripts/e2e-smoke"
# Job control so a backgrounded launcher is not started with SIGINT ignored.
set -m

token_still_running() {
  local receipt="$1"
  # Use the helper's own Linux /proc or macOS ps -E listing. Do not print it:
  # that listing contains other processes' environments.
  python3 - "$receipt" "$ROOT/scripts/owned_process.py" <<'PY'
import importlib.util
import json
import sys

receipt = json.load(open(sys.argv[1]))
token = receipt.get("token", "")
spec = importlib.util.spec_from_file_location("owned_process_probe", sys.argv[2])
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
found = mod._pids_with_exact_env(token)
if found is None or found:
    raise SystemExit(1)
raise SystemExit(0)
PY
}

tmp="$(mktemp -d "${TMPDIR:-/tmp}/anx-smoke-lifecycle.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

# A new session keeps the default SIGINT disposition. A background job of this
# script would inherit SIG_IGN, and bash cannot trap a signal it ignored at start.
python3 - "$SMOKE" "$tmp" <<'PY'
import os, signal, subprocess, sys, time
from pathlib import Path
smoke, tmp = sys.argv[1:]
artifact = Path(tmp) / "interrupt"
artifact.mkdir()
log_path = Path(tmp) / "interrupt.log"
env = os.environ.copy()
env["SMOKE_ARTIFACT_DIR"] = str(artifact)
env["SMOKE_LIFECYCLE_SELFTEST"] = "interrupt"
log = log_path.open("w")
proc = subprocess.Popen(["bash", smoke], env=env, start_new_session=True, stdout=log, stderr=subprocess.STDOUT)
ready = None
for _ in range(50):
    found = list(artifact.rglob("ready"))
    if found:
        ready = found[0]
        break
    time.sleep(0.1)
if ready is None:
    proc.kill()
    proc.wait(timeout=5)
    sys.stderr.write(log_path.read_text() if log_path.is_file() else "")
    raise SystemExit("interrupt selftest never became ready")
os.kill(proc.pid, signal.SIGINT)
try:
    code = proc.wait(timeout=20)
except subprocess.TimeoutExpired:
    proc.kill()
    proc.wait(timeout=5)
    sys.stderr.write("interrupt selftest did not exit after SIGINT\n")
    sys.stderr.write(log_path.read_text() if log_path.is_file() else "")
    raise SystemExit(1)
if code != 130:
    sys.stderr.write(f"expected SIGINT to exit 130, got {code}\n")
    sys.stderr.write(log_path.read_text() if log_path.is_file() else "")
    raise SystemExit(1)
PY
receipt="$(find "$tmp/interrupt" -name core-process.json -print -quit)"
if [[ -z "$receipt" ]]; then
  echo "interrupt selftest left no receipt" >&2
  find "$tmp/interrupt" -print >&2
  exit 1
fi
token_still_running "$receipt"

foreign_began="$(date +%s)"
SMOKE_ARTIFACT_DIR="$tmp/foreign" SMOKE_LIFECYCLE_SELFTEST=foreign "$SMOKE" >"$tmp/foreign.log" 2>&1
foreign_elapsed="$(( $(date +%s) - foreign_began ))"
if (( foreign_elapsed > 15 )); then
  echo "foreign stop waited ${foreign_elapsed}s without receipt proof" >&2
  exit 1
fi
state="$(find "$tmp/foreign" -name foreign-state -print -quit)"
stop_status="$(find "$tmp/foreign" -name stop-status -print -quit)"
if [[ "$(cat "$state")" != "foreign-alive" || "$(cat "$stop_status")" != "stop-failed" ]]; then
  echo "stop_supervised must refuse an unproved pid and say so (state=$(cat "$state" 2>/dev/null) status=$(cat "$stop_status" 2>/dev/null))" >&2
  cat "$tmp/foreign.log" >&2 || true
  exit 1
fi

echo "e2e-smoke lifecycle trap regressions passed"
