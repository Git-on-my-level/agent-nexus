"""Turn episodes into results.json and a readable summary.

The summary answers two questions: did agents get the work done, and what
did the CLI cost them on the way. The friction list is the part to act on:
every command that failed, grouped by what the agent tried.
"""

from __future__ import annotations

import json
import statistics
from collections import Counter
from dataclasses import asdict
from pathlib import Path

from .agent import Episode

HELP_WORDS = {"help", "--help", "-h", "concepts"}


def episode_row(task_id: str, repeat: int, oracle_commands: int, passed: bool, reason: str,
                episode: Episode) -> dict:
    commands = episode.commands
    return {
        "task": task_id,
        "repeat": repeat,
        "passed": passed,
        "check": reason,
        "stop_reason": episode.stop_reason,
        "error": episode.error,
        "turns": episode.turns,
        "commands": len(commands),
        "oracle_commands": oracle_commands,
        "failed_commands": sum(1 for c in commands if c.exit_code != 0),
        "help_commands": sum(1 for c in commands if c.args and (c.args[0] in HELP_WORDS or c.args[-1] in HELP_WORDS)),
        "truncated_outputs": sum(1 for c in commands if c.truncated),
        "cli_output_chars": sum(c.output_chars for c in commands),
        "prompt_tokens": episode.prompt_tokens,
        "completion_tokens": episode.completion_tokens,
        "seconds": round(episode.seconds, 1),
        "final_answer": episode.final_answer[:600],
        "command_log": [asdict(c) for c in commands],
    }


def summarize(rows: list[dict], meta: dict) -> dict:
    by_task: dict[str, list[dict]] = {}
    for row in rows:
        by_task.setdefault(row["task"], []).append(row)
    tasks = []
    for task_id, group in by_task.items():
        tasks.append({
            "task": task_id,
            "runs": len(group),
            "passed": sum(1 for r in group if r["passed"]),
            "commands_median": statistics.median(r["commands"] for r in group),
            "oracle_commands": group[0]["oracle_commands"],
            "failed_commands": sum(r["failed_commands"] for r in group),
            "help_commands": sum(r["help_commands"] for r in group),
            "cli_output_chars_median": statistics.median(r["cli_output_chars"] for r in group),
            "prompt_tokens_median": statistics.median(r["prompt_tokens"] for r in group),
        })
    total_commands = sum(r["commands"] for r in rows)
    friction = Counter()
    examples: dict[tuple[str, str], str] = {}
    for row in rows:
        for command in row["command_log"]:
            if command["exit_code"] == 0:
                continue
            key = (_command_path(command["args"]), command["error_code"] or f"exit {command['exit_code']}")
            friction[key] += 1
            examples.setdefault(key, command["error_line"])
    return {
        "meta": meta,
        "totals": {
            "runs": len(rows),
            "passed": sum(1 for r in rows if r["passed"]),
            "commands": total_commands,
            "oracle_commands": sum(r["oracle_commands"] for r in rows),
            "failed_commands": sum(r["failed_commands"] for r in rows),
            "help_commands": sum(r["help_commands"] for r in rows),
            "truncated_outputs": sum(r["truncated_outputs"] for r in rows),
            "cli_output_chars": sum(r["cli_output_chars"] for r in rows),
            "prompt_tokens": sum(r["prompt_tokens"] for r in rows),
            "completion_tokens": sum(r["completion_tokens"] for r in rows),
            "hit_turn_limit": sum(1 for r in rows if r["stop_reason"] == "max_turns"),
            "model_errors": sum(1 for r in rows if r["stop_reason"] == "model_error"),
        },
        "tasks": tasks,
        "friction": [
            {"command": path, "error": code, "count": count, "example": examples[(path, code)]}
            for (path, code), count in friction.most_common()
        ],
        "rows": rows,
    }


def _command_path(args: list[str]) -> str:
    """The command words before the first flag or ref-like argument, e.g. `plan step add`."""
    words = []
    for arg in args:
        if arg.startswith("-") or ":" in arg or " " in arg or len(words) == 3:
            break
        words.append(arg)
    return " ".join(words) or "(none)"


