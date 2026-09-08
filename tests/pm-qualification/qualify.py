#!/usr/bin/env python3
"""Black-box, synthetic qualification against freshly built Nexus binaries.

Only temporary, loopback workspaces are writable targets. No production URL flag.
The report deliberately contains assertions and provenance, never response bodies.
"""
from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]


def utc():
    return dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def clean_env():
    # Do not inherit source credentials, proxies, ANX auth bypasses or bot config.
    return {k: os.environ[k] for k in ("PATH", "HOME", "TMPDIR", "SYSTEMROOT") if k in os.environ}


class Core:
    def __init__(self, binaries, directory, schema):
        self.directory = Path(directory)
        self.directory.mkdir(mode=0o700)
        self.binaries = binaries
        self.schema = schema
        self.token = ""
        self.actor = ""
        self.bootstrap = uuid.uuid4().hex
        self.profile = "qualification-" + uuid.uuid4().hex
        self.process = None
        self.log = None
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            self.port = sock.getsockname()[1]
        self.url = f"http://127.0.0.1:{self.port}"

    def start(self):
        env = clean_env()
        env.update(ANX_BOOTSTRAP_TOKEN=self.bootstrap, ANX_SIDECAR_ROUTER_ENABLED="false",
                   ANX_PROJECTION_MODE="manual", ANX_SHUTDOWN_TIMEOUT="2s")
        self.log = (self.directory / "core.log").open("ab")
        self.process = subprocess.Popen(
            [str(self.binaries["core"]), "--listen-addr", f"127.0.0.1:{self.port}",
             "--workspace-root", str(self.directory / "workspace"),
             "--schema-path", str(self.schema)], env=env, stdout=self.log, stderr=self.log)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            require(self.process.poll() is None, "core exited before readiness; inspect private temporary log")
            try:
                if self.http("GET", "/readyz", token="")[0] == 200:
                    return
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(0.1)
        raise AssertionError("core did not become ready within 20 seconds")

    def stop(self, crash=False):
        if self.process is not None:
            if self.process.poll() is None:
                self.process.send_signal(signal.SIGKILL if crash else signal.SIGTERM)
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait(timeout=5)
            self.process = None
        if self.log is not None:
            self.log.close()
            self.log = None

    def restart(self, crash=False):
        self.stop(crash)
        self.start()

    def http(self, method, path, body=None, token=None):
        require(path.startswith("/") and not path.startswith("//"), "API path must be local")
        headers = {"Accept": "application/json"}
        auth = self.token if token is None else token
        if auth:
            headers["Authorization"] = "Bearer " + auth
        raw = None if body is None else json.dumps(body).encode()
        if raw is not None:
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(self.url + path, raw, headers, method=method)
        # Explicitly ignore host proxy config; disallow redirects away from owned core.
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args, **kwargs):
                return None
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        try:
            response = opener.open(req, timeout=10)
        except urllib.error.HTTPError as exc:
            response = exc
        with response:
            content = response.read(4 * 1024 * 1024 + 1)
            require(len(content) <= 4 * 1024 * 1024, "API output exceeds bound")
            payload = json.loads(content) if content else {}
            return response.code, payload

    def api(self, method, path, body=None, statuses=(200,)):
        status, payload = self.http(method, path, body)
        code = payload.get("error", {}).get("code", "") if isinstance(payload.get("error"), dict) else ""
        require(status in statuses, f"{method} {path.split('?')[0]}: HTTP {status} ({code}), expected {statuses}")
        return payload

    def register(self):
        # RFC 8032 test-vector public key. No private key or user profile is needed:
        # auth bootstrap returns an ephemeral bearer token held only in memory.
        public_key = base64.b64encode(bytes.fromhex(
            "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a")).decode()
        response = self.api("POST", "/auth/agents/register", {
            "username": self.profile, "public_key": public_key,
            "bootstrap_token": self.bootstrap}, (201,))
        self.token = response["tokens"]["access_token"]
        self.actor = response["agent"]["actor_id"]

    def cli(self, *args, body=None, ok=True):
        env = clean_env()
        env["ANX_ACCESS_TOKEN"] = self.token
        result = subprocess.run(
            [str(self.binaries["cli"]), "--json", "--base-url", self.url,
             "--agent", self.profile, *args], input=None if body is None else json.dumps(body),
            capture_output=True, text=True, timeout=20, env=env)
        try:
            payload = json.loads(result.stdout)
        except ValueError:
            raise AssertionError("CLI must emit exactly one JSON envelope") from None
        require(isinstance(payload, dict) and isinstance(payload.get("ok"), bool), "CLI missing boolean ok envelope")
        error_code = payload.get("error", {}).get("code", "") if isinstance(payload.get("error"), dict) else ""
        require((result.returncode == 0) == ok and payload["ok"] == ok,
                f"CLI {' '.join(args[:2])}: unexpected exit {result.returncode} ({error_code})")
        return payload

    def board(self):
        result = self.api("POST", "/boards", {"board": {
            "title": "Synthetic qualification", "owners": [self.actor],
            "refs": [], "pinned_refs": []}}, (201,))
        return result["board"]


