#!/usr/bin/env python3
"""Git merge driver for generated JSON inventories keyed by name, not by line.

Inventories like core/internal/storage/testdata/resource_access_storage.json are
sorted maps regenerated from live schema and source, so two branches that add
different tables, columns or writers produce textually adjacent edits that a
line merge rejects. Merging per key resolves those automatically and still
conflicts when both sides give the same key different values.

The driver never invents entries: the committed inventory remains the reviewed
artifact, and `make -C .. access-inventory` (plus the storage inventory test)
stays the authority on what the file must contain.

Usage (configured by `make install-hooks`): json_inventory_merge.py %O %A %B %P
"""

import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


CONFLICT = object()


def merge(base, ours, theirs, path, conflicts):
    """Three-way merge two JSON values, recursing through objects by key."""
    if ours == theirs:
        return ours
    if ours == base:
        return theirs
    if theirs == base:
        return ours
    if not (isinstance(ours, dict) and isinstance(theirs, dict)):
        conflicts.append("/".join(path) or "<document>")
        return CONFLICT
    merged = {}
    reference = base if isinstance(base, dict) else {}
    for key in sorted(set(ours) | set(theirs)):
        if key not in ours or key not in theirs:
            # One side deleted what the other kept or changed: a real conflict
            # unless the other side left it exactly as the base had it.
            kept = theirs if key in theirs else ours
            if key in reference and reference[key] != kept[key]:
                conflicts.append("/".join([*path, key]))
                continue
            if key not in reference:
                merged[key] = kept[key]
            continue
        value = merge(reference.get(key), ours[key], theirs[key], [*path, key], conflicts)
        if value is not CONFLICT:
            merged[key] = value
    return merged


def normalized(value):
    """Sort object keys the way Go's encoder emits regenerated inventories."""
    if isinstance(value, dict):
        return {key: normalized(value[key]) for key in sorted(value)}
    return value


def ordered(merged, ours):
    """Keep the generator's top-level field order; sort everything below it."""
    keys = [key for key in ours if key in merged] + [k for k in merged if k not in ours]
    return {key: normalized(merged[key]) for key in keys}


def format_with_prettier(target):
    """Match the repo's committed formatting, or report that it could not."""
    root = Path(__file__).resolve().parents[2]
    try:
        # Git hands the driver an extensionless temp file, so name the parser;
        # `pnpm -C` runs prettier from web-ui, so the path has to be absolute.
        subprocess.run(["pnpm", "-C", "web-ui", "exec", "prettier", "--write",
                        "--parser=json", str(Path(target).resolve())],
                       cwd=root, check=True, capture_output=True)
        return True
    except (OSError, subprocess.CalledProcessError):
        return False


def hand_back(base_file, ours_file, theirs_file, label, reason):
    """Leave the normal line merge, markers and all, for a human to resolve.

    Returning early without touching %A would hand Git a file that still parses
    and still looks like one side's work, which a `git add` during a rebase
    would silently accept. Conflict markers keep it unresolvable until someone
    looks at it.
    """
    merged = subprocess.run(["git", "merge-file", "-p", "--diff3",
                             "-L", "ours", "-L", "base", "-L", "theirs",
                             str(ours_file), str(base_file), str(theirs_file)],
                            capture_output=True)
    if merged.stdout:
        ours_file.write_bytes(merged.stdout)
    print(f"{label}: {reason}; resolve the conflict markers by hand, or take one side and run "
          "make access-inventory.", file=sys.stderr)
    return 1


def main(argv):
    if len(argv) < 3:
        print(__doc__, file=sys.stderr)
        return 2
    base_file, ours_file, theirs_file = (Path(p) for p in argv[:3])
    label = argv[3] if len(argv) > 3 else str(ours_file)
    try:
        documents = [json.loads(p.read_bytes() or b"{}") for p in
                     (base_file, ours_file, theirs_file)]
    except ValueError as error:
        return hand_back(base_file, ours_file, theirs_file, label, f"not mergeable as JSON ({error})")
    base, ours, theirs = documents
    conflicts = []
    merged = merge(base, ours, theirs, [], conflicts)
    if merged is CONFLICT or conflicts:
        return hand_back(base_file, ours_file, theirs_file, label,
                         "both sides changed " + ", ".join(conflicts or ["the document"]))
    # Format a scratch copy first: %A must still hold our side if this fails.
    scratch = Path(tempfile.mkdtemp(prefix="anx-inventory-merge-")) / "inventory.json"
    try:
        scratch.write_text(json.dumps(ordered(merged, ours), indent=2, ensure_ascii=False) + "\n",
                           encoding="utf-8")
        if not format_with_prettier(scratch):
            # Unformatted generated output fails the static check later, during
            # a rebase nobody is watching; stop here instead.
            return hand_back(base_file, ours_file, theirs_file, label,
                             "merged per entry but prettier is unavailable to format it "
                             "(run make setup)")
        ours_file.write_bytes(scratch.read_bytes())
    finally:
        shutil.rmtree(scratch.parent, ignore_errors=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
