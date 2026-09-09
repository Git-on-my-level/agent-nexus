"""Bound an existing anx-agent-bridge JSON adapter for contextual PM turns.

Provider/model selection stays in the operator's existing delegate adapter
settings. This wrapper is a process/time/output boundary, NOT a sandbox. Run it
under a deployment-enforced read-only PM capability envelope. Never give its
provider ambient source-write credentials or an unrestricted tool runtime.
"""
from __future__ import annotations

import json
import os
import selectors
import signal
import subprocess
import sys
import time
from datetime import datetime, timezone
from typing import Any

REQUEST = "anx-bridge-adapter-request/v1"
RESPONSE = "anx-bridge-adapter-response/v1"
MAX_REQUEST = 256 * 1024


def _bounded_process(command: list[str], payload: bytes, *, timeout: float,
                     max_output: int, env: dict[str, str], cwd: str | None) -> bytes:
    """One absolute deadline covers stdin, execution, and both output pipes."""
    if timeout <= 0 or timeout > 600:
        raise ValueError("PM execution deadline is expired or exceeds ten minutes")
    proc = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, cwd=cwd, env=env,
                            start_new_session=True)
    deadline = time.monotonic() + timeout
    selector = selectors.DefaultSelector()
    output = bytearray()
    error_bytes = 0
    written = 0
    try:
        assert proc.stdin and proc.stdout and proc.stderr
        for pipe, role, events in ((proc.stdin, "stdin", selectors.EVENT_WRITE),
                                   (proc.stdout, "stdout", selectors.EVENT_READ),
                                   (proc.stderr, "stderr", selectors.EVENT_READ)):
            os.set_blocking(pipe.fileno(), False)
            selector.register(pipe, events, role)
        while selector.get_map():
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("PM provider exceeded its execution deadline")
            for key, _ in selector.select(min(remaining, 0.1)):
                pipe, role = key.fileobj, key.data
                if role == "stdin":
                    try:
                        written += os.write(pipe.fileno(), payload[written:written + 8192])
                    except BrokenPipeError:
                        written = len(payload)
                    if written == len(payload):
                        selector.unregister(pipe)
                        pipe.close()
                    continue
                data = os.read(pipe.fileno(), 8192)
                if not data:
                    selector.unregister(pipe)
                    pipe.close()
                elif role == "stdout":
                    output.extend(data)
                    if len(output) > max_output:
                        raise ValueError("PM provider exceeded its output bound")
                else:
                    error_bytes += len(data)
                    if error_bytes > 16384:
                        raise ValueError("PM provider exceeded its diagnostic bound")
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("PM provider exceeded its execution deadline")
        if proc.wait(timeout=remaining) != 0:
            # Do not copy provider stdout/stderr or credentials into receipts.
            raise RuntimeError("PM provider execution failed")
        return bytes(output)
    finally:
        # Also terminate descendants that outlive a successfully exited parent.
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        proc.wait(timeout=5)
        selector.close()
        for pipe in (proc.stdin, proc.stdout, proc.stderr):
            if pipe and not pipe.closed:
                pipe.close()


def dispatch(request: dict[str, Any]) -> dict[str, Any]:
    if request.get("schema_version") != REQUEST:
        raise ValueError("Unsupported bridge adapter request version")
    mode = request.get("mode")
    if mode not in ("doctor", "dispatch"):
        raise ValueError("Unsupported bridge adapter mode")
    settings = request.get("adapter", {})
    delegate = settings.get("delegate", {})
    command = delegate.get("command")
    if (not isinstance(command, list) or not command or
            any(not isinstance(arg, str) or not arg for arg in command)):
        raise ValueError("An existing bridge delegate command must be configured")
    configured_timeout = float(settings.get("pm_timeout_seconds", 120))
    max_output = int(settings.get("pm_max_output_bytes", 16000))
    if not 1 <= configured_timeout <= 600 or not 256 <= max_output <= 64000:
        raise ValueError("Invalid PM execution bounds")
    timeout = min(configured_timeout, 15) if mode == "doctor" else configured_timeout
    if mode == "dispatch":
        packet = request.get("wake_packet", {})
        summary = packet.get("context_inline", {}).get("current_summary", "")
        try:
            policy = json.loads(summary)
            deadline = datetime.fromisoformat(policy["deadline"].replace("Z", "+00:00"))
            if deadline.tzinfo is None or not policy.get("pm_turn_id"):
                raise ValueError("missing PM turn identity or timezone")
            remaining = (deadline - datetime.now(timezone.utc)).total_seconds()
            timeout = min(timeout, remaining)
            max_output = min(max_output, int(policy["max_output_bytes"]))
        except (KeyError, TypeError, ValueError, AttributeError) as exc:
            raise ValueError("PM wake is missing a valid execution policy") from exc
        if timeout <= 0 or not 256 <= max_output <= 64000:
            raise ValueError("PM execution policy is expired or invalid")
    # Static operator settings select the EXISTING provider command and model.
    # Never derive command, model, env or cwd from source content or PM text.
    inner = {**request, "adapter": delegate.get("adapter", {})}
    environment = {key: value for key, value in os.environ.items()
                   if key in {"PATH", "LANG", "LC_ALL", "TMPDIR", "SYSTEMROOT"}}
    extra_env = delegate.get("env", {})
    if (not isinstance(extra_env, dict) or
            any(not isinstance(k, str) or not isinstance(v, str) for k, v in extra_env.items())):
        raise ValueError("Invalid delegate environment")
    environment.update(extra_env)
    environment["ANX_BRIDGE_MODE"] = mode
    encoded = json.dumps(inner, separators=(",", ":")).encode()
    if len(encoded) > MAX_REQUEST:
        raise ValueError("PM request exceeds its input bound")
    raw = _bounded_process(command, encoded, timeout=timeout,
                           max_output=max_output + 8192, env=environment,
                           cwd=delegate.get("cwd"))
    try:
        result = json.loads(raw)
    except (ValueError, UnicodeError) as exc:
        raise ValueError("Provider did not return a bridge adapter response") from exc
    if not isinstance(result, dict) or result.get("schema_version") != RESPONSE:
        raise ValueError("Provider response has an unsupported bridge schema")
    if mode == "doctor":
        if result.get("ok") is not True:
            raise RuntimeError("Existing provider doctor did not establish readiness")
        return {"schema_version": RESPONSE, "ok": True,
                "message": "Existing provider is reachable; deployment must separately enforce PM permissions"}
    text = result.get("response_text")
    if not isinstance(text, str) or not text.strip() or len(text.encode()) > max_output:
        raise ValueError("Provider response is empty or exceeds the PM output bound")
    native = result.get("native_session_id")
    if native is not None and (not isinstance(native, str) or len(native) > 1024):
        raise ValueError("Provider returned an invalid native session identity")
    return {"schema_version": RESPONSE, "response_text": text,
            "native_session_id": native,
            "metadata": {"adapter_kind": "pm_bounded_delegate", "bounded": True}}


def main() -> int:
    try:
        raw = sys.stdin.buffer.read(MAX_REQUEST + 1)
        if len(raw) > MAX_REQUEST:
            raise ValueError("PM input exceeds its size bound")
        request = json.loads(raw)
        if not isinstance(request, dict):
            raise ValueError("PM request must be a JSON object")
        print(json.dumps(dispatch(request), ensure_ascii=False))
        return 0
    except Exception:
        # Detailed provider failures can contain credentials and source text.
        print("PM adapter failed; inspect the protected runtime diagnostics", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