def render_markdown(summary: dict, baseline: dict | None = None) -> str:
    totals, meta = summary["totals"], summary["meta"]
    runs = totals["runs"] or 1
    lines = [
        "# Agent ergonomics eval",
        "",
        f"- Model: `{meta.get('model')}`; mode: {meta.get('mode')}; skill in prompt: {meta.get('skill')}",
        f"- CLI version: {meta.get('cli_version')}; commit: `{meta.get('commit')}`; started {meta.get('started_at')}",
        "",
        "## Totals",
        "",
        "| Measure | This run |" + (" Baseline |" if baseline else ""),
        "| --- | ---: |" + (" ---: |" if baseline else ""),
    ]
    base_totals = (baseline or {}).get("totals") or {}

    def total_row(label: str, value: str, base_value: str | None) -> None:
        lines.append(f"| {label} | {value} |" + (f" {base_value} |" if baseline else ""))

    def ratio(numerator: float, denominator: float) -> str:
        return f"{numerator / denominator:.2f}" if denominator else "n/a"

    def passed(t: dict) -> str:
        return f"{t.get('passed', 0)}/{t.get('runs', 0)}" if t else "n/a"

    total_row("Tasks passed", passed(totals), passed(base_totals))
    total_row("Commands per oracle command", ratio(totals["commands"], totals["oracle_commands"]),
              ratio(base_totals.get("commands", 0), base_totals.get("oracle_commands", 0)))
    total_row("Failed commands", str(totals["failed_commands"]), str(base_totals.get("failed_commands", "n/a")))
    total_row("Help lookups", str(totals["help_commands"]), str(base_totals.get("help_commands", "n/a")))
    total_row("CLI output read per run (chars)", f"{totals['cli_output_chars'] // runs:,}",
              f"{base_totals.get('cli_output_chars', 0) // (base_totals.get('runs') or 1):,}")
    total_row("Prompt tokens per run", f"{totals['prompt_tokens'] // runs:,}",
              f"{base_totals.get('prompt_tokens', 0) // (base_totals.get('runs') or 1):,}")
    total_row("Outputs cut for length", str(totals["truncated_outputs"]), str(base_totals.get("truncated_outputs", "n/a")))
    total_row("Runs that hit the turn limit", str(totals["hit_turn_limit"]), str(base_totals.get("hit_turn_limit", "n/a")))
    if totals["model_errors"]:
        lines += ["", f"**{totals['model_errors']} run(s) ended on a model API error and say nothing about the CLI.**"]
    lines += ["", "## By task", "",
              "| Task | Passed | Commands (median) | Oracle | Failed | Help | CLI output chars (median) |",
              "| --- | ---: | ---: | ---: | ---: | ---: | ---: |"]
    for task in summary["tasks"]:
        lines.append(f"| {task['task']} | {task['passed']}/{task['runs']} | {task['commands_median']:g} | "
                     f"{task['oracle_commands']} | {task['failed_commands']} | {task['help_commands']} | "
                     f"{int(task['cli_output_chars_median']):,} |")
    lines += ["", "## Friction: commands that failed", ""]
    if not summary["friction"]:
        lines.append("None.")
    else:
        lines += ["| Command | Error | Times | Example |", "| --- | --- | ---: | --- |"]
        for item in summary["friction"][:40]:
            example = item["example"].replace("|", "\\|").replace("\n", " ")[:160]
            lines.append(f"| `anx {item['command']}` | {item['error']} | {item['count']} | {example} |")
    failed = [r for r in summary["rows"] if not r["passed"]]
    if failed:
        lines += ["", "## Runs that did not pass", ""]
        for row in failed:
            lines.append(f"- **{row['task']}** (repeat {row['repeat']}, {row['stop_reason']}): {row['check']}"
                         + (f" Error: {row['error']}" if row["error"] else ""))
    return "\n".join(lines) + "\n"


def write(out_dir: Path, summary: dict, baseline: dict | None) -> None:
    (out_dir / "results.json").write_text(json.dumps(summary, indent=2) + "\n")
    (out_dir / "summary.md").write_text(render_markdown(summary, baseline))
