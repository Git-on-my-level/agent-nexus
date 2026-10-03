"""Fixed-argv process execution. Never uses a shell."""

from __future__ import annotations

import subprocess
import time
from dataclasses import dataclass

REMOTE_PATH = "PATH=$HOME/.local/bin:$HOME/.bun/bin:/opt/homebrew/bin:/usr/bin:/bin"
SSH_OPTIONS = ("-o", "BatchMode=yes", "-o", "ConnectTimeout=10")
# Whole fleet-sync run, including ssh reads and ANX writes. Per-command
# timeouts are capped by whatever budget remains so a hung ssh cannot
# outlive this.
RUN_BUDGET_SECONDS = 270


@dataclass
class RunResult:
    ok: bool
    argv: list[str]
    code: int
    stdout: str
    stderr: str
    error: str | None = None


class Runner:
    def __call__(self, argv: list[str], *, timeout: float, input_text: str | None = None) -> RunResult:
        if not argv or any(not isinstance(arg, str) for arg in argv):
            raise ValueError("commands must be a fixed argv list of strings")
        try:
            completed = subprocess.run(
                argv,
                input=input_text,
                capture_output=True,
                text=True,
                timeout=timeout,
                shell=False,
            )
        except subprocess.TimeoutExpired:
            return RunResult(False, argv, 124, "", "", f"timed out after {int(timeout)}s")
        except FileNotFoundError:
            return RunResult(False, argv, 127, "", "", f"{argv[0]} is not installed")
        except OSError as exc:
            return RunResult(False, argv, 1, "", "", str(exc))
        error = None
        if completed.returncode != 0:
            error = (completed.stderr or completed.stdout or f"exit {completed.returncode}").strip()
            error = error[:500]
        return RunResult(
            completed.returncode == 0,
            argv,
            completed.returncode,
            completed.stdout,
            completed.stderr,
            error,
        )


class Budget:
    """Monotonic deadline. timeout() returns None once less than a second remains."""

    def __init__(self, seconds: float = RUN_BUDGET_SECONDS, *, now=time.monotonic):
        self.deadline = now() + seconds
        self._now = now

    def timeout(self, requested: float) -> float | None:
        left = self.deadline - self._now()
        if left < 1:
            return None
        return min(float(requested), left)


class BudgetRunner:
    """Cap every command by the run budget. A missed deadline is a failed read, not a hang."""

    def __init__(self, inner: Runner, budget: Budget):
        self.inner = inner
        self.budget = budget

    def __call__(self, argv: list[str], *, timeout: float, input_text: str | None = None) -> RunResult:
        capped = self.budget.timeout(timeout)
        if capped is None:
            return RunResult(False, list(argv), 124, "", "", "run budget exhausted")
        return self.inner(argv, timeout=capped, input_text=input_text)


def ssh_argv(alias: str, remote_argv: list[str]) -> list[str]:
    if not alias or any(char.isspace() for char in alias):
        raise ValueError("ssh alias must be a single token")
    return ["ssh", *SSH_OPTIONS, alias, "env", REMOTE_PATH, *remote_argv]


class HostExec:
    """Run a fixed argv locally or over ssh. One failed probe marks the alias unreachable."""

    def __init__(self, runner: Runner | None = None):
        self.runner = runner or Runner()
        self._reach: dict[str, RunResult] = {}

    def run(self, host: dict, argv: list[str], *, timeout: float, input_text: str | None = None) -> RunResult:
        if host.get("local"):
            return self.runner(argv, timeout=timeout, input_text=input_text)
        alias = str(host.get("ssh") or "")
        probe = self._reach.get(alias)
        if probe is None:
            probe = self.runner(ssh_argv(alias, ["/usr/bin/true"]), timeout=15)
            self._reach[alias] = probe
        if not probe.ok:
            message = probe.error or "ssh failed"
            return RunResult(False, probe.argv, probe.code, "", "", f"ssh {alias} failed: {message}")
        return self.runner(ssh_argv(alias, argv), timeout=timeout, input_text=input_text)
