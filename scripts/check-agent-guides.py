#!/usr/bin/env python3
"""Keep AGENTS.md guides short enough to read and free of dead local paths.

A guide an agent must finish reading before working costs more than the
answers it holds, so root and module guides get a 120-line budget and deeper
nested guides 60. Oversized guides from before the limit carry explicit,
reasoned exceptions and may not grow.

Relative paths a guide states in backticks or links must resolve, from the
guide's own directory or the repository root; a guide that points at a file
that no longer exists sends an agent looking for it.
"""

import argparse
import os
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ROOT_LIMIT = 120
NESTED_LIMIT = 60
# Guides over today's limits, with the ceiling they may not exceed. The
# trimming lane shrinks them; empty this table once they are within limits.
LIMIT_EXCEPTIONS = {
    "AGENTS.md": (166, "being trimmed"),
    "cli/AGENTS.md": (134, "being trimmed"),
    "core/AGENTS.md": (267, "being trimmed"),
}

BACKTICK = re.compile(r"`([^`\n]+)`")
LINK = re.compile(r"\]\(([^)\s]+)\)")
# Lowercase only: `stream.Mount` is a Go symbol, not a `.Mount` file.
FILE_EXTENSION = re.compile(r"\.[a-z0-9]+$")
TRAILING_PUNCTUATION = ".,;:)"
# Fenced or tilde code blocks are quoted material, not guide references.
FENCE = re.compile(r"^\s*(```|~~~)")
# References shaped like file paths that name something else. Each carries
# its owner; delete the entry when the guide stops using the reference.
PATH_ALLOWANCES = {
    "lib/inboxResponseQueue.js": "web-ui/AGENTS.md abbreviates src/lib as lib; normalize the guide",
}


def tracked_guides():
    listing = subprocess.check_output(["git", "ls-files", "-z", "--", "**/AGENTS.md", "AGENTS.md"],
                                      cwd=ROOT, stderr=subprocess.PIPE)
    return sorted(os.fsdecode(p) for p in listing.split(b"\0") if p)


def limit_for(path):
    if path in LIMIT_EXCEPTIONS:
        return LIMIT_EXCEPTIONS[path][0]
    depth = len(Path(path).parts) - 1  # AGENTS.md at the root has depth 0
    return ROOT_LIMIT if depth <= 1 else NESTED_LIMIT


def check_length(root):
    problems = []
    for path in tracked_guides():
        guide = root / path
        if not guide.is_file():
            continue
        count = len(guide.read_text(errors="replace").splitlines())
        limit = limit_for(path)
        if count > limit:
            note = (f" (exception: {LIMIT_EXCEPTIONS[path][1]}; do not add lines)"
                    if path in LIMIT_EXCEPTIONS else "")
            problems.append(
                f"{path} is {count} lines, over its {limit}-line budget{note}: trim it, or move "
                "detail into the deeper guide or doc that owns the topic")
    return problems


def candidate_paths(path):
    """Repository file paths a guide states, without prose or placeholders."""
    lines = []
    fenced = False
    for line in path.read_text(errors="replace").splitlines():
        if FENCE.match(line):
            fenced = not fenced
            continue
        if not fenced:
            lines.append(line)
    tokens = []
    for token in BACKTICK.finditer("\n".join(lines)):
        tokens.append(token.group(1))
    for token in LINK.finditer("\n".join(lines)):
        tokens.append(token.group(1))
    for token in tokens:
        token = token.strip()
        if token in PATH_ALLOWANCES:
            continue
        # Anchors and queries are not files; strip them before testing shape.
        token = token.split("#", 1)[0].split("?", 1)[0].rstrip(TRAILING_PUNCTUATION)
        if (not token or " " in token or "://" in token or token.startswith(("mailto:", "tel:"))
                or any(mark in token for mark in "*?<>$~")
                or ".." in Path(token).parts or "/" not in token
                or not FILE_EXTENSION.search(token)):
            continue
        yield token


def check_paths(root):
    problems = []
    for path in tracked_guides():
        guide = root / path
        if not guide.is_file():
            continue
        for token in candidate_paths(guide):
            resolved = [(guide.parent / token).resolve(), (root / token).resolve()]
            if not any(candidate.exists() for candidate in resolved):
                problems.append(f"{path} references `{token}`, which does not exist "
                                "(from the guide's directory or the repository root)")
    return problems


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.parse_args()
    problems = check_length(ROOT) + check_paths(ROOT)
    if problems:
        raise SystemExit("Agent guide problems:\n" + "\n".join(problems))
    print(f"Agent guides within limits and local paths resolve "
          f"({len(tracked_guides())} guides).")


if __name__ == "__main__":
    main()
