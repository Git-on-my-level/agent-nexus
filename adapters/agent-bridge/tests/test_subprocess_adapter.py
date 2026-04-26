import json
import os
import sys
from pathlib import Path

import pytest

from anx_agent_bridge.adapters import subprocess_adapter as subprocess_adapter_mod
from anx_agent_bridge.adapters.subprocess_adapter import SubprocessAdapter
from anx_agent_bridge.models import WakePacket


ECHO_ADAPTER = '''import json, os, sys
req = json.load(sys.stdin)
mode = os.environ.get("ANX_BRIDGE_MODE") or req.get("mode", "")
if mode == "doctor":
    print(json.dumps({"schema_version": "anx-bridge-adapter-response/v1", "ok": True, "details": {}}))
elif mode == "dispatch":
    print(json.dumps({
        "schema_version": "anx-bridge-adapter-response/v1",
        "response_text": "echo:" + req.get("prompt_text", "")[:20],
        "native_session_id": "n1",
    }))
else:
    sys.exit(2)
'''


def _write_echo(tmp_path: Path) -> Path:
    p = tmp_path / "echo_adapter.py"
    p.write_text(ECHO_ADAPTER, encoding="utf-8")
    return p


def _minimal_packet() -> WakePacket:
    return WakePacket(
        wakeup_id="wake_1",
        handle="agent1",
        actor_id="actor_1",
        workspace_id="ws_main",
        workspace_name="Main",
        thread_id="thread_1",
        thread_title="T",
        trigger_event_id="evt_1",
        trigger_created_at="2026-01-01T00:00:00Z",
        trigger_author_actor_id="actor_h",
        trigger_text="@agent1 hi",
        current_summary="s",
        session_key="sk",
        anx_base_url="http://localhost:8080",
        thread_context_url="http://localhost:8080/c",
        thread_workspace_url="http://localhost:8080/w",
        trigger_event_url="http://localhost:8080/e",
        cli_thread_inspect="anx threads inspect --thread-id thread_1 --json",
        cli_thread_workspace="anx threads workspace --thread-id thread_1 --json",
    )


def test_subprocess_doctor_and_dispatch(tmp_path: Path) -> None:
    script = _write_echo(tmp_path)
    raw = {"kind": "subprocess", "command": [sys.executable, str(script)]}
    ad = SubprocessAdapter(
        command=[sys.executable, str(script)],
        handle="agent1",
        workspace_id="ws_main",
        adapter_raw=raw,
    )
    doc = ad.doctor()
    assert doc.get("doctor_ok") is True
    res = ad.dispatch(_minimal_packet(), "hello world", "sk", None)
    assert res.response_text.startswith("echo:")
    assert res.native_session_id == "n1"


def test_subprocess_nonzero_exit(tmp_path: Path) -> None:
    script = tmp_path / "bad.py"
    script.write_text("import sys\nsys.exit(1)\n", encoding="utf-8")
    raw = {"kind": "subprocess", "command": [sys.executable, str(script)]}
    ad = SubprocessAdapter(
        command=[sys.executable, str(script)],
        handle="h",
        workspace_id="ws",
        adapter_raw=raw,
    )
    with pytest.raises(RuntimeError, match="exited 1"):
        ad.doctor()


def test_subprocess_invalid_stdout_json(tmp_path: Path) -> None:
    script = tmp_path / "garbage.py"
    script.write_text("import sys\nsys.stdin.read()\nprint('not json')\n", encoding="utf-8")
    raw = {"kind": "subprocess", "command": [sys.executable, str(script)]}
    ad = SubprocessAdapter(
        command=[sys.executable, str(script)],
        handle="h",
        workspace_id="ws",
        adapter_raw=raw,
    )
    with pytest.raises(ValueError, match="not valid JSON"):
        ad.doctor()


def test_subprocess_timeout(tmp_path: Path) -> None:
    script = tmp_path / "slow.py"
    script.write_text("import time\ntime.sleep(30)\n", encoding="utf-8")
    raw = {"kind": "subprocess", "command": [sys.executable, str(script)]}
    ad = SubprocessAdapter(
        command=[sys.executable, str(script)],
        handle="h",
        workspace_id="ws",
        doctor_timeout_seconds=1,
        adapter_raw=raw,
    )
    with pytest.raises(RuntimeError, match="timed out"):
        ad.doctor()


def test_parse_dispatch_rejects_non_string_response_text(tmp_path: Path) -> None:
    script = tmp_path / "badtype.py"
    script.write_text(
        "import json,sys\n"
        "json.load(sys.stdin)\n"
        'print(json.dumps({"schema_version":"anx-bridge-adapter-response/v1","response_text":123}))\n',
        encoding="utf-8",
    )
    raw = {"kind": "subprocess", "command": [sys.executable, str(script)]}
    ad = SubprocessAdapter(
        command=[sys.executable, str(script)],
        handle="h",
        workspace_id="ws",
        adapter_raw=raw,
    )
    with pytest.raises(ValueError, match="must be a JSON string"):
        ad.dispatch(_minimal_packet(), "hi", "sk", None)


def test_subprocess_stdin_stall_times_out(tmp_path: Path) -> None:
    script = tmp_path / "noread.py"
    script.write_text("import time\ntime.sleep(60)\n", encoding="utf-8")
    huge = "x" * (256 * 1024)
    with pytest.raises(RuntimeError, match="did not accept stdin"):
        subprocess_adapter_mod._run_subprocess_capped(
            [sys.executable, str(script)],
            huge,
            timeout=2,
            cwd=None,
            env=os.environ.copy(),
            max_io_bytes=subprocess_adapter_mod._MAX_ADAPTER_IO_BYTES,
        )


def test_subprocess_rejects_huge_stdout(tmp_path: Path) -> None:
    script = tmp_path / "flood.py"
    script.write_text(
        "import sys\n"
        "sys.stdin.read()\n"
        "sys.stdout.write('x' * (20 * 1024 * 1024))\n"
        "sys.stdout.flush()\n",
        encoding="utf-8",
    )
    raw = {"kind": "subprocess", "command": [sys.executable, str(script)]}
    ad = SubprocessAdapter(
        command=[sys.executable, str(script)],
        handle="h",
        workspace_id="ws",
        doctor_timeout_seconds=30,
        adapter_raw=raw,
    )
    with pytest.raises(RuntimeError, match="exceeded"):
        ad.doctor()


def test_subprocess_doctor_ok_false(tmp_path: Path) -> None:
    script = tmp_path / "faildoc.py"
    script.write_text(
        "import json,sys\n"
        "json.load(sys.stdin)\n"
        'print(json.dumps({"schema_version":"anx-bridge-adapter-response/v1","ok":false,"message":"no"}))\n',
        encoding="utf-8",
    )
    raw = {"kind": "subprocess", "command": [sys.executable, str(script)]}
    ad = SubprocessAdapter(
        command=[sys.executable, str(script)],
        handle="h",
        workspace_id="ws",
        adapter_raw=raw,
    )
    with pytest.raises(RuntimeError, match="no"):
        ad.doctor()
