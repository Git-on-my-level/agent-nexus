"""Disposable local workspace for the agent ergonomics eval.

Builds the CLI and core from this checkout, starts core on a free loopback
port under scripts/owned_process.py, registers one human through the dev
passkey route and enrolls one host, so `anx --as <name>` works for any agent
name. Nothing outside the run directory is read or written.
"""

from __future__ import annotations

import json
import os
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[3]
OWNED_PROCESS = REPO_ROOT / "scripts" / "owned_process.py"
BOOTSTRAP_TOKEN = "ergonomics-eval-bootstrap-token"
SHUTDOWN_DEADLINE_SECONDS = 15


class FixtureError(RuntimeError):
    pass


@dataclass
class CommandResult:
    argv: list[str]
    exit_code: int
    stdout: str
    stderr: str
    duration_ms: int

    def json(self) -> dict:
        try:
            return json.loads(self.stdout)
        except json.JSONDecodeError as exc:
            raise FixtureError(f"not JSON from {' '.join(self.argv)}: {self.stdout[:400]}") from exc


@dataclass
class Workspace:
    run_dir: Path
    cli_bin: Path
    core_bin: Path
    base_url: str = ""
    human_token: str = ""
    receipt: Path = field(init=False)
    supervisor: subprocess.Popen | None = None

    def __post_init__(self) -> None:
        self.receipt = self.run_dir / "core-process.json"
        self.home = self.run_dir / "home"
        self.home.mkdir(parents=True, exist_ok=True)

    # --- lifecycle -------------------------------------------------------

    def start(self) -> None:
        port = _free_port()
        self.base_url = f"http://127.0.0.1:{port}"
        workspace_root = self.run_dir / "workspace"
        workspace_root.mkdir(parents=True, exist_ok=True)
        (workspace_root / ".anx-dev-insecure-auth").touch()
        env = dict(os.environ)
        env.update(
            ANX_BOOTSTRAP_TOKEN=BOOTSTRAP_TOKEN,
            ANX_ALLOW_PASSKEY_DEV_BYPASS="1",
            ANX_HOSTED_DEV_MODE="1",
            ANX_ENABLE_DEV_ACTOR_MODE="1",
        )
        log = open(self.run_dir / "core.log", "wb")
        self.supervisor = subprocess.Popen(
            [
                sys.executable, str(OWNED_PROCESS), "supervise",
                "--receipt", str(self.receipt),
                "--label", "anx-ergonomics-eval-core",
                "--shutdown-deadline", str(SHUTDOWN_DEADLINE_SECONDS),
                "--cwd", str(REPO_ROOT / "core"),
                "--",
                str(self.core_bin),
                "--listen-addr", f"127.0.0.1:{port}",
                "--workspace-root", str(workspace_root),
                "--schema-path", str(REPO_ROOT / "contracts" / "anx-schema.yaml"),
            ],
            env=env, stdout=log, stderr=subprocess.STDOUT,
        )
        self._wait_ready()

    def stop(self) -> None:
        if self.receipt.exists():
            subprocess.run(
                [sys.executable, str(OWNED_PROCESS), "stop",
                 "--receipt", str(self.receipt),
                 "--shutdown-deadline", str(SHUTDOWN_DEADLINE_SECONDS)],
                check=False, capture_output=True,
            )
        if self.supervisor is not None:
            try:
                self.supervisor.wait(timeout=SHUTDOWN_DEADLINE_SECONDS + 5)
            except subprocess.TimeoutExpired:
                pass

    def _wait_ready(self) -> None:
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            if self.supervisor is not None and self.supervisor.poll() is not None:
                raise FixtureError(f"core exited early; see {self.run_dir / 'core.log'}")
            try:
                with urllib.request.urlopen(self.base_url + "/readyz", timeout=2) as resp:
                    if resp.status == 200:
                        return
            except (urllib.error.URLError, OSError):
                pass
            time.sleep(0.15)
        raise FixtureError(f"core not ready after 60s; see {self.run_dir / 'core.log'}")

    # --- identities ------------------------------------------------------

    def register_human_and_host(self) -> None:
        human = self.http("POST", "/auth/passkey/dev/register", {
            "display_name": "Eval operator", "bootstrap_token": BOOTSTRAP_TOKEN,
        }, auth=False)
        self.human_token = human["tokens"]["access_token"]
        expires = (datetime.now(timezone.utc) + timedelta(minutes=30)).isoformat()
        token = self.http("POST", "/auth/hosts/enrollment-tokens", {
            "label": "Eval host", "expires_at": expires,
        })["token"]
        result = self.anx("seeder", ["host", "enroll", "--name", "eval-host", f"--token={token}"])
        if result.exit_code != 0:
            raise FixtureError(f"host enroll failed: {result.stdout[:600]} {result.stderr[:300]}")

    # --- calls -----------------------------------------------------------

    def http(self, method: str, path: str, body: dict | None = None, auth: bool = True) -> dict:
        """Call core as the human operator. Used for seeding and for scoring."""
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(self.base_url + path, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        if auth:
            req.add_header("Authorization", f"Bearer {self.human_token}")
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                raw = resp.read().decode()
        except urllib.error.HTTPError as exc:
            raise FixtureError(f"{method} {path} -> {exc.code}: {exc.read().decode()[:600]}") from exc
        return json.loads(raw) if raw.strip() else {}

    def anx(self, agent: str, args: list[str], stdin: str | None = None,
            json_output: bool = True, timeout: int = 150) -> CommandResult:
        """Run the CLI as a derived agent. The eval agent's calls use json_output=False."""
        argv = [str(self.cli_bin)]
        if json_output:
            argv.append("--json")
        argv += ["--base-url", self.base_url, "--as", agent, *args]
        env = {
            "HOME": str(self.home),
            "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
            "NO_COLOR": "1",
            "TZ": "UTC",
        }
        started = time.monotonic()
        try:
            proc = subprocess.run(argv, input=stdin, capture_output=True, text=True,
                                  env=env, cwd=str(self.home), timeout=timeout)
            code, out, err = proc.returncode, proc.stdout, proc.stderr
        except subprocess.TimeoutExpired as exc:
            code, out, err = 124, (exc.stdout or ""), f"timed out after {timeout}s"
            if isinstance(out, bytes):
                out = out.decode(errors="replace")
        return CommandResult(args, code, out, err, int((time.monotonic() - started) * 1000))


def build_binaries(out_dir: Path) -> tuple[Path, Path]:
    out_dir.mkdir(parents=True, exist_ok=True)
    cli_bin, core_bin = out_dir / "anx", out_dir / "anx-core"
    for module, target, binary in (("cli", "./cmd/anx", cli_bin), ("core", "./cmd/anx-core", core_bin)):
        proc = subprocess.run(["go", "build", "-o", str(binary), target],
                              cwd=str(REPO_ROOT / module), capture_output=True, text=True)
        if proc.returncode != 0:
            raise FixtureError(f"go build {module} failed:\n{proc.stderr[-2000:]}")
    return cli_bin, core_bin


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]
