"""Run a user-defined command with JSON on stdin; expect JSON AdapterResult on stdout.

The bridge operator model is single-tenant / trusted host: ``command``, ``cwd``, and ``env`` come
from config and inherit the bridge process environment. Do not point this at untrusted config sources.
"""

from __future__ import annotations

import json
import logging
import os
import subprocess
import threading
from typing import Any

# Per-stream cap (stdout and stderr measured as UTF-8 byte length) to bound host memory if a child
# prints huge output or runs away.
_MAX_ADAPTER_IO_BYTES = 16 * 1024 * 1024

from ..models import WakePacket
from .adapter_contract import (
    ENV_BRIDGE_MODE,
    MODE_DISPATCH,
    MODE_DOCTOR,
    build_dispatch_request,
    build_doctor_request,
    parse_dispatch_response,
    parse_doctor_response,
)
from .base import AdapterResult

LOGGER = logging.getLogger(__name__)


class SubprocessAdapter:
    def __init__(
        self,
        *,
        command: list[str],
        handle: str,
        workspace_id: str,
        cwd: str | None = None,
        env: dict[str, str] | None = None,
        dispatch_timeout_seconds: int = 600,
        doctor_timeout_seconds: int = 60,
        doctor_command: list[str] | None = None,
        adapter_raw: dict[str, Any],
    ) -> None:
        if not command:
            raise ValueError("subprocess adapter requires non-empty command")
        self.command = command
        self.handle = handle.strip()
        self.workspace_id = workspace_id.strip()
        self.cwd = cwd.strip() if cwd and cwd.strip() else None
        self.extra_env = env or {}
        self.dispatch_timeout_seconds = max(1, int(dispatch_timeout_seconds))
        self.doctor_timeout_seconds = max(1, int(doctor_timeout_seconds))
        self.doctor_command = doctor_command
        self.adapter_raw = adapter_raw

    def doctor(self) -> dict[str, Any]:
        req = build_doctor_request(
            handle=self.handle,
            workspace_id=self.workspace_id,
            adapter_settings=dict(self.adapter_raw),
        )
        cmd = self.doctor_command if self.doctor_command else self.command
        stdout, stderr, code = self._run_json_mode(
            cmd,
            req,
            timeout=self.doctor_timeout_seconds,
            mode=MODE_DOCTOR,
        )
        if code != 0:
            raise RuntimeError(
                f"adapter doctor command exited {code}: {stderr or stdout or '(no output)'}"
            )
        parsed = parse_doctor_response(stdout)
        result: dict[str, Any] = {
            "adapter_kind": "subprocess",
            "command": " ".join(cmd),
            "doctor_ok": parsed["ok"],
        }
        if "message" in parsed:
            result["message"] = parsed["message"]
        if "details" in parsed:
            result["details"] = parsed["details"]
        if not parsed["ok"]:
            raise RuntimeError(result.get("message") or "adapter doctor reported ok=false")
        return result

    def dispatch(
        self,
        packet: WakePacket,
        prompt_text: str,
        session_key: str,
        existing_native_session_id: str | None = None,
    ) -> AdapterResult:
        req = build_dispatch_request(
            wake_packet=packet,
            prompt_text=prompt_text,
            session_key=session_key,
            existing_native_session_id=existing_native_session_id,
            adapter_settings=dict(self.adapter_raw),
        )
        stdout, stderr, code = self._run_json_mode(
            self.command,
            req,
            timeout=self.dispatch_timeout_seconds,
            mode=MODE_DISPATCH,
        )
        if code != 0:
            raise RuntimeError(
                f"adapter dispatch command exited {code}: {stderr or stdout or '(no output)'}"
            )
        return parse_dispatch_response(stdout)

    def _run_json_mode(
        self,
        argv: list[str],
        request: dict[str, Any],
        *,
        timeout: int,
        mode: str,
    ) -> tuple[str, str, int]:
        payload = json.dumps(request, separators=(",", ":"), ensure_ascii=False)
        run_env = {**os.environ, **self.extra_env, ENV_BRIDGE_MODE: mode}
        wakeup_id = ""
        session_key = ""
        if mode == MODE_DISPATCH:
            try:
                wakeup_id = str(request.get("wake_packet", {}).get("wakeup_id", "") or "")
            except (TypeError, AttributeError):
                wakeup_id = ""
            session_key = str(request.get("session_key", "") or "")
        LOGGER.info(
            "subprocess adapter: mode=%s wakeup_id=%s session_key=%s argv0=%s",
            mode,
            wakeup_id or "-",
            session_key or "-",
            argv[0] if argv else "",
        )
        LOGGER.debug("subprocess adapter %s full argv: %s", mode, argv)
        return _run_subprocess_capped(
            argv,
            payload,
            timeout=timeout,
            cwd=self.cwd,
            env=run_env,
            max_io_bytes=_MAX_ADAPTER_IO_BYTES,
        )


