"""Thin anx CLI wrapper. Arguments are a fixed list; the shell is never involved."""

from __future__ import annotations

import json
from typing import Any

from readers.run import RunResult, Runner


class AnxError(RuntimeError):
    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code
        self.message = message


class AnxClient:
    def __init__(self, binary: str, base_url: str, agent: str, runner: Runner | None = None):
        self.binary = binary
        self.base_url = base_url
        self.agent = agent
        self.runner = runner or Runner()

    def work_list(self, source: str) -> list[dict]:
        cards = []
        cursor = ""
        for _ in range(50):
            args = ["work", "list", "--source", source, "--limit", "200"]
            if cursor:
                args.extend(["--cursor", cursor])
            result = self._call(args, timeout=60)
            page = result.get("work") or []
            if isinstance(page, list):
                cards.extend(item for item in page if isinstance(item, dict))
            cursor = result.get("next_cursor") or ""
            if not cursor:
                break
        return cards

    def work_create(self, body: dict) -> dict:
        return self._call_body(["work", "create"], body, timeout=60)

    def observe(self, ref: str, body: dict) -> dict:
        return self._call_body(["work", "observations", "submit", ref], body, timeout=60)

    def docs_list(self) -> list[dict]:
        result = self._call(["docs", "list"], timeout=60)
        documents = result.get("documents") or []
        return documents if isinstance(documents, list) else []

    def docs_create(self, topic: str, title: str, body_file: str) -> dict:
        return self._call(
            ["docs", "create", "--topic", topic, "--title", title, "--body-file", body_file],
            timeout=60,
        )

    def docs_revise(self, ref: str, body_file: str) -> dict:
        return self._call(["docs", "revise", ref, "--apply", "--body-file", body_file], timeout=90)

    def docs_content(self, ref: str) -> str:
        result = self._call(["docs", "get", ref], timeout=60)
        revision = result.get("revision") or {}
        content = revision.get("content")
        if not isinstance(content, str):
            raise AnxError("invalid_request", "docs get did not return revision content")
        return content

    def _call_body(self, args: list[str], body: dict, *, timeout: float) -> dict:
        import tempfile
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as handle:
            json.dump(body, handle)
            path = handle.name
        try:
            return self._call([*args, "--from-file", path], timeout=timeout)
        finally:
            import os
            os.unlink(path)

    def _call(self, args: list[str], *, timeout: float) -> dict:
        argv = [self.binary, "--json", "--base-url", self.base_url, "--as", self.agent, *args]
        result = self.runner(argv, timeout=timeout)
        return _envelope(result)


def _envelope(result: RunResult) -> dict[str, Any]:
    try:
        payload = json.loads(result.stdout or "")
    except json.JSONDecodeError as exc:
        detail = (result.error or result.stderr or "anx returned invalid JSON")[:300]
        raise AnxError("invalid_response", detail) from exc
    if not isinstance(payload, dict):
        raise AnxError("invalid_response", "anx returned a non-object envelope")
    if payload.get("ok") is True:
        result_body = payload.get("result")
        return result_body if isinstance(result_body, dict) else {}
    error = payload.get("error") or {}
    code = str(error.get("code") or "error")
    message = str(error.get("message") or result.error or "anx command failed")
    raise AnxError(code, message[:500])
