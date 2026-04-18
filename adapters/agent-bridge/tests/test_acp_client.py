import os

import subprocess

from anx_agent_bridge.adapters.acp_client import ACPProcessClient


def test_build_env_merges_with_parent_environment(monkeypatch):
    monkeypatch.setenv("PATH", "/usr/bin")
    monkeypatch.setenv("ANX_PARENT", "present")
    client = ACPProcessClient(command=["hermes", "acp"], cwd="/tmp", env={"ANX_CHILD": "set"})

    env = client._build_env()

    assert env["PATH"] == os.environ["PATH"]
    assert env["ANX_PARENT"] == "present"
    assert env["ANX_CHILD"] == "set"


class _StubStdin:
    def __init__(self):
        self.closed = False

    def close(self):
        self.closed = True


class _StubProc:
    def __init__(self):
        self.stdin = _StubStdin()
        self.wait_calls = []
        self.killed = False

    def poll(self):
        return None

    def terminate(self):
        raise subprocess.TimeoutExpired(cmd="hermes", timeout=5)

    def wait(self, timeout=None):
        self.wait_calls.append(timeout)
        if not self.killed:
            raise subprocess.TimeoutExpired(cmd="hermes", timeout=timeout or 0)
        return 0

    def kill(self):
        self.killed = True


def test_close_reaps_process_after_forced_kill():
    client = ACPProcessClient(command=["hermes", "acp"], cwd="/tmp")
    proc = _StubProc()
    client._proc = proc

    client.close()

    assert proc.stdin.closed is True
    assert proc.killed is True
    assert proc.wait_calls == [5]
