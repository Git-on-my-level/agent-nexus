"""The agent under test: a model with one tool, the `anx` CLI.

The model reaches the CLI only through this tool, so every byte it reads and
every command it tries is counted. Any OpenAI-compatible chat completions
endpoint works; nothing here is specific to one provider.
"""

from __future__ import annotations

import json
import os
import re
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path

from .fixture import Workspace

MAX_TOOL_OUTPUT_CHARS = 12_000
# Flags the harness sets itself, and commands that change the machine rather
# than the workspace. The agent is told so instead of being allowed to run them.
RESERVED_FLAGS = ("--base-url", "--as", "--config-dir", "--workspace")
MACHINE_COMMANDS = {"update", "bridge", "install", "skills", "host", "integrations", "import"}

SYSTEM_PROMPT = """You are an AI agent on a team that coordinates its work in a shared workspace.
You reach the workspace through one tool, `anx`, which runs the anx command-line program with the
arguments you give it. You are already signed in and the workspace is already selected.

Do exactly what the task asks, then reply with a short final answer and no tool call.
{skill}"""

COLD_START = "You have not used anx before. `anx help` shows how it works."

TOOL = {
    "type": "function",
    "function": {
        "name": "anx",
        "description": "Run the anx command-line program. `args` is everything after `anx`, one list "
                       "entry per argument, as a shell would split it. Returns the exit code and output.",
        "parameters": {
            "type": "object",
            "properties": {
                "args": {"type": "array", "items": {"type": "string"}},
                "stdin": {"type": "string", "description": "Optional text to pass on standard input."},
            },
            "required": ["args"],
        },
    },
}


class ModelError(RuntimeError):
    pass


@dataclass
class ModelConfig:
    api_base: str
    api_key: str
    model: str
    extra_body: dict = field(default_factory=dict)

    @classmethod
    def from_env(cls) -> "ModelConfig":
        base = os.environ.get("ANX_EVAL_API_BASE", "").rstrip("/")
        model = os.environ.get("ANX_EVAL_MODEL", "")
        key = os.environ.get("ANX_EVAL_API_KEY", "")
        key_file = os.environ.get("ANX_EVAL_API_KEY_FILE", "")
        if not key and key_file:
            key = Path(key_file).read_text().strip()
        missing = [name for name, value in (("ANX_EVAL_API_BASE", base), ("ANX_EVAL_MODEL", model),
                                            ("ANX_EVAL_API_KEY or ANX_EVAL_API_KEY_FILE", key)) if not value]
        if missing:
            raise ModelError("model mode needs " + ", ".join(missing))
        extra = json.loads(os.environ.get("ANX_EVAL_EXTRA_BODY", "") or "{}")
        return cls(base, key, model, extra)


@dataclass
class Command:
    args: list[str]
    exit_code: int
    output_chars: int
    truncated: bool
    duration_ms: int
    error_code: str = ""
    error_line: str = ""
    denied: bool = False


@dataclass
class Episode:
    final_answer: str = ""
    commands: list[Command] = field(default_factory=list)
    turns: int = 0
    prompt_tokens: int = 0
    completion_tokens: int = 0
    stop_reason: str = "answered"  # answered | max_turns | model_error
    error: str = ""
    seconds: float = 0.0


def run_tool(ws: Workspace, agent: str, args: list[str], stdin: str | None) -> tuple[str, Command]:
    """Run one agent command and return (text shown to the model, record)."""
    denied = _denial(args)
    if denied:
        return f"exit_code=2\n{denied}", Command(args, 2, len(denied), False, 0, "eval_denied", denied, True)
    result = ws.anx(agent, args, stdin=stdin, json_output=False)
    body = result.stdout
    if result.stderr.strip():
        body += ("\n" if body and not body.endswith("\n") else "") + "[stderr]\n" + result.stderr
    truncated = len(body) > MAX_TOOL_OUTPUT_CHARS
    shown = body[:MAX_TOOL_OUTPUT_CHARS]
    if truncated:
        shown += f"\n[output cut: {len(body)} characters, first {MAX_TOOL_OUTPUT_CHARS} shown]"
    code, line = _error_of(body) if result.exit_code != 0 else ("", "")
    record = Command(args, result.exit_code, len(body), truncated, result.duration_ms, code, line)
    return f"exit_code={result.exit_code}\n{shown}", record


def run_oracle(ws: Workspace, agent: str, commands: list[list[str]], answer: str) -> Episode:
    episode = Episode(final_answer=answer)
    started = time.monotonic()
    for args in commands:
        _, record = run_tool(ws, agent, args, None)
        episode.commands.append(record)
    episode.turns = len(commands)
    episode.seconds = time.monotonic() - started
    return episode