def ref(obj, kind="card"):
    return obj.get("ref") or f"{kind}:{obj.get('handle') or obj['id']}"


def item_path(work):
    return "/work/" + urllib.parse.quote(ref(work), safe=":")


def register_work(core, board, native_id="42", authority="github", **extra):
    body = {"board_ref": ref(board, "board"), "title": "Synthetic same title",
            "definition_of_done": ["A separately verified artifact satisfies acceptance"],
            "source": {"authority": authority, "connection_id": "synthetic-local",
                       "native_id": native_id, "native_status": "OPEN"}}
    body.update(extra)
    return core.api("POST", "/work", body, (200, 201))["work"]


def observation(key, sequence, **extra):
    body = {"idempotency_key": key, "reader_id": "synthetic-remote-agent",
            "reader_revision": "qualification-v1", "observed_at": utc(),
            "source_revision": f"synthetic-rev-{sequence}", "source_sequence": sequence,
            "status": "reported", "facts": {"native_status": "IN_PROGRESS", "phase": "in_progress"},
            "evidence": [{"ref": f"synthetic:revision-{sequence}", "summary": "Local fixture; no upstream read"}],
            "stale_after_seconds": 60}
    body.update(extra)
    return body


def submit(core, work, obs):
    return core.api("POST", item_path(work) + "/observations", {"observation": obs}, (200, 201))


def baseline(core, other):
    core.cli("boards", "list")
    for token in ("", other.token):
        require(core.http("GET", "/boards", token=token)[0] in (401, 403),
                "workspace business read accepted absent/foreign-workspace token")
    board = core.board()
    board_id = ref(board, "board")
    core.restart(crash=True)
    persisted = core.api("GET", "/boards/" + urllib.parse.quote(board_id, safe=":"))["board"]
    require(ref(persisted, "board") == board_id, "board identity did not survive crash restart")
    core.cli("boards", "get", board_id)


def authority(core, other):
    board = core.board()
    first = register_work(core, board)
    again = register_work(core, board, title="Changed title, same source identity")
    separate = register_work(core, board, native_id="43")
    other_authority = register_work(core, board, authority="multica")
    require(ref(first) == ref(again), "canonical source identity created duplicate commitments")
    require(len({ref(first), ref(separate), ref(other_authority)}) == 3,
            "equal title or native ID collapsed distinct source ownership")
    status, _ = core.http("PATCH", item_path(first), {
        "if_version": again["version"], "patch": {"phase": "done", "title": "Local overwrite"}})
    require(status in (400, 403, 409, 422), "external workflow fields were locally mutable")
    current = core.api("GET", item_path(first))["work"]
    require(current["phase"] != "done", "rejected external mutation changed stored work")
    for token in ("", other.token):
        require(core.http("GET", item_path(first), token=token)[0] in (401, 403),
                "work leaked across workspace authentication boundary")
    core.cli("work", "get", ref(first))


