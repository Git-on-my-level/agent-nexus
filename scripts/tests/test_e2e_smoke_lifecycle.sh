#!/usr/bin/env bash
# Behavioral checks for the e2e-smoke trap and receipt-only stop path.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SMOKE="$ROOT/scripts/e2e-smoke"
# Job control so a backgrounded launcher is not started with SIGINT ignored.
set -m

token_still_running() {
  local receipt="$1"
  python3 - "$receipt" <<'PY'
import json, subprocess, sys
receipt = json.load(open(sys.argv[1]))
token = receipt.get("token", "")
field = f"OWNED_PROCESS_TOKEN={token}".encode()
out = subprocess.check_output(["ps", "-axww", "-E", "-o", "pid=", "-o", "command="])
for line in out.splitlines():
    stripped = line.strip()
    if not stripped:
        continue
    pid_text, sep, rest = stripped.partition(b" ")
    if not sep or not pid_text.isdigit():
        continue
    padded = b" " + rest + b" "
    if b" " + field + b" " in padded:
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
