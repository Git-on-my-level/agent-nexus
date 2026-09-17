#!/usr/bin/env python3
"""Read one approved real source, import into temporary core, verify with real CLI.

Source configuration uses anx-observe's operator-owned JSON format. No upstream
writes occur. Real source bodies exist only in memory and temporary local state,
which is deleted. Reports contain source fingerprints and assertion metadata.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import uuid

from qualify import Core, ROOT, item_path, ref, require, utc


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True, help="Approved existing anx-observe source JSON")
    parser.add_argument("--build-root", type=Path, default=ROOT)
    parser.add_argument("--report", type=Path)
    parser.add_argument("--controlled-ssh-outage", action="store_true",
                        help="After successful SSH read, use a temporary empty known_hosts to verify failure retention")
    args = parser.parse_args()
    root = args.build_root.resolve()
    config = json.loads(args.config.read_text())
    target = config["target"]
    require(target["source"] in ("github", "multica", "ssh_git"), "unsupported source")
    require(not args.controlled_ssh_outage or target["source"] == "ssh_git", "controlled outage requires SSH source")
    report = {"schema_version": 1, "evidence_class": "real_source_read_into_synthetic_local_workspace",
              "started_at": utc(), "source": target["source"],
              "target_sha256": hashlib.sha256(json.dumps(target, sort_keys=True).encode()).hexdigest(),
              "source_head": subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip(),
              "source_dirty": bool(subprocess.check_output(["git", "-C", str(root), "status", "--porcelain"], text=True).strip()),
              "limitations": ["No production tracker writes", "No scheduled collection, browser or second-machine CLI proof",
                              "Source-native completion is not independently verified acceptance"]}
    env = dict(os.environ)
    env.pop("GOROOT", None)
    with tempfile.TemporaryDirectory(prefix="anx-real-source-qualification-") as tmp:
        temp = Path(tmp)
        env["GOCACHE"] = str(temp / "cache")
        binaries = {key: temp / key for key in ("core", "cli", "observe")}
        core = None
        try:
            for key, module, package in (("core", "core", "./cmd/anx-core"), ("cli", "cli", "./cmd/anx"), ("observe", "core", "./cmd/anx-observe")):
                result = subprocess.run(["go", "build", "-mod=readonly", "-o", str(binaries[key]), package],
                                        cwd=root / module, env=env, capture_output=True, text=True, timeout=240)
                require(result.returncode == 0, "build failed: " + key)
                report.setdefault("binary_sha256", {})[key] = hashlib.sha256(binaries[key].read_bytes()).hexdigest()
            result = subprocess.run([str(binaries["observe"]), "--config", str(args.config.resolve()), "--core-envelope", "read"],
                                    capture_output=True, text=True, timeout=75)
            require(result.returncode == 0, "approved real-source reader failed; raw source output withheld")
            obs = json.loads(result.stdout)
            core = Core(binaries, temp / "core-state", root / "contracts/anx-schema.yaml")
            core.start()
            core.register()
            board = core.board()
            native = target["native_id"]
            if target["source"] == "github":
                native = target["repository"] + "#" + native
            source = {"authority": target["source"], "connection_id": target["connection_id"], "native_id": native}
            work = core.api("POST", "/work", {"board_ref": ref(board, "board"),
                "title": "Temporary qualification of approved real source", "source": source}, (201,))["work"]
            first = core.api("POST", item_path(work) + "/observations", {"observation": obs})
            duplicate = core.api("POST", item_path(work) + "/observations", {"observation": obs})
            require(duplicate.get("duplicate") is True, "real-source observation replay duplicated")
            require(first["observation"]["id"] == duplicate["observation"]["id"], "replay changed durable observation identity")
            current = core.api("GET", item_path(work))["work"]
            require(current["source"]["native_id"] == native, "source identity changed during local import")
            require(current["latest_observation"]["verification"] == "reported", "source report self-certified acceptance")
            cli = core.cli("work", "get", ref(work))
            body = cli.get("data", {}).get("body", cli.get("data", {}))
            require(body.get("work", {}).get("ref") == ref(work), "CLI and API disagree on imported work identity")
            if args.controlled_ssh_outage:
                # Keep host/path unchanged and keep strict checking enabled. Only
                # this test-owned invocation loses trust; operator files stay intact.
                known_hosts = temp / "empty-known-hosts"
                known_hosts.write_text("")
                failure_config = dict(config, known_hosts_file=str(known_hosts))
                failure_path = temp / "outage-config.json"
                failure_path.write_text(json.dumps(failure_config))
                failure_path.chmod(0o600)
                failed = subprocess.run([str(binaries["observe"]), "--config", str(failure_path), "read"],
                                        capture_output=True, text=True, timeout=75)
                require(failed.returncode != 0, "controlled missing-host-trust read unexpectedly succeeded")
                error_observation = {"idempotency_key": "controlled-outage-" + uuid.uuid4().hex,
                    "reader_id": obs["reader_id"], "reader_revision": obs["reader_revision"], "observed_at": utc(),
                    "status": "error", "facts": {}, "evidence": [],
                    "error": {"code": "unavailable", "message": "Controlled empty-known-hosts reader failure"}}
                core.api("POST", item_path(work) + "/observations", {"observation": error_observation})
                current = core.api("GET", item_path(work))["work"]
                require(current["freshness"]["status"] == "error", "actual failed SSH attempt is not visible")
                require(current["latest_observation"]["id"] == first["observation"]["id"], "actual failed SSH read replaced last-good evidence")
            core.restart(crash=True)
            require(core.api("GET", item_path(work))["work"]["latest_observation"]["id"] == first["observation"]["id"], "restart lost real-source evidence")
            report.update(status="pass", reader_id=obs["reader_id"], reader_revision=obs["reader_revision"],
                          observed_at=obs["observed_at"], source_revision=obs.get("source_revision"),
                          coverage=obs.get("coverage"), assertions=["real reader", "canonical authority", "duplicate replay", "API/CLI identity parity", "claims remain reported", "crash persistence"])
            if args.controlled_ssh_outage:
                require(core.api("GET", item_path(work))["work"]["freshness"]["status"] == "error", "restart lost SSH outage visibility")
                report["assertions"].append("controlled real SSH trust failure retains last-good evidence and error across crash")
                report["limitations"].append("Outage uses a temporary empty known_hosts; failure mapping into core is performed by the harness")
        except Exception as exc:
            report.update(status="fail", reason=str(exc) if isinstance(exc, AssertionError) else type(exc).__name__)
        finally:
            if core is not None:
                core.stop()
    report["finished_at"] = utc()
    print(json.dumps(report, indent=2))
    if args.report:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, indent=2) + "\n")
    return 0 if report["status"] == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
