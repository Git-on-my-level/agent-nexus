#!/usr/bin/env python3
"""Run the agent ergonomics eval. See README.md in this directory."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from harness import report  # noqa: E402
from harness.agent import ModelConfig, ModelError, run_model, run_oracle  # noqa: E402
from harness.fixture import REPO_ROOT, FixtureError, Workspace, build_binaries  # noqa: E402
from harness.seed import EVAL_AGENT, seed  # noqa: E402
from harness.tasks import CheckContext, by_id  # noqa: E402

DEFAULT_OUT = REPO_ROOT / ".tmp" / "agent-ergonomics"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("oracle", "model"), default="oracle",
                        help="oracle replays each task's known solution (no model, no key); model runs the agent")
    parser.add_argument("--task", action="append", dest="tasks", help="task id to run; repeatable; default all")
    parser.add_argument("--repeats", type=int, default=1, help="runs per task (model mode)")
    parser.add_argument("--max-turns", type=int, default=25)
    parser.add_argument("--skill", action="store_true",
                        help="put the bundled participant skill in the agent's prompt (default: cold start)")
    parser.add_argument("--out", type=Path, default=DEFAULT_OUT)
    parser.add_argument("--compare", type=Path, help="results.json from an earlier run to show beside this one")
    parser.add_argument("--min-pass", type=int, help="exit 1 when fewer runs than this pass")
    args = parser.parse_args()

    started = datetime.now(timezone.utc)
    args.out = args.out.resolve()  # core runs from another directory; relative paths would not match
    out_dir = args.out / started.strftime("%Y%m%dT%H%M%SZ")
    out_dir.mkdir(parents=True, exist_ok=True)
    tasks = by_id(args.tasks)
    config = None
    try:
        if args.mode == "model":
            config = ModelConfig.from_env()
        cli_bin, core_bin = build_binaries(args.out / "bin")
    except (FixtureError, ModelError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    repeats = args.repeats if args.mode == "model" else 1
    meta = {
        "mode": args.mode,
        "model": config.model if config else "oracle",
        "skill": bool(args.skill),
        "max_turns": args.max_turns,
        "repeats": repeats,
        "started_at": started.isoformat(timespec="seconds"),
        "cli_version": (REPO_ROOT / "VERSION").read_text().strip(),
        "commit": _commit(),
    }
    baseline = json.loads(args.compare.read_text()) if args.compare else None
    rows: list[dict] = []
    skill_text = ""
    for task in tasks:
        for repeat in range(1, repeats + 1):
            run_dir = out_dir / f"{task.id}-{repeat}"
            run_dir.mkdir()
            ws = Workspace(run_dir, cli_bin, core_bin)
            try:
                ws.start()
                ws.register_human_and_host()
                seeded = seed(ws)
                if args.skill and not skill_text:
                    skill_text = _skill_text(ws, run_dir)
                if config is None:
                    episode = run_oracle(ws, EVAL_AGENT, task.oracle, task.oracle_answer)
                else:
                    episode = run_model(ws, EVAL_AGENT, config, task.prompt, skill_text, args.max_turns,
                                        run_dir / "transcript.jsonl")
                passed, reason = task.check(CheckContext(ws, seeded, episode.final_answer))
            except FixtureError as exc:
                print(f"error: fixture failed for {task.id}: {exc}", file=sys.stderr)
                return 2
            finally:
                ws.stop()
            rows.append(report.episode_row(task.id, repeat, len(task.oracle), passed, reason, episode))
            # Rewrite the report after every episode so a stopped run keeps what it measured.
            report.write(out_dir, report.summarize(rows, meta), baseline)
            mark = "pass" if passed else "FAIL"
            print(f"{mark}  {task.id:<20} commands={len(episode.commands):<3} "
                  f"failed={rows[-1]['failed_commands']:<2} {episode.stop_reason}  {reason}", flush=True)

    summary = report.summarize(rows, meta)
    totals = summary["totals"]
    print(f"\n{totals['passed']}/{totals['runs']} passed; {totals['failed_commands']} failed commands; "
          f"report: {out_dir / 'summary.md'}")
    if args.mode == "oracle" and totals["passed"] != totals["runs"]:
        return 1
    if args.min_pass is not None and totals["passed"] < args.min_pass:
        return 1
    return 0


def _skill_text(ws: Workspace, run_dir: Path) -> str:
    """Export the participant skill with the CLI under test, as an agent's setup would."""
    path = run_dir / "SKILL.md"
    result = ws.anx(EVAL_AGENT, ["install", "skill", "--path", str(path)])
    if result.exit_code != 0 or not path.exists():
        raise FixtureError(f"could not export the participant skill: {result.stdout[:400]}")
    return path.read_text()


def _commit() -> str:
    proc = subprocess.run(["git", "rev-parse", "--short", "HEAD"], cwd=str(REPO_ROOT), capture_output=True, text=True)
    return proc.stdout.strip() or "unknown"


if __name__ == "__main__":
    raise SystemExit(main())
