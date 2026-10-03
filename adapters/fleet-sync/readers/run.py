"""Fixed-argv process execution. Never uses a local shell.

ssh still asks the remote login shell to parse one command string. That string
is built with shlex.quote so a remote argument cannot break out. $HOME is
expanded only inside the quoted PATH script and in a double-quoted $HOME path
that already matched a strict pattern.
"""

from __future__ import annotations

import re
import shlex
import subprocess
import time
from dataclasses import dataclass

# Expanded by the remote shell when this script runs, then exec preserves
# the remaining argv without a second parse.
_PATH_SCRIPT = (
    'PATH="$HOME/.local/bin:$HOME/.bun/bin:/opt/homebrew/bin:/usr/bin:/bin" exec "$@"'
)
SSH_OPTIONS = ("-o", "BatchMode=yes", "-o", "ConnectTimeout=10")
# No leading dash: an alias must not be read as an ssh option.
_ALIAS = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._@:-]{0,127}$")
# Config values that become remote arguments. No whitespace, shell syntax,
# or a leading dash (that would look like a flag). ~/ is a token, not an
# expansion; only a $HOME/... path is expanded remotely.
_REMOTE_TOKEN = re.compile(r"^[A-Za-z0-9_./:@~+][A-Za-z0-9_./:@~+-]*$")
_HOME_PATH = re.compile(r"^\$HOME(?:/[A-Za-z0-9._~-]+)*$")
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


def valid_ssh_alias(alias: str) -> bool:
    return bool(_ALIAS.fullmatch(alias or ""))


def valid_remote_config(value: str) -> bool:
    """True for a remote path or token. $HOME is allowed only as a path prefix.

    A value that starts with `-` is rejected so it cannot be read as a flag.
    """
    text = value or ""
    if text.startswith("-"):
        return False
    return bool(_REMOTE_TOKEN.fullmatch(text) or _HOME_PATH.fullmatch(text))


def _remote_word(arg: str) -> str:
    if _HOME_PATH.fullmatch(arg):
        # Double quotes so the remote shell expands HOME and nothing else.
        return '"' + arg + '"'
    return shlex.quote(arg)


def ssh_argv(alias: str, remote_argv: list[str]) -> list[str]:
    if not valid_ssh_alias(alias):
        raise ValueError("ssh alias must be a strict token and must not start with '-'")
    if not remote_argv or any(not isinstance(arg, str) or arg == "" for arg in remote_argv):
        raise ValueError("remote command must be a list of non-empty strings")
    remote = " ".join(["sh", "-c", shlex.quote(_PATH_SCRIPT), "fleet-sync", *(_remote_word(arg) for arg in remote_argv)])
    return ["ssh", *SSH_OPTIONS, alias, remote]


class HostExec:
    """Run a fixed argv locally or over ssh. One failed probe marks the alias unreachable."""

    def __init__(self, runner: Runner | None = None):
        self.runner = runner or Runner()
        self._reach: dict[str, RunResult] = {}

    def run(self, host: dict, argv: list[str], *, timeout: float, input_text: str | None = None) -> RunResult:
        if host.get("local"):
            return self.runner(argv, timeout=timeout, input_text=input_text)
        alias = str(host.get("ssh") or "")
        try:
            probe_argv = ssh_argv(alias, ["/usr/bin/true"])
            command_argv = ssh_argv(alias, argv)
        except ValueError as exc:
            return RunResult(False, [alias], 1, "", "", str(exc))
        probe = self._reach.get(alias)
        if probe is None:
            probe = self.runner(probe_argv, timeout=15)
            self._reach[alias] = probe
        if not probe.ok:
            message = probe.error or "ssh failed"
            return RunResult(False, probe.argv, probe.code, "", "", f"ssh {alias} failed: {message}")
        return self.runner(command_argv, timeout=timeout, input_text=input_text)
