#!/usr/bin/env python3
"""Bounded synthetic-only idle profiling: one 1-CPU / 1-GiB container per run."""

import argparse
import json
import pathlib
import subprocess
import threading
import time
import uuid


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


p = argparse.ArgumentParser(description=__doc__)
p.add_argument("--binary", type=pathlib.Path, required=True)
p.add_argument("--workspace", type=pathlib.Path, required=True)
p.add_argument("--output", type=pathlib.Path, required=True)
p.add_argument("--schema", type=pathlib.Path, default=pathlib.Path("contracts/anx-schema.yaml"))
p.add_argument("--platform", default="linux/arm64")
p.add_argument("--seconds", type=int, default=430)
a = p.parse_args()
if not 410 <= a.seconds <= 580:
    p.error("--seconds must be 410..580; hard container lifetime is under ten minutes")
if (a.workspace / ".synthetic-idle-fixture").read_text() != "synthetic idle fixture\n":
    p.error("refusing an unmarked workspace")
key = (a.workspace / ".synthetic-heartbeat-key").read_text().strip()
for path in (a.binary, a.schema, a.workspace / "state.sqlite"):
    if not path.is_file():
        p.error(f"missing input: {path}")
out = a.output
out.mkdir(parents=True, exist_ok=False)
name = "anx-idle-" + uuid.uuid4().hex[:12]
env = {
    "ANX_WORKSPACE_ROOT": "/state",
    "ANX_SCHEMA_PATH": "/schema.yaml",
    "ANX_DEBUG_PPROF_ADDR": "127.0.0.1:6060",
    "ANX_BOOTSTRAP_TOKEN": "synthetic-idle-bootstrap-token-0123456789",
    "ANX_HEARTBEAT_PUBLISHER_URL": "http://127.0.0.1:6062",
    "ANX_WORKSPACE_SERVICE_ID": "synthetic-idle",
    "ANX_WORKSPACE_SERVICE_PRIVATE_KEY": key,
}
command = [
    "docker", "create", "--platform", a.platform, "--name", name,
    "--cpus=1", "--memory=1g", "--memory-swap=1g", "--tmpfs", "/tmp:size=64m",
]
for field, value in env.items():
    command.extend(["-e", f"{field}={value}"])
command.extend(["python:3.11-slim", "timeout", "-k", "5", "585", "/core"])
run(*command, stdout=subprocess.DEVNULL)
errors = []
threads = []


def profile(window):
    code = (
        "import urllib.request,sys;sys.stdout.buffer.write(urllib.request.urlopen("
        "'http://127.0.0.1:6060/debug/pprof/profile?seconds=45',timeout=60).read())"
    )
    with (out / f"{window}.pprof").open("wb") as target:
        result = subprocess.run(
            ["docker", "exec", name, "python3", "-c", code],
            stdout=target, stderr=subprocess.PIPE,
        )
    (out / f"{window}.stderr").write_bytes(result.stderr)
    if result.returncode or not (out / f"{window}.pprof").stat().st_size:
        errors.append(f"{window} profile failed")


try:
    run("docker", "cp", str(a.binary.resolve()), name + ":/core")
    run("docker", "cp", str(a.workspace.resolve()), name + ":/state")
    run("docker", "cp", str(a.schema.resolve()), name + ":/schema.yaml")
    run("docker", "start", name, stdout=subprocess.DEVNULL)
    collector = """from http.server import HTTPServer, BaseHTTPRequestHandler
class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get('Content-Length', 0)))
        self.send_response(204)
        self.end_headers()
    def log_message(self, *args):
        pass
HTTPServer(('127.0.0.1', 6062), Handler).serve_forever()
"""
    run("docker", "exec", "-d", name, "python3", "-c", collector)
    start = time.monotonic()
    started = set()
    with (out / "cpu.jsonl").open("w") as samples:
        while time.monotonic() - start < a.seconds:
            elapsed = time.monotonic() - start
            # Cgroup counters include the receiver and probes: conservative CPU.
            code = (
                "import json,pathlib;print(json.dumps({k:int(v) for k,v in "
                "(x.split() for x in pathlib.Path('/sys/fs/cgroup/cpu.stat')"
                ".read_text().splitlines())}|{'memory_bytes':int(pathlib.Path("
                "'/sys/fs/cgroup/memory.current').read_text())}))"
            )
            raw = subprocess.check_output(
                ["docker", "exec", name, "python3", "-c", code], text=True,
            )
            data = json.loads(raw)
            data["elapsed"] = elapsed
            samples.write(json.dumps(data) + "\n")
            samples.flush()
            for window, at in [("startup", 5), ("steady", 360)]:
                if elapsed >= at and window not in started:
                    started.add(window)
                    thread = threading.Thread(target=profile, args=(window,))
                    thread.start()
                    threads.append(thread)
            time.sleep(5)
    for thread in threads:
        thread.join()
    if errors:
        raise SystemExit("; ".join(errors))
finally:
    subprocess.run(["docker", "stop", "-t", "5", name], stdout=subprocess.DEVNULL)
    with (out / "core.log").open("wb") as logs:
        subprocess.run(
            ["docker", "logs", "--timestamps", name], stdout=logs, stderr=subprocess.STDOUT,
        )
    subprocess.run(["docker", "rm", name], stdout=subprocess.DEVNULL)
