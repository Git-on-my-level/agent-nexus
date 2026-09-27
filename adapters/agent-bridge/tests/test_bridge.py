import json
import os
import stat
from pathlib import Path

import httpx
import pytest

from anx_agent_bridge.anx_client import Client, HostCLI
from anx_agent_bridge.bridge import Bridge
from anx_agent_bridge.config import Runtime, load_config


def executable(path: Path, content: str) -> Path:
    path.write_text(content)
    path.chmod(path.stat().st_mode | stat.S_IXUSR)
    return path


def setup_bridge(tmp_path, monkeypatch, agentctl=False):
    output = tmp_path / "runtime.json"
    runtime = executable(tmp_path / "runtime", "#!/usr/bin/env python3\nimport json,os,sys\nopen(os.environ['WAKE_OUTPUT'],'w').write(json.dumps({'prompt':sys.stdin.read(),'as':os.getenv('ANX_AS'),'adapter':os.getenv('AGENTCTL_ADAPTER')}))\n")
    anx = executable(tmp_path / "anx", "#!/usr/bin/env python3\nimport json,sys,os\na=sys.argv[2:]\nwith open(os.environ['ANX_CALLS'],'a') as f:f.write(json.dumps(a)+'\\n')\nr={'token':'test-token','expires_at':'tomorrow','agent':{'id':'agent-1','handle':'codex.test-host'}} if a[:2]==['host','token'] else {}\nprint(json.dumps({'ok':True,'result':r}))\n")
    calls = tmp_path / "calls.jsonl"
    monkeypatch.setenv("ANX_CALLS", str(calls))
    monkeypatch.setenv("WAKE_OUTPUT", str(output))
    monkeypatch.setenv("PATH", str(tmp_path) + os.pathsep + os.environ["PATH"])
    if agentctl:
        executable(tmp_path / "agentctl", "#!/usr/bin/env python3\nimport json,os,sys,subprocess\na=sys.argv[1:]\nwith open(os.environ['TEST_CTL_CALLS'],'a') as f:f.write(json.dumps(a)+'\\n')\nif a[:3]==['id','generate','exec']:print(json.dumps({'result':{'id':'exec-test-test-test-test-test-test'}}))\nelif a[0]=='run':\n cmd=a[a.index('--')+1:];p=subprocess.run(cmd,input=sys.stdin.read(),text=True,env={**os.environ,'AGENTCTL_ADAPTER':'codex'});sys.exit(p.returncode)\nelse:print(json.dumps({'ok':True}))\n")
        monkeypatch.setenv("TEST_CTL_CALLS", str(tmp_path / "agentctl.jsonl"))
    config_path = tmp_path / "bridge.toml"
    config_path.write_text(f'''[host]\nbase_url = "http://core.test"\nid = "host-1"\nslug = "test-host"\nanx = "{anx}"\nagentctl = "{tmp_path / 'agentctl'}"\nstate_dir = "{tmp_path}"\n[agents.codex]\ncommand = ["{runtime}"]\ncwd = "{tmp_path}"\n''')
    config = load_config(config_path)
    cli = HostCLI(config)
    client = Client(config, cli)
    def handler(request):
        assert request.headers["Authorization"] == "Bearer test-token"
        if request.url.path == "/hosts/host-1":
            return httpx.Response(200, json={"host":{"id":"host-1","slug":"test-host","agents":[{"name":"codex"}],"excluded_names":[]}})
        if request.url.path == "/agent-notifications":
            return httpx.Response(200, json={"items":[{"wakeup_id":"artifact-1","target_handle":"codex.test-host","delivery_status":"requested","trigger_text":"hello","thread_id":"thread-1"}]})
        if request.url.path == "/artifacts/artifact-1/content":
            return httpx.Response(200, json={"subject_ref":"card:task-7","trigger":{"text":"please work"}})
        raise AssertionError(request.url)
    client.http.close()
    client.http = httpx.Client(base_url=config.base_url, transport=httpx.MockTransport(handler))
    return Bridge(config, cli, client), output, calls


@pytest.mark.parametrize("agentctl", [False, True])
def test_host_checkin_and_wake_launch(tmp_path, monkeypatch, agentctl):
    bridge, output, calls = setup_bridge(tmp_path, monkeypatch, agentctl)
    assert bridge.run_once() == 1
    observed = json.loads(output.read_text())
    assert "please work" in observed["prompt"]
    assert observed["as"] == (None if agentctl else "codex")
    if agentctl:
        assert observed["adapter"] == "codex"
        actions = [json.loads(x) for x in (tmp_path / "agentctl.jsonl").read_text().splitlines()]
        assert any(a[:2] == ["subscribe", "create"] and "--destination" in a and "command" in a and "--arg" in a for a in actions)
        assert any(a[0] == "run" and "anx.card.task-7" in a for a in actions)
    actions = [json.loads(x) for x in calls.read_text().splitlines()]
    wakes = [a for a in actions if a[:3] == ["host", "bridge", "wake"]]
    assert ["host", "bridge", "wake", "claim"] == wakes[0][:4]
    assert ["host", "bridge", "wake", "complete"] == wakes[-1][:4]
    assert any(a[:3] == ["host", "bridge", "check-in"] for a in actions)


def test_roster_mismatch_prevents_checkin(tmp_path, monkeypatch):
    bridge, _, calls = setup_bridge(tmp_path, monkeypatch)
    bridge.client.host = lambda name: {"id":"host-1","slug":"test-host","agents":[{"name":"codex"},{"name":"reviewer"}]}
    with pytest.raises(ValueError, match="missing=.*reviewer"):
        bridge.checkin()
    if calls.exists():
        assert not any("check-in" in x for x in calls.read_text().splitlines())


def test_roster_derives_all_configured_agents_before_host_read(tmp_path, monkeypatch):
    bridge, _, _ = setup_bridge(tmp_path, monkeypatch)
    original = bridge.config.runtimes["codex"]
    bridge.config.runtimes["reviewer"] = Runtime("reviewer", "generic", original.command, original.cwd, {})
    derived = []
    bridge.cli.token = lambda name: derived.append(name) or "token"
    def host(_):
        assert derived == ["codex", "reviewer"]
        return {"id":"host-1","slug":"test-host","agents":[{"name":"codex"},{"name":"reviewer"}]}
    bridge.client.host = host
    assert bridge.validate_roster() == ["codex", "reviewer"]


def test_config_rejects_agent_home(tmp_path):
    path = tmp_path / "bridge.toml"
    path.write_text('agent_home = "./old"\n[host]\nbase_url="http://x"\nid="h"\nslug="host"\n[agents.codex]\ncommand=["true"]\n')
    with pytest.raises(ValueError, match="obsolete"):
        load_config(path)
