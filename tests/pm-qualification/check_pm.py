#!/usr/bin/env python3
"""Run independent PM public-service acceptance in an isolated temporary Go module."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build-root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    root = args.build_root.resolve()
    source = Path(__file__).resolve().parent
    env = dict(os.environ)
    env.pop("GOROOT", None)
    env["GOWORK"] = "off"
    with tempfile.TemporaryDirectory(prefix="anx-pm-service-qualification-") as temp:
        directory = Path(temp)
        env["GOCACHE"] = str(directory / "cache")
        # The module path is inside the core import boundary, allowing independent
        # tests of the public PM API without adding tests to peer-owned core files.
        (directory / "go.mod").write_text("module agent-nexus-core/qualification\n\ngo 1.23.0\n\nrequire agent-nexus-core v0.0.0\n\nreplace agent-nexus-core => " + json.dumps(str(root / "core")) + "\n")
        shutil.copyfile(root / "core/go.sum", directory / "go.sum")
        for test_file in source.glob("*_acceptance_test.go"):
            shutil.copyfile(test_file, directory / test_file.name)
        result = subprocess.run(["go", "test", "-mod=mod", "-json", "-count=1", "-timeout=90s", "./..."],
                                cwd=directory, env=env, capture_output=True, text=True, timeout=180)
        results = []
        for line in result.stdout.splitlines():
            try:
                event = json.loads(line)
            except ValueError:
                continue
            if event.get("Test") and event.get("Action") in ("pass", "fail", "skip"):
                results.append({"test": event["Test"], "status": event["Action"]})
            if event.get("Action") == "output" and event.get("Test"):
                print(event.get("Output", ""), end="")
        report = {"schema_version": 1, "evidence_class": "synthetic_public_service_sqlite",
                  "source_head": subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip(),
                  "source_dirty": bool(subprocess.check_output(["git", "-C", str(root), "status", "--porcelain"], text=True).strip()),
                  "exit_code": result.returncode, "results": results,
                  "limitations": ["Synthetic source/bridge/policy callbacks", "No real channel or server authentication evidence"]}
        if not results:
            print("No acceptance tests ran; compilation/dependency failure. Details are not a passing result.")
            print(result.stderr)
        if args.report:
            args.report.parent.mkdir(parents=True, exist_ok=True)
            args.report.write_text(json.dumps(report, indent=2) + "\n")
        return 0 if result.returncode == 0 and results and all(r["status"] == "pass" for r in results) else 1


if __name__ == "__main__":
    raise SystemExit(main())