def replay(core, other):
    work = register_work(core, core.board(), native_id="replay")
    newer = observation("synthetic-replay-20", 20)
    first = submit(core, work, newer)
    second = submit(core, work, newer)
    require(second.get("duplicate") is True, "repeat report not identified as duplicate")
    require(first["observation"]["id"] == second["observation"]["id"], "duplicate has different durable identity")
    older = observation("synthetic-replay-19", 19, facts={"phase": "backlog", "native_status": "OLD"})
    submit(core, work, older)
    core.restart(crash=True)
    after = submit(core, work, newer)
    require(after.get("duplicate") is True, "crash restart lost idempotency record")
    current = core.api("GET", item_path(work))["work"]
    require(current["source"]["native_status"] == "IN_PROGRESS", "older report regressed projected source status")
    conflict = dict(newer, facts={"native_status": "DIFFERENT"})
    status, _ = core.http("POST", item_path(work) + "/observations", {"observation": conflict})
    require(status == 409, "same replay key with different content must conflict")


def outage(core, other):
    work = register_work(core, core.board(), native_id="outage")
    past = (dt.datetime.now(dt.timezone.utc) - dt.timedelta(minutes=5)).isoformat().replace("+00:00", "Z")
    good = submit(core, work, observation("synthetic-stale-good", 1, observed_at=past,
                                       stale_after_seconds=1))["observation"]
    stale = core.api("GET", item_path(work))["work"]
    require(stale["freshness"]["status"] == "stale", "expired source evidence appears fresh")
    submit(core, work, observation("synthetic-outage", 2, status="error", facts={}, evidence=[],
                                 error={"code": "source_unreachable", "message": "Synthetic controlled outage"}))
    failed = core.api("GET", item_path(work))["work"]
    require(failed["latest_observation"]["id"] == good["id"], "failed read replaced last good evidence")
    require(failed["freshness"]["status"] in ("stale", "error"), "failed read appears healthy")
    require(failed["refresh"].get("last_error"), "failed refresh has no visible diagnostic")
    core.restart()
    again = core.api("GET", item_path(work))["work"]
    require(again["latest_observation"]["id"] == good["id"], "restart lost retained last-good evidence")


def attempt_ordering(core, other):
    work = register_work(core, core.board(), native_id="attempt-ordering")
    good = submit(core, work, observation("synthetic-sequenced-good", 10))["observation"]
    failure = observation("synthetic-unsequenced-outage", 11, status="error", facts={}, evidence=[],
                          error={"code": "source_unreachable", "message": "Synthetic outage has no source revision"})
    failure.pop("source_sequence")
    failure.pop("source_revision")
    submit(core, work, failure)
    current = core.api("GET", item_path(work))["work"]
    require(current["latest_observation"]["id"] == good["id"], "unsequenced failure replaced good source state")
    require(current["freshness"]["status"] == "error" and current["refresh"].get("last_error"),
            "outage without source sequence was hidden after sequenced success")
    core.restart(crash=True)
    current = core.api("GET", item_path(work))["work"]
    require(current["freshness"]["status"] == "error", "restart lost unsequenced outage visibility")


def completion(core, other):
    work = register_work(core, core.board(), native_id="run-not-work")
    status, _ = core.http("POST", item_path(work) + "/observations", {"observation":
        observation("synthetic-run-completed", 1, status="verified",
                    facts={"phase": "done", "native_status": "OPEN", "run_status": "completed"}, evidence=[])})
    require(status in (200, 201, 400, 422), "completion gate returned unexpected response")
    current = core.api("GET", item_path(work))["work"]
    require(current["phase"] != "done", "successful run or self-attestation completed unaccepted work")
    # A rejected empty completion is valid enforcement. Independently test that
    # an otherwise valid observation cannot promote its own verification label.
    submit(core, work, observation("synthetic-self-attestation", 2, status="verified"))
    latest = core.api("GET", item_path(work))["work"]["latest_observation"]
    require(latest.get("verification") != "verified", "untrusted reporter self-upgraded verification")


