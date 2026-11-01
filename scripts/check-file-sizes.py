#!/usr/bin/env python3
"""Ratchet keeping hand-written source files small.

A file that is not in the baseline may never exceed the limit; a baselined
file may never grow past its recorded count. Anything else -- a baselined
file that shrank, moved under the limit or disappeared -- only makes the
baseline stale, and the fix is `make file-size-baseline`, so the baseline
moves down in normal use and never up without a recorded reason
(`--allow-growth "<reason>"`, printed for the PR description).

Tests, spec files and generated trees are not hand-written, so they carry no
ratchet. Line counts match `wc -l` (newline count).
"""

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BASELINE = ROOT / "scripts/file-size-baseline.json"
LIMIT = 800
SUFFIXES = (".go", ".py", ".js", ".mjs", ".ts", ".svelte")
# Generated trees the contract generators own; mirrors the prettier and drift
# exclusions in scripts/git-hooks/checks.py.
GENERATED_PREFIXES = ("contracts/gen/", "cli/docs/generated/", "web-ui/src/lib/generated/")
# _gen.go covers sqlc/scopedrepo-gen output; _generated.go covers help and version files.
GENERATED_NAMES = ("_test.go", ".test.", ".spec.", "_generated.go", "_gen.go")


def is_handwritten(path):
    name = Path(path).name
    return (name.endswith(SUFFIXES)
            and not path.startswith(GENERATED_PREFIXES)
            and not any(mark in name for mark in GENERATED_NAMES))


def tracked_files(root):
    listing = subprocess.check_output(["git", "ls-files", "-z"], cwd=root,
                                      stderr=subprocess.PIPE)
    return sorted(os.fsdecode(p) for p in listing.split(b"\0") if p)


def line_count(root, path):
    return (root / path).read_bytes().count(b"\n")


def oversized(root):
    """Every tracked hand-written file over the limit, as {path: line count}."""
    found = {}
    for path in tracked_files(root):
        if is_handwritten(path) and (root / path).is_file():
            count = line_count(root, path)
            if count > LIMIT:
                found[path] = count
    return found


def load_baseline(required=True):
    if not BASELINE.is_file():
        if required:
            raise SystemExit("Missing scripts/file-size-baseline.json; run make file-size-baseline.")
        return {}
    return json.loads(BASELINE.read_text())


def check(root):
    current = oversized(root)
    recorded = load_baseline()
    problems = []
    for path, count in current.items():
        if path not in recorded:
            problems.append(
                f"{path} is {count} lines (limit {LIMIT}) and not in the baseline: "
                f"split {path} by concern before adding code, or record the growth with "
                "make file-size-baseline ARGS='--allow-growth \"<reason>\"'")
        elif count > recorded[path]:
            problems.append(
                f"{path} grew from {recorded[path]} to {count} lines: "
                f"split {path} by concern before adding code")
    for path, count in sorted(recorded.items()):
        now = current.get(path)
        if now is None:
            # Below the limit, excluded, or deleted; say which, so the refresh
            # instruction explains itself.
            if (root / path).is_file() and is_handwritten(path):
                state = "dropped under the limit"
            elif (root / path).is_file():
                state = "no longer counts as hand-written"
            else:
                state = "is gone"
            problems.append(
                f"baseline entry {path} (recorded {count} lines) {state}: "
                "run make file-size-baseline to record the improvement")
        elif now < count:
            state = (f"shrank to {now} lines" if now > LIMIT
                     else "dropped under the limit")
            problems.append(
                f"baseline entry {path} (recorded {count} lines) {state}: "
                "run make file-size-baseline to record the improvement")
        # now > count is growth, already reported above with its split instruction.
    if problems:
        raise SystemExit("File-size ratchet:\n" + "\n".join(problems))
    print(f"File sizes within the ratchet ({len(recorded)} baselined files over {LIMIT} lines).")


def write(root, allow_growth):
    current = oversized(root)
    # Bootstrap-tolerant: the very first baseline has nothing to compare against.
    recorded = load_baseline(required=False)
    added = sorted(set(current) - set(recorded))
    raised = sorted(path for path in set(current) & set(recorded)
                    if current[path] > recorded[path])
    if (added or raised) and not allow_growth:
        details = [f"{path} would be added at {current[path]} lines" for path in added]
        details += [f"{path} would grow from {recorded[path]} to {current[path]} lines"
                    for path in raised]
        raise SystemExit(
            "Refusing to grow the file-size baseline:\n" + "\n".join(details)
            + "\nSplit the file by concern instead, or pass a reason that will be "
            "printed for the PR description:\n"
            "  make file-size-baseline ARGS='--allow-growth \"<reason>\"'")
    BASELINE.write_text(json.dumps(current, indent=2, sort_keys=True) + "\n")
    removed = sorted(set(recorded) - set(current))
    lowered = sorted(path for path in set(current) & set(recorded)
                     if current[path] < recorded[path])
    for path in removed:
        print(f"baseline removed {path} (was {recorded[path]} lines)")
    for path in lowered:
        print(f"baseline lowered {path}: {recorded[path]} -> {current[path]} lines")
    if added or raised:
        print("Growth recorded; copy this line into the PR description:")
        print(f"file-size baseline growth: {allow_growth}")
        for path in added:
            print(f"  added {path} at {current[path]} lines")
        for path in raised:
            print(f"  grew {path} from {recorded[path]} to {current[path]} lines")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true",
                        help="regenerate the baseline (shrink-only unless --allow-growth)")
    parser.add_argument("--allow-growth", metavar="REASON",
                        help="permit new or raised entries, recording this reason")
    args = parser.parse_args()
    if args.allow_growth and not args.write:
        raise SystemExit("--allow-growth only applies with --write.")
    if args.write:
        write(ROOT, args.allow_growth)
    else:
        check(ROOT)


if __name__ == "__main__":
    main()
