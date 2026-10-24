"""Run one shard of the advisory legacy performance tests.

Tests are discovered with `go test -list`, so a new TestPerformance* test lands
in exactly one shard without editing this script. Longest-processing-time
balancing over measured CI seconds keeps each shard well inside its timeout.
Run from core/: python3 ../scripts/performance-legacy-shard.py --shard 1 --shards 3
"""

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

PACKAGES = ("./internal/server", "./internal/storage")
SELECT = r"^TestPerformance|^TestResourceAccess(CommonReadPerformance|LargeDenialPrepareAndPMRoutes)$"
# The route matrix has its own four-shard job.
EXCLUDE = {"./internal/server": {"TestPerformanceRoutes"}}
DEFAULT_SECONDS = 45  # one shared-corpus fixture plus its samples


def discover(packages=PACKAGES):
    tests = []
    for package in packages:
        listed = subprocess.run(
            ["go", "test", "-list", SELECT, package],
            check=True,
            capture_output=True,
            text=True,
        ).stdout
        for name in listed.split():
            excluded = EXCLUDE.get(package, ())
            if re.fullmatch(r"Test\w*", name) and name not in excluded:
                tests.append((package, name))
    return tests


def partition(tests, weights, shards):
    """Assign each (package, test) to exactly one shard, heaviest first."""
    loads = [0.0] * shards
    out = [[] for _ in range(shards)]
    ordered = sorted(
        tests, key=lambda t: (-weights.get(t[1], DEFAULT_SECONDS), t[0], t[1])
    )
    for test in ordered:
        shard = min(range(shards), key=lambda i: (loads[i], i))
        loads[shard] += weights.get(test[1], DEFAULT_SECONDS)
        out[shard].append(test)
    return out, loads


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--shard", type=int, required=True)
    parser.add_argument("--shards", type=int, required=True)
    parser.add_argument("--log-dir", type=Path, required=True)
    args = parser.parse_args()
    if not 1 <= args.shard <= args.shards:
        parser.error("--shard must be within 1..--shards")
    weights_path = Path(__file__).resolve().parent / "performance-legacy-weights.json"
    weights = json.loads(weights_path.read_text())
    assigned, loads = partition(discover(), weights, args.shards)
    mine = assigned[args.shard - 1]
    print(
        f"shard {args.shard}/{args.shards}: {len(mine)} tests, "
        f"estimated {loads[args.shard - 1]:.0f}s",
        flush=True,
    )
    args.log_dir.mkdir(parents=True, exist_ok=True)
    result = 0
    for package in PACKAGES:
        names = sorted(name for p, name in mine if p == package)
        if not names:
            continue
        log = args.log_dir / f"legacy-{Path(package).name}.log"
        command = [
            "go",
            "test",
            "-p=1",
            "-parallel=1",
            "-timeout=10m",
            "-count=1",
            "-v",
            package,
            "-run",
            "^(" + "|".join(names) + ")$",
        ]
        print(" ".join(command), flush=True)
        with log.open("w") as out:
            process = subprocess.Popen(
                command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True
            )
            for line in process.stdout:
                sys.stdout.write(line)
                out.write(line)
            if process.wait() != 0:
                result = 1
    return result


if __name__ == "__main__":
    raise SystemExit(main())