def views(core, other):
    board = core.board()
    work = register_work(core, board, native_id="view-independence",
                         due_at="2030-01-02T00:00:00Z", relations=[{"kind": "related", "ref": ref(board, "board")}])
    before = core.api("GET", item_path(work))["work"]
    core.api("GET", "/inbox")
    after = core.api("GET", item_path(work))["work"]
    require(before == after, "reading inbox changed work")
    listed = core.api("GET", "/work?limit=200")["work"]
    board_cards = core.api("GET", "/boards/" + urllib.parse.quote(ref(board, "board"), safe=":") + "/cards")
    require(any(ref(w) == ref(work) for w in listed), "table projection omitted registered work")
    require(ref(work) in json.dumps(board_cards) or work.get("id", "missing") in json.dumps(board_cards),
            "existing board projection omitted work card")
    require(after.get("due_at") == "2030-01-02T00:00:00Z" and after.get("relations"), "date/relation metadata was lost")
    core.cli("work", "list", "--limit", "1")


SCENARIOS = {"baseline": baseline, "authority": authority, "replay": replay,
             "outage": outage, "attempt_ordering": attempt_ordering,
             "completion": completion, "views": views}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--suite", choices=("baseline", "work", "all"), default="all")
    parser.add_argument("--report", type=Path, help="Write redacted JSON result to this path")
    parser.add_argument("--build-root", type=Path, default=ROOT, help="Integrated checkout to build (read-only)")
    args = parser.parse_args()
    root = args.build_root.resolve()
    report = {"schema_version": 1, "evidence_class": "synthetic_local_real_binaries",
              "started_at": utc(), "results": [], "not_verified": [
                  "real GitHub/Multica collector", "real SSH remote/recovery", "real Telegram/Discord conversations",
                  "PM downstream delivery and reconciliation", "Linux generated reader runtime isolation",
                  "browser rendering/accessibility", "CI/deployment/production behavior"]}
    report["source_head"] = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
    report["source_dirty"] = bool(subprocess.check_output(["git", "-C", str(root), "status", "--porcelain"], text=True).strip())
    selected = list(SCENARIOS) if args.suite == "all" else (["baseline"] if args.suite == "baseline" else list(SCENARIOS)[1:])
    # Cleanup all workspace state, credentials and logs even after failed assertions.
    with tempfile.TemporaryDirectory(prefix="anx-pm-qualification-") as temp:
        binaries = {"core": Path(temp) / "anx-core", "cli": Path(temp) / "anx"}
        build_env = dict(os.environ)
        # PATH selects the Go installation. An inherited goenv GOROOT can point
        # at a different compiler and cause misleading standard-library failures.
        build_env.pop("GOROOT", None)
        build_env["GOCACHE"] = str(Path(temp) / "go-cache")
        for module, package in (("core", "./cmd/anx-core"), ("cli", "./cmd/anx")):
            built = subprocess.run(["go", "build", "-o", str(binaries[module]), package],
                                   cwd=root / module, env=build_env, capture_output=True, text=True, timeout=240)
            if built.returncode:
                print(f"BUILD FAIL: {module}; compiler output withheld from report")
                print(built.stderr, flush=True)
                report["results"].append({"scenario": "build_" + module, "status": "fail"})
                break
            report.setdefault("binary_sha256", {})[module] = hashlib.sha256(binaries[module].read_bytes()).hexdigest()
        else:
            for name in selected:
                cores = [Core(binaries, Path(temp) / f"{name}-{n}", root / "contracts/anx-schema.yaml") for n in (1, 2)]
                started = time.monotonic()
                try:
                    for core in cores:
                        core.start()
                        core.register()
                    SCENARIOS[name](*cores)
                    result = {"scenario": name, "status": "pass"}
                except Exception as exc:
                    # HTTP assertions are intentionally payload-free. Unexpected exception
                    # details may contain request data; record only its type.
                    result = {"scenario": name, "status": "fail", "reason":
                              str(exc) if isinstance(exc, AssertionError) else type(exc).__name__}
                finally:
                    for core in cores:
                        core.stop()
                result["duration_seconds"] = round(time.monotonic() - started, 3)
                report["results"].append(result)
                print(json.dumps(result), flush=True)
    report["finished_at"] = utc()
    report["passed"] = len(report["results"]) == len(selected) and all(r["status"] == "pass" for r in report["results"])
    if args.report:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, indent=2) + "\n")
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
