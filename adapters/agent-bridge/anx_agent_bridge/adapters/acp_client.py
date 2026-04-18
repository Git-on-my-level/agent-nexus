from __future__ import annotations

import json
import logging
import os
import queue
import subprocess
import threading
import time
from pathlib import Path
from typing import Any

LOGGER = logging.getLogger(__name__)


class ACPClientError(RuntimeError):
    pass


class ACPProcessClient:
    def __init__(
        self,
        command: list[str],
        cwd: str,
        env: dict[str, str] | None = None,
        *,
        auto_select_permission: bool = True,
        permission_option_id: str | None = None,
        startup_timeout_seconds: int = 20,
    ) -> None:
        self.command = command
        self.cwd = cwd
        self.env = env
        self.auto_select_permission = auto_select_permission
        self.permission_option_id = permission_option_id
        self.startup_timeout_seconds = startup_timeout_seconds
        self._proc: subprocess.Popen[str] | None = None
        self._id = 0
        self._pending: dict[int, queue.Queue[dict[str, Any]]] = {}
        self._pending_lock = threading.Lock()
        self._capture_lock = threading.Lock()
        self._captured_chunks: dict[str, list[str]] = {}
        self._reader_thread: threading.Thread | None = None
        self._stderr_thread: threading.Thread | None = None
        self._initialized = False
        self._starting = False

    def ensure_started(self) -> None:
        if self._proc and self._proc.poll() is None and (self._initialized or self._starting):
            return
        self.close()
        LOGGER.info("Starting ACP agent: %s", " ".join(self.command))
        self._proc = subprocess.Popen(
            self.command,
            cwd=self.cwd,
            env=self._build_env(),
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )
        if not self._proc.stdin or not self._proc.stdout or not self._proc.stderr:
            raise ACPClientError("ACP process pipes are unavailable")
        self._reader_thread = threading.Thread(target=self._reader_loop, name="acp-reader", daemon=True)
        self._stderr_thread = threading.Thread(target=self._stderr_loop, name="acp-stderr", daemon=True)
        self._reader_thread.start()
        self._stderr_thread.start()
        self._starting = True
        try:
            self.initialize()
        finally:
            self._starting = False

    def close(self) -> None:
        proc = self._proc
        self._initialized = False
        self._starting = False
        self._proc = None
        if not proc:
            return
        try:
            if proc.stdin:
                proc.stdin.close()
        except Exception:
            pass
        if proc.poll() is None:
            try:
                proc.terminate()
                proc.wait(timeout=5)
            except Exception:
                try:
                    proc.kill()
                    proc.wait(timeout=5)
                except Exception:
                    pass

    def _next_id(self) -> int:
        self._id += 1
        return self._id

    def _send(self, payload: dict[str, Any]) -> None:
        if not self._proc or not self._proc.stdin:
            raise ACPClientError("ACP process is not running")
        line = json.dumps(payload, separators=(",", ":"))
        LOGGER.debug("ACP -> %s", line)
        self._proc.stdin.write(line + "\n")
        self._proc.stdin.flush()

    def _build_env(self) -> dict[str, str]:
        env = os.environ.copy()
        if self.env:
            env.update(self.env)
        return env

    def call(self, method: str, params: dict[str, Any], timeout_seconds: int = 600) -> Any:
        self.ensure_started()
        request_id = self._next_id()
        q: queue.Queue[dict[str, Any]] = queue.Queue(maxsize=1)
        with self._pending_lock:
            self._pending[request_id] = q
        self._send({"jsonrpc": "2.0", "id": request_id, "method": method, "params": params})
        try:
            message = q.get(timeout=timeout_seconds)
        except queue.Empty as exc:
            with self._pending_lock:
                self._pending.pop(request_id, None)
            raise ACPClientError(f"Timed out waiting for ACP response to {method}") from exc
        if "error" in message:
            error = message["error"] or {}
            raise ACPClientError(f"ACP {method} failed: {error}")
        return message.get("result")

    def notify(self, method: str, params: dict[str, Any]) -> None:
        self.ensure_started()
        self._send({"jsonrpc": "2.0", "method": method, "params": params})

    def initialize(self) -> None:
        result = self.call(
            "initialize",
            {
                "protocolVersion": 1,
                "clientCapabilities": {
                    "fs": {"readTextFile": False, "writeTextFile": False},
                    "terminal": False,
                },
                "clientInfo": {"name": "anx-agent-bridge", "version": "0.1.0"},
            },
            timeout_seconds=self.startup_timeout_seconds,
        )
        LOGGER.info("ACP initialized: %s", result)
        self._initialized = True

    def _reader_loop(self) -> None:
        assert self._proc and self._proc.stdout
        for line in self._proc.stdout:
            line = line.strip()
            if not line:
                continue
            LOGGER.debug("ACP <- %s", line)
            try:
                payload = json.loads(line)
            except json.JSONDecodeError:
                LOGGER.warning("Ignoring non-JSON ACP stdout line: %s", line)
                continue
            try:
                self._handle_message(payload)
            except Exception:
                LOGGER.exception("Unhandled ACP message")
        LOGGER.warning("ACP stdout loop ended")

    def _stderr_loop(self) -> None:
        assert self._proc and self._proc.stderr
        for line in self._proc.stderr:
            line = line.rstrip()
            if line:
                LOGGER.info("ACP stderr: %s", line)

    def _handle_message(self, payload: dict[str, Any]) -> None:
        if "id" in payload and ("result" in payload or "error" in payload):
            request_id = int(payload["id"])
            with self._pending_lock:
                q = self._pending.pop(request_id, None)
            if q is not None:
                q.put(payload)
            return
        method = payload.get("method")
        if method == "session/update":
            params = payload.get("params") or {}
            session_id = str(params.get("sessionId", ""))
            update = params.get("update") or {}
            if str(update.get("sessionUpdate", "")) == "agent_message_chunk":
                content = update.get("content") or {}
                text = str(content.get("text", ""))
                if text and session_id:
                    with self._capture_lock:
                        self._captured_chunks.setdefault(session_id, []).append(text)
            return
        if method == "session/request_permission":
            self._handle_permission_request(payload)
            return
        if method:
            LOGGER.debug("Ignoring unsupported ACP request/notification: %s", method)
            if "id" in payload:
                self._send({
                    "jsonrpc": "2.0",
                    "id": payload["id"],
                    "error": {"code": -32601, "message": f"Unsupported method: {method}"},
                })

    def _handle_permission_request(self, payload: dict[str, Any]) -> None:
        if "id" not in payload:
            return
        request_id = payload["id"]
        params = payload.get("params") or {}
        options = params.get("options") or []
        if not self.auto_select_permission:
            result = {"outcome": "cancelled"}
        else:
            option_id = self.permission_option_id
            if not option_id and isinstance(options, list) and options:
                first = options[0]
                if isinstance(first, dict):
                    option_id = str(first.get("optionId") or first.get("id") or "")
            if not option_id:
                result = {"outcome": "cancelled"}
            else:
                result = {"outcome": "selected", "optionId": option_id}
        self._send({"jsonrpc": "2.0", "id": request_id, "result": result})

    def get_or_create_session(self, session_id: str | None, cwd: str) -> str:
        if session_id:
            try:
                self.call("session/load", {"cwd": cwd, "sessionId": session_id, "mcpServers": []}, timeout_seconds=30)
                return session_id
            except ACPClientError:
                LOGGER.info("ACP session/load failed for %s; creating a new session", session_id)
        result = self.call("session/new", {"cwd": cwd, "mcpServers": []}, timeout_seconds=30)
        new_session_id = str((result or {}).get("sessionId", "")).strip()
        if not new_session_id:
            raise ACPClientError("ACP session/new returned no sessionId")
        return new_session_id

    def prompt(self, session_id: str, text: str, timeout_seconds: int = 1800) -> str:
        with self._capture_lock:
            self._captured_chunks[session_id] = []
        result = self.call(
            "session/prompt",
            {
                "sessionId": session_id,
                "prompt": [{"type": "text", "text": text}],
            },
            timeout_seconds=timeout_seconds,
        )
        LOGGER.debug("ACP prompt result: %s", result)
        with self._capture_lock:
            chunks = self._captured_chunks.pop(session_id, [])
        return "".join(chunks).strip()
