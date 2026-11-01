#!/usr/bin/env python3
"""Generate docs/map.md, the where-things-live index of the repository.

The output is derived entirely from tracked files (never `go list`), so it is
fast, offline and deterministic: same tree, same bytes. Never hand-edit it;
regenerate with `make map`, or compare without writing with `--check`.

Large-file detection shares the ratchet's logic (scripts/check-file-sizes.py),
so the map's "split before extending" list and the enforced baseline are the
same set.
"""

import argparse
import importlib.util
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "docs/map.md"

_spec = importlib.util.spec_from_file_location(
    "check_file_sizes", ROOT / "scripts/check-file-sizes.py")
check_file_sizes = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(check_file_sizes)

# Sentence end is punctuation followed by whitespace or end of text, so version
# numbers ("v0.13.51") and dotted identifiers do not cut a sentence short.
SENTENCE_END = re.compile(r"[.!?:](?=\s|$)")
# //go:build and friends: machine directives attached to the clause, not prose.
DIRECTIVE = re.compile(r"^//[a-z+]")


def first_sentence(text):
    text = " ".join(text.split())
    match = SENTENCE_END.search(text)
    if match:
        text = text[:match.start() + 1].rstrip(":")
    return text or "(undocumented)"


def description_text(path):
    """The first paragraph after the title of a README or AGENTS guide."""
    lines = path.read_text(errors="replace").splitlines()
    seen_title = False
    paragraph = []
    for line in lines:
        if not line.strip():
            if paragraph:
                break
            continue
        if line.lstrip().startswith("#"):
            if paragraph:
                break
            seen_title = True
            continue
        if seen_title or paragraph:
            paragraph.append(line.strip())
    return first_sentence(" ".join(paragraph)) if paragraph else None


def module_description(root, directory):
    """One-line description from the nearest README/AGENTS first paragraph."""
    for name in ("README.md", "AGENTS.md"):
        candidate = root / directory / name
        if candidate.is_file():
            text = description_text(candidate)
            if text:
                return text
    return "(undocumented)"


def area_description(root, directory, ceiling):
    """Like module_description, but only walks up as far as `ceiling`.

    Above the ceiling every area would inherit the module README's one-liner,
    which says nothing about the individual area.
    """
    area = Path(directory)
    while True:
        for name in ("README.md", "AGENTS.md"):
            candidate = root / area / name
            if candidate.is_file():
                text = description_text(candidate)
                if text:
                    return text
        if area == Path(ceiling) or area.parent == area:
            return "(undocumented)"
        area = area.parent


def package_doc(files):
    """First sentence of the package doc comment, or None.

    The doc comment is the //-block directly above the package clause, with
    build directives (//go:build) dropped because they are not prose.
    """
    for name in sorted(files):
        lines = []
        with (files[name]).open("rb") as handle:
            for _ in range(100):
                line = handle.readline()
                if not line:
                    break
                lines.append(line.decode(errors="replace").rstrip("\n"))
        for index, line in enumerate(lines):
            if not line.startswith("package "):
                continue
            block = []
            above = index - 1
            while above >= 0 and lines[above].startswith("//"):
                if not DIRECTIVE.match(lines[above]):
                    block.append(lines[above].lstrip("/ ").strip())
                above -= 1
            text = " ".join(reversed(block)).strip()
            # The doc comment may sit in any of the package's files; keep
            # looking until one carries prose.
            if text:
                return text
            break
    return None


def go_packages(root, tracked):
    """[(path, package name, doc, non-test file count)] for every package."""
    sources = [p for p in tracked
               if p.endswith(".go") and not p.endswith("_test.go")
               and not p.startswith(check_file_sizes.GENERATED_PREFIXES)
               and "testdata" not in Path(p).parts]
    by_dir = {}
    for path in sources:
        by_dir.setdefault(str(Path(path).parent), []).append(path)
    packages = []
    for directory in sorted(by_dir):
        files = by_dir[directory]
        package_name = ""
        for path in files:
            with (root / path).open("rb") as handle:
                for _ in range(20):
                    line = handle.readline().decode(errors="replace")
                    if line.startswith("package "):
                        package_name = line.split()[1]
                        break
                if package_name:
                    break
        doc = package_doc({name: root / name for name in files})
        packages.append((directory, package_name,
                         first_sentence(doc) if doc else "(undocumented)", len(files)))
    return packages


def web_ui_areas(root, tracked):
    """web-ui/src/lib subtrees holding source, and route page areas."""
    lib = "web-ui/src/lib"
    areas = {lib} | {
        str(Path(p).parent) for p in tracked
        if p.startswith(lib + "/") and check_file_sizes.is_handwritten(p)}
    areas |= {
        str(Path(p).parent) for p in tracked
        if p.startswith("web-ui/src/routes/")
        and Path(p).name in {"+page.svelte", "+layout.svelte"}}
    described = [(area, area_description(root, area, "web-ui/src")) for area in sorted(areas)]
    return described


def table(headers, rows):
    def cell(text):
        return str(text).replace("|", "\\|")

    widths = [len(str(h)) for h in headers]
    for row in rows:
        for index, value in enumerate(row):
            widths[index] = max(widths[index], len(str(value)))
    # Prettier-stable shape: padded cells and spaced separator dashes, so the
    # committed output never needs a formatting pass.
    lines = ["| " + " | ".join(str(h).ljust(widths[i]) for i, h in enumerate(headers)) + " |",
             "| " + " | ".join("-" * width for width in widths) + " |"]
    for row in rows:
        lines.append("| " + " | ".join(cell(v).ljust(widths[i]) for i, v in enumerate(row)) + " |")
    return "\n".join(lines) + "\n"


def render(root):
    tracked = check_file_sizes.tracked_files(root)
    modules = sorted({Path(p).parts[0] for p in tracked if "/" in p})
    packages = go_packages(root, tracked)
    undocumented = sum(1 for _p, _n, doc, _f in packages if doc == "(undocumented)")
    large = sorted(check_file_sizes.oversized(root).items(), key=lambda item: (-item[1], item[0]))
    parts = [
        "<!-- Generated by scripts/gen-map.py from the tracked tree; do not edit. -->\n\n"
        "Regenerate with `make map`.\n\n"
        "# Repository map\n\n"
        "Where things live. Generated; never hand-edited.\n\n"
        "## Modules\n\n",
        table(["Path", "Description"],
              [(m, module_description(root, m)) for m in modules]) + "\n",
        "## Go packages\n\n",
        table(["Package", "Files", "Doc"],
              [(p, files, doc) for p, _name, doc, files in packages]) + "\n"
        f"{undocumented} of {len(packages)} packages are undocumented.\n\n",
        "## web-ui areas\n\n",
        table(["Area", "Description"], web_ui_areas(root, tracked)) + "\n",
        "## large files, split before extending\n\n"
        f"Hand-written files over the {check_file_sizes.LIMIT}-line ratchet "
        "(scripts/check-file-sizes.py keeps this list from growing).\n\n",
        table(["Lines", "File"], [(count, path) for path, count in large]),
    ]
    return "".join(parts)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true",
                        help="compare against docs/map.md without writing")
    args = parser.parse_args()
    rendered = render(ROOT)
    if args.check:
        current = OUTPUT.read_text() if OUTPUT.is_file() else None
        if current != rendered:
            raise SystemExit("docs/map.md is stale; run make map and commit it with the change.")
        print("docs/map.md up to date.")
        return
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(rendered)
    print(f"Wrote {OUTPUT}.")


if __name__ == "__main__":
    main()
