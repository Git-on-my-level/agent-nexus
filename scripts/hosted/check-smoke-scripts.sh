#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

python3 - "$REPO_ROOT" <<'PY'
import pathlib
import re
import sys

repo = pathlib.Path(sys.argv[1])
paths = [
    repo / "scripts" / "hosted-smoke",
    repo / "scripts" / "hosted" / "test-hosted-ops.sh",
]

failures = []
for path in paths:
    text = path.read_text()
    command = []
    start_line = 0
    for line_number, line in enumerate(text.splitlines(), start=1):
        stripped = line.rstrip()
        if not command:
            start_line = line_number
        command.append(stripped)
        if stripped.endswith("\\"):
            continue

        joined = " ".join(part.rstrip("\\").strip() for part in command)
        if "curl" in joined and re.search(r"(^|\s)-X\s+POST(\s|$)", joined):
            if re.search(r'(?:"|\$[A-Z_]+/|http://[^"\s]+/|https://[^"\s]+/)threads(?:"|\s|$|\))', joined):
                failures.append(f"{path.relative_to(repo)}:{start_line}: POST /threads is not supported; use a topic-backed thread flow")
        command = []

if failures:
    print("hosted smoke script audit failed:", file=sys.stderr)
    for failure in failures:
        print(f"  {failure}", file=sys.stderr)
    sys.exit(1)
PY
