"""Real core, human-approved host, derived mention, fake runtime (no model)."""
import base64
import hashlib
import json
import os
import shutil
import socket
import subprocess
import sys
import time
from pathlib import Path

import httpx
import pytest
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization

from anx_agent_bridge.bridge import Bridge
from anx_agent_bridge.config import load_config

REPO = Path(__file__).resolve().parents[3]
CORE = REPO / "core"
CLI = REPO / "cli"
PORT = 8093
BASE = f"http://127.0.0.1:{PORT}"


def post(client, path, body, token="", status=200):
    response = client.post(path, json=body, headers={"Authorization": f"Bearer {token}"} if token else {})
    assert response.status_code == status, (path, response.status_code, response.text)
    return response.json()


def executable(path, source):
    path.write_text(source.replace("#!/usr/bin/env python3", "#!" + sys.executable, 1))
    path.chmod(0o700)
    return path


@pytest.mark.parametrize("agentctl", [False, True])
def test_real_core_host_mention_wakes_stub(tmp_path, monkeypatch, agentctl):
    work = REPO / ".tmp" / f"bridge-e2e-{os.getpid()}-{int(agentctl)}"
    work.mkdir(parents=True, exist_ok=True)
    try:
        with socket.socket() as sock:
            assert sock.connect_ex(("127.0.0.1", PORT)) != 0, "assigned core port 8093 is occupied"
        binary = work / "anx-core"
        build = subprocess.run(["go", "build", "-o", str(binary), "./cmd/anx-core"], cwd=CORE,
                               env={**os.environ, "GOCACHE": "/private/tmp/anx-cc-go-cache"}, capture_output=True, text=True)
        assert build.returncode == 0, build.stderr
        anx = work / "anx"
        cli_build = subprocess.run(["go", "build", "-o", str(anx), "./cmd/anx"], cwd=CLI,
                                   env={**os.environ, "GOCACHE": "/private/tmp/anx-cc-go-cache"}, capture_output=True, text=True)
        assert cli_build.returncode == 0, cli_build.stderr
        (work / "workspace").mkdir()
        (work / "workspace" / ".anx-dev-insecure-auth").touch()
        core_env = {**os.environ, "ANX_PORT": str(PORT), "ANX_WORKSPACE_ROOT": str(work / "workspace"),
                    "ANX_BLOB_BACKEND": "filesystem", "ANX_SCHEMA_PATH": str(REPO / "contracts/anx-schema.yaml"),
                    "ANX_ALLOW_PASSKEY_DEV_BYPASS": "1", "ANX_ENABLE_DEV_ACTOR_MODE": "1",
                    "ANX_HOSTED_DEV_MODE": "1", "ANX_BOOTSTRAP_TOKEN": "bridge-e2e-bootstrap-token-1234567890",
                    "ANX_SIDECAR_ROUTER_POLL_INTERVAL": "100ms", "ANX_SIDECAR_ROUTER_PRINCIPAL_CACHE_TTL": "100ms"}
        with (work / "core.log").open("w") as log, subprocess.Popen([str(binary)], cwd=CORE, env=core_env, stdout=log, stderr=log) as core:
            try:
                with httpx.Client(base_url=BASE, timeout=5) as http:
                    for _ in range(100):
                        try:
                            if http.get("/readyz").status_code == 200:
                                break
                        except httpx.TransportError:
                            pass
                        time.sleep(0.1)
                    else:
                        pytest.fail((work / "core.log").read_text())
                    human = post(http, "/auth/passkey/dev/register", {"display_name":"Operator", "bootstrap_token":"bridge-e2e-bootstrap-token-1234567890"}, status=201)
                    admin = human["tokens"]["access_token"]
                    private = Ed25519PrivateKey.generate()
                    public = private.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
                    seed = private.private_bytes(serialization.Encoding.Raw, serialization.PrivateFormat.Raw, serialization.NoEncryption())
                    start = post(http, "/auth/hosts/enrollments", {"public_key":base64.b64encode(public).decode(), "requested_slug":"bridge-test", "os_user":"test", "hostname":"test", "discovered_adapters":["codex"], "request_nonce":base64.urlsafe_b64encode(public[:16]).decode().rstrip("="), "adoptions":[]}, status=201)
                    enrollment, poll = start["enrollment_id"], start["poll_token"]
                    post(http, f"/auth/hosts/enrollments/{enrollment}/approve", {}, admin)
                    signature = base64.b64encode(private.sign(f"anx-host-enroll-complete|{enrollment}|{poll}".encode())).decode()
                    host = post(http, f"/auth/hosts/enrollments/{enrollment}/complete", {"poll_token":poll, "signature":signature}, status=201)["host"]
                    config_dir = work / "anx-config"
                    workspace_id = host.get("workspace_id", "")
                    workspace_key = workspace_id if workspace_id and workspace_id[0].isalnum() and all(c.isalnum() or c in "_-" for c in workspace_id) and len(workspace_id) <= 128 else hashlib.sha256(BASE.encode()).hexdigest()[:24]
                    host_dir = config_dir / "hosts" / workspace_key
                    host_dir.mkdir(parents=True, mode=0o700)
                    key_path = host_dir / "host.ed25519"
                    key_path.write_text(base64.b64encode(seed + public).decode() + "\n")
                    key_path.chmod(0o600)
                    record = {"id":host["id"], "key_id":host["key_id"], "slug":host["slug"],
                              "workspace_id":workspace_id, "base_url":BASE, "private_key_path":str(key_path)}
                    (host_dir / "host.json").write_text(json.dumps(record))
                    (host_dir / "host.json").chmod(0o600)
                    output = work / "runtime.json"
                    monkeypatch.setenv("WAKE_OUTPUT", str(output))
                    runtime = executable(work / "runtime", '''#!/usr/bin/env python3
import json,os,sys
open(os.environ['WAKE_OUTPUT'],'w').write(json.dumps({'prompt':sys.stdin.read(),'as':os.getenv('ANX_AS'),'adapter':os.getenv('AGENTCTL_ADAPTER')}))
''')
                    ctl = work / "agentctl"
                    if agentctl:
                        executable(ctl, '''#!/usr/bin/env python3
import json,os,subprocess,sys
args=sys.argv[1:]
if args[:3]==['id','generate','exec']:print(json.dumps({'result':{'id':'exec-test-test-test-test-test-test'}}))
elif args[0]=='run':
 r=subprocess.run(args[args.index('--')+1:],input=sys.stdin.read(),text=True,env={**os.environ,'AGENTCTL_ADAPTER':'codex'});sys.exit(r.returncode)
else:print(json.dumps({'ok':True}))
''')
                    config_path = work / "bridge.toml"
                    config_path.write_text(f'[host]\nbase_url="{BASE}"\nid="{host["id"]}"\nslug="bridge-test"\nconfig_dir="{config_dir}"\nanx="{anx}"\nagentctl="{ctl}"\n[agents.codex]\ncommand=["{runtime}"]\ncwd="{work}"\n')
                    bridge = Bridge(load_config(config_path))
                    bridge.checkin()  # derives codex, then checks in the host
                    topic = post(http, "/topics", {"actor_id":human["agent"]["actor_id"],"topic":{"title":"Wake topic","summary":"test","owner_refs":[],"document_refs":[],"board_refs":[],"related_refs":[],"provenance":{"sources":["inferred"]}}}, admin, 201)["topic"]
                    thread = topic["thread_id"]
                    post(http, "/events", {"event":{"type":"message_posted","thread_id":thread,"summary":"@codex.bridge-test please inspect","refs":[f"thread:{thread}"],"payload":{"text":"@codex.bridge-test please inspect"},"provenance":{"sources":["inferred"]}}}, admin, 201)
                    for _ in range(100):
                        if bridge.run_once() and output.exists():
                            break
                        time.sleep(0.1)
                    else:
                        pytest.fail("bridge did not launch stub runtime: " + (work / "core.log").read_text()[-3000:])
                    observed = json.loads(output.read_text())
                    assert "@codex.bridge-test" in observed["prompt"]
                    assert observed["as"] == (None if agentctl else "codex")
                    assert observed["adapter"] == ("codex" if agentctl else None)
            finally:
                core.terminate()
                core.wait(timeout=10)
    finally:
        shutil.rmtree(work, ignore_errors=True)
