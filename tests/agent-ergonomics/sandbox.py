#!/usr/bin/env python3
"""Start a seeded workspace that an agent with a shell can dogfood.

The eval in run.py drives a model through one tool. This starts the same
seeded workspace and leaves it running, so a full coding agent (any harness)
can use the CLI the way it would in real work and say what got in its way.

    python3 tests/agent-ergonomics/sandbox.py --dir /tmp/anx-sandbox

writes into --dir:

    anx        the CLI under test, pinned to the sandbox workspace and to the
               agent identity "eval"; nothing outside the sandbox is reachable
    human      the same workspace as the person who runs it (HTTP as the human):
               ./human GET /brief, ./human POST /inbox/<id>/respond '<json>'
    SKILL.md   the participant skill this CLI ships
    PM-SKILL.md the project-manager skill

It runs until interrupted or until a file named STOP appears in --dir, then
stops core. Nothing here touches a real workspace: core listens on loopback
with a workspace under --dir, and the wrapper sets HOME to a directory there.
"""

from __future__ import annotations

import argparse
import os
import signal
import stat
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from harness.fixture import FixtureError, Workspace, build_binaries  # noqa: E402
from harness.seed import EVAL_AGENT, seed  # noqa: E402

REPO_ROOT = Path(__file__).resolve().parents[2]


def _write_executable(path: Path, text: str) -> None:
    path.write_text(text)
    path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--dir", type=Path, required=True, help="sandbox directory (created)")
    parser.add_argument("--agent", default=EVAL_AGENT, help="agent identity the anx wrapper uses")
    args = parser.parse_args()
    root = args.dir.resolve()
    root.mkdir(parents=True, exist_ok=True)
    stop_file = root / "STOP"
    stop_file.unlink(missing_ok=True)
    try:
        cli_bin, core_bin = build_binaries(REPO_ROOT / ".tmp" / "agent-ergonomics" / "bin")
        ws = Workspace(root / "run", cli_bin, core_bin)
    except FixtureError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    signal.signal(signal.SIGTERM, lambda *_: stop_file.touch())
    try:
        ws.start()
        ws.register_human_and_host()
        seed(ws)
        _write_executable(root / "anx", (
            "#!/bin/sh\n"
            "# The CLI under test, pinned to the sandbox workspace and identity.\n"
            f"HOME='{ws.home}' NO_COLOR=1 TZ=UTC ANX_UPDATE_POLICY=off exec '{cli_bin}' --base-url '{ws.base_url}' --as '{args.agent}' \"$@\"\n"
        ))
        _write_executable(root / "human", (
            "#!/bin/sh\n"
            "# Call the sandbox workspace as the person who runs it: ./human METHOD PATH [JSON]\n"
            f"exec curl -sS -X \"$1\" '{ws.base_url}'\"$2\" -H 'Authorization: Bearer {ws.human_token}' "
            "-H 'Content-Type: application/json' ${3:+-d \"$3\"}\n"
        ))
        for role, name in (("participant", "SKILL.md"), ("pm", "PM-SKILL.md")):
            (root / name).write_text((REPO_ROOT / "cli" / "skills" / f"anx-{role}" / "SKILL.md").read_text())
        print(f"sandbox ready: {root}\n  agent CLI: {root / 'anx'}\n  human API: {root / 'human'}\n"
              f"  stop: touch {stop_file}", flush=True)
        while not stop_file.exists():
            time.sleep(1)
    except KeyboardInterrupt:
        pass
    except FixtureError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    finally:
        ws.stop()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
