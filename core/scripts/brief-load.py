#!/usr/bin/env python3
"""Seed or measure a disposable local synthetic brief workspace. No network."""

import argparse
import os
from pathlib import Path
import shutil
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["seed", "measure"])
    parser.add_argument("directory", type=Path, help="scratch output directory")
    parser.add_argument("--label", default="sample")
    args = parser.parse_args()
    root = args.directory.resolve()
    root.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, TMPDIR=str(root))
    env.pop("ANX_BRIEF_LOAD_SEED", None)
    env.pop("ANX_BRIEF_LOAD_PROFILES", None)
    fixture = root / "seed"
    if args.action == "seed":
        if fixture.exists():
            parser.error("seed already exists; choose an empty scratch directory")
        env["ANX_BRIEF_LOAD_SEED"] = "1"
        workspace = fixture
    else:
        if not args.label.replace("-", "").replace("_", "").isalnum():
            parser.error("label must be a simple filename")
        workspace = root / args.label
        # Seed process has exited: copy the DB and any WAL together, offline.
        shutil.copytree(fixture, workspace)
        env["ANX_BRIEF_LOAD_PROFILES"] = str(root / (args.label + "-profiles"))
    env["ANX_BRIEF_LOAD_ROOT"] = str(workspace)
    repo = Path(__file__).resolve().parents[2]
    command = ["go", "test", "-C", str(repo / "core"), "./internal/server",
               "-run", "^TestBriefLoadProfile$", "-count=1", "-v", "-timeout=10m"]
    with (root / (args.action + "-" + args.label + ".log")).open("w") as log:
        result = subprocess.run(command, env=env, stdout=log, stderr=subprocess.STDOUT, check=False)
    raise SystemExit(result.returncode)


if __name__ == "__main__":
    main()