def _run_subprocess_capped(
    argv: list[str],
    payload: str,
    *,
    timeout: int,
    cwd: str | None,
    env: dict[str, str],
    max_io_bytes: int,
) -> tuple[str, str, int]:
    proc = subprocess.Popen(
        argv,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        cwd=cwd,
        env=env,
    )
    out_chunks: list[str] = []
    err_chunks: list[str] = []
    out_total = [0]
    err_total = [0]
    overflow: list[RuntimeError | None] = [None]

    def drain(label: str, pipe: Any, chunks: list[str], total: list[int]) -> None:
        try:
            while True:
                block = pipe.read(65536)
                if not block:
                    break
                total[0] += len(block.encode("utf-8"))
                if total[0] > max_io_bytes:
                    overflow[0] = RuntimeError(
                        f"adapter subprocess {label} exceeded {max_io_bytes} bytes "
                        "(limit debug or misconfigured adapter output)"
                    )
                    try:
                        proc.kill()
                    except OSError:
                        pass
                    break
                chunks.append(block)
        finally:
            try:
                pipe.close()
            except OSError:
                pass

    t_out = threading.Thread(
        target=drain,
        args=("stdout", proc.stdout, out_chunks, out_total),
        name="anx-subprocess-stdout",
        daemon=True,
    )
    t_err = threading.Thread(
        target=drain,
        args=("stderr", proc.stderr, err_chunks, err_total),
        name="anx-subprocess-stderr",
        daemon=True,
    )
    t_out.start()
    t_err.start()
    if proc.stdin is None:
        raise RuntimeError("subprocess stdin is unexpectedly unavailable")
    write_done = threading.Event()
    write_error: list[BaseException | None] = [None]

    def writer() -> None:
        try:
            proc.stdin.write(payload)
        except (BrokenPipeError, OSError) as exc:
            write_error[0] = exc
        finally:
            try:
                proc.stdin.close()
            except OSError:
                pass
            write_done.set()

    tw = threading.Thread(target=writer, name="anx-subprocess-stdin", daemon=True)
    tw.start()
    # If the child never reads stdin, the pipe buffer can fill and block the writer; bound this
    # phase separately from proc.wait so dispatch_timeout still applies to end-to-end hangs.
    stdin_budget = float(min(max(timeout, 1), 120))
    if not write_done.wait(timeout=stdin_budget):
        try:
            proc.kill()
        except OSError:
            pass
        try:
            proc.wait(timeout=5)
        except OSError:
            pass
        t_out.join(timeout=5)
        t_err.join(timeout=5)
        raise RuntimeError(
            "adapter subprocess did not accept stdin (child may not be reading JSON from stdin)"
        ) from None
    if write_error[0] is not None:
        try:
            proc.kill()
        except OSError:
            pass
        try:
            proc.wait(timeout=5)
        except OSError:
            pass
        t_out.join(timeout=5)
        t_err.join(timeout=5)
        raise RuntimeError(f"adapter subprocess stdin write failed: {write_error[0]}") from write_error[0]
    try:
        proc.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        try:
            proc.kill()
        except OSError:
            pass
        try:
            proc.wait(timeout=5)
        except OSError:
            pass
        t_out.join(timeout=5)
        t_err.join(timeout=5)
        raise RuntimeError(f"adapter subprocess timed out after {timeout}s") from None
    t_out.join()
    t_err.join()
    if overflow[0] is not None:
        raise overflow[0]
    return "".join(out_chunks).strip(), "".join(err_chunks).strip(), int(proc.returncode or 0)