def run_model(ws: Workspace, agent: str, config: ModelConfig, prompt: str, skill_text: str,
              max_turns: int, transcript: Path) -> Episode:
    system = SYSTEM_PROMPT.format(skill=("\n" + skill_text) if skill_text else COLD_START)
    messages: list[dict] = [{"role": "system", "content": system}, {"role": "user", "content": prompt}]
    episode = Episode()
    started = time.monotonic()
    with transcript.open("w") as log:
        log.write(json.dumps(messages[0]) + "\n" + json.dumps(messages[1]) + "\n")
        while True:
            if episode.turns >= max_turns:
                episode.stop_reason = "max_turns"
                break
            try:
                reply, usage = _complete(config, messages)
            except ModelError as exc:
                episode.stop_reason, episode.error = "model_error", str(exc)
                break
            episode.turns += 1
            episode.prompt_tokens += int(usage.get("prompt_tokens") or 0)
            episode.completion_tokens += int(usage.get("completion_tokens") or 0)
            messages.append(reply)
            log.write(json.dumps(reply) + "\n")
            calls = reply.get("tool_calls") or []
            if not calls:
                episode.final_answer = reply.get("content") or ""
                break
            for call in calls:
                text, record = _dispatch(ws, agent, call)
                if record is not None:
                    episode.commands.append(record)
                message = {"role": "tool", "tool_call_id": call.get("id"), "content": text}
                messages.append(message)
                log.write(json.dumps(message) + "\n")
    episode.seconds = time.monotonic() - started
    return episode


def _dispatch(ws: Workspace, agent: str, call: dict) -> tuple[str, Command | None]:
    function = call.get("function") or {}
    try:
        arguments = json.loads(function.get("arguments") or "{}")
        args = arguments["args"]
        if function.get("name") != "anx" or not isinstance(args, list) or not all(isinstance(a, str) for a in args):
            raise ValueError("bad shape")
    except (ValueError, KeyError, TypeError):
        return 'exit_code=2\nThe tool takes {"args": ["..."]} with a list of strings.', None
    if args and args[0] == "anx":
        args = args[1:]
    stdin = arguments.get("stdin") if isinstance(arguments.get("stdin"), str) else None
    return run_tool(ws, agent, args, stdin)


def _denial(args: list[str]) -> str:
    for arg in args:
        if arg.split("=", 1)[0] in RESERVED_FLAGS:
            return f"{arg.split('=', 1)[0]} is already set for you in this environment; leave it out."
    command = next((arg for arg in args if not arg.startswith("-")), "")
    if command in MACHINE_COMMANDS:
        return f"`anx {command}` changes this machine and is not available in this environment."
    return ""


_TEXT_ERROR = re.compile(r'^error code=(\S+)', re.MULTILINE)


def _error_of(body: str) -> tuple[str, str]:
    """Pull the error code and first line out of text or JSON CLI output."""
    stripped = body.lstrip()
    if stripped.startswith("{"):
        try:
            error = json.loads(stripped).get("error") or {}
            return str(error.get("code") or ""), str(error.get("message") or "")[:200]
        except json.JSONDecodeError:
            pass
    match = _TEXT_ERROR.search(body)
    first = next((line for line in body.splitlines() if line.strip() and line.strip() != "[stderr]"), "")[:200]
    return (match.group(1) if match else ""), first


def _complete(config: ModelConfig, messages: list[dict]) -> tuple[dict, dict]:
    body = {"model": config.model, "messages": messages, "tools": [TOOL], "temperature": 0, **config.extra_body}
    request = urllib.request.Request(
        config.api_base + "/chat/completions", data=json.dumps(body).encode(), method="POST",
        headers={"Authorization": f"Bearer {config.api_key}", "Content-Type": "application/json"},
    )
    delay = 2.0
    for attempt in range(6):
        try:
            with urllib.request.urlopen(request, timeout=180) as response:
                payload = json.loads(response.read().decode())
            choice = (payload.get("choices") or [{}])[0]
            message = choice.get("message")
            if not message:
                raise ModelError(f"no message in model response: {json.dumps(payload)[:300]}")
            reply = {"role": "assistant", "content": message.get("content") or ""}
            if message.get("tool_calls"):
                reply["tool_calls"] = message["tool_calls"]
            return reply, payload.get("usage") or {}
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode(errors="replace")[:300]
            if exc.code not in (408, 429, 500, 502, 503, 504) or attempt == 5:
                raise ModelError(f"model API returned {exc.code}: {detail}") from exc
            retry_after = exc.headers.get("Retry-After", "")
            time.sleep(float(retry_after) if retry_after.isdigit() else delay)
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            if attempt == 5:
                raise ModelError(f"model API unreachable: {exc}") from exc
            time.sleep(delay)
        delay = min(delay * 2, 30)
    raise ModelError("model API retries exhausted")
