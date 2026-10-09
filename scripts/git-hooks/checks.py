#!/usr/bin/env python3
"""Offline static checks and merge-base-selected unit tests; never browser tests."""

import argparse
import json
import os
from pathlib import Path
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[2]
MODULES = ("core", "cli", "mcp", "web-ui")
GO_MODULES = MODULES[:-1]
SHARED = {"Makefile", ".pre-commit-config.yaml", "package.json",
          "pnpm-lock.yaml", "pnpm-workspace.yaml", ".github/workflows/ci.yml"}
# Module-relative paths that defeat package-level selection inside a Go module.
GO_MODULE_WIDE = {"go.mod", "go.sum", "go.work", "go.work.sum", "Makefile"}
GO_MODULE_WIDE_PREFIXES = ("scripts/", "dev/")
# Documentation suffixes, only ever skipped when no package encloses the file.
INERT_SUFFIXES = {".md", ".txt"}
WIDEN = object()
# web-ui roots the Vitest module graph cannot attribute to individual tests.
UI_WIDE = {"web-ui/package.json", "web-ui/vite.config.js", "web-ui/vitest.config.js",
           "web-ui/svelte.config.js", "web-ui/postcss.config.cjs", "web-ui/tailwind.config.cjs"}
UI_WIDE_PREFIXES = ("web-ui/tests/mocks/", "web-ui/src/lib/generated/")
ROUTE_INVENTORY = "contracts/gen/meta/routes.json"
# The release-managed version files, and the detector CI uses to confirm a bump.
VERSION_MANAGED_LIST = "scripts/version-managed-files.sh"
RELEASE_BASE_DETECTOR = "scripts/ci-release-base.py"


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, stderr=subprocess.PIPE)


def paths(data):
    return {os.fsdecode(p) for p in data.split(b"\0") if p}


def merge_base(base=None):
    """Merge base with the integration branch, or None when it cannot be resolved."""
    try:
        return git("merge-base", base or "origin/main", "HEAD").decode().strip()
    except subprocess.CalledProcessError:
        if base:
            raise
        return None


def changed_paths(tier, all_files=False, base=None):
    if all_files:
        return paths(git("ls-files", "-z"))
    if tier == "static":
        return paths(git("diff", "--cached", "--name-only", "-z", "--no-renames"))
    found = merge_base(base)
    if found is None:
        print("No origin/main merge base; checking every module (no fetch).", flush=True)
        return paths(git("ls-files", "-z")) | SHARED
    # Include commits AND staged/unstaged/new work for standalone invocations.
    return (paths(git("diff", "--name-only", "-z", "--no-renames", found, "HEAD"))
            | paths(git("diff", "--cached", "--name-only", "-z", "--no-renames"))
            | paths(git("diff", "--name-only", "-z", "--no-renames"))
            | paths(git("ls-files", "--others", "--exclude-standard", "-z")))


def version_managed_files():
    """The release-managed version files, read from the list the CI gate shares."""
    listing = subprocess.check_output(["bash", str(ROOT / VERSION_MANAGED_LIST)],
                                     cwd=ROOT, stderr=subprocess.PIPE)
    return {name for name in listing.decode().splitlines() if name}


def release_bump_only(changed, base):
    """True when everything between `base` and HEAD is an exact generated version bump.

    A release bump rewrites `core/internal/buildinfo/version_generated.go`, which
    nearly every core package imports, so normal selection runs most of the repo
    for a commit the release flow already gated on the source commit's green CI.

    Filenames cannot decide this on their own: `web-ui/package.json` and the Go
    version files can carry executable changes too. So the file list only rules
    the question out cheaply, and a candidate is then confirmed the way the CI
    short-circuit confirms it -- `scripts/ci-release-base.py` regenerates each
    commit from its parent's tooling and compares -- walking back to `base`.
    Anything unverifiable falls through to normal selection.
    """
    if base is None or not changed:
        return False
    try:
        # A fast path, not a guard: the walk below already implies this.
        if changed - version_managed_files():
            return False
        # The walk says nothing about uncommitted edits to the files it clears.
        if git("status", "--porcelain", "--untracked-files=all"):
            return False
        head = git("rev-parse", "HEAD").decode().strip()
        if head == base:
            # Nothing committed to verify, so nothing the walk below could clear.
            return False
        while head != base:
            parent = subprocess.check_output(
                [sys.executable, "-B", str(ROOT / RELEASE_BASE_DETECTOR), head],
                cwd=ROOT, stderr=subprocess.PIPE).decode().strip()
            if not parent:
                return False
            head = parent
    except (subprocess.CalledProcessError, OSError, UnicodeError):
        return False
    return True


def fans_out(changed):
    """True when a change crosses module boundaries and every module must run whole."""
    return bool(changed & SHARED) or any(
        p.startswith(("contracts/", "scripts/git-hooks/", "scripts/contract-",
                      "core/cmd/contract-gen/", "core/cmd/route-inventory/")) for p in changed)


def selected_modules(changed):
    if fans_out(changed):
        return set(MODULES)
    modules = {m for m in MODULES if any(p.startswith(m + "/") for p in changed)}
    if "VERSION" in changed or any(p.startswith(("scripts/read-version", "scripts/sync-version",
                                                "scripts/set-version")) for p in changed):
        modules.add("cli")
    return modules


def check_env(extra_env=None):
    # Setup owns downloads. Hooks must fail with missing dependencies, not fetch them.
    env = {**os.environ, "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local",
           **(extra_env or {})}
    env["GOFLAGS"] = (env.get("GOFLAGS", "") + " -mod=readonly").strip()
    return env


def run(*argv, cwd=ROOT, extra_env=None):
    print("+ " + " ".join(str(arg) for arg in argv), flush=True)
    subprocess.run([str(arg) for arg in argv], cwd=cwd, env=check_env(extra_env), check=True)


def start(argv, cwd=ROOT):
    """Start a command in its own group, with output held in a file.

    A file rather than a pipe: a pipe's buffer fills at a few tens of kilobytes
    and would stall the command until someone reads it.
    """
    sink = tempfile.TemporaryFile()
    process = subprocess.Popen([str(arg) for arg in argv], cwd=cwd, env=check_env(),
                               stdout=sink, stderr=subprocess.STDOUT, start_new_session=True)
    process.output = sink
    return process


def finish(process):
    """Wait for a started command and return everything it printed."""
    process.wait()
    process.output.seek(0)
    output = process.output.read()
    process.output.close()
    return output


def end(process):
    """Stop a started command and its children, ignoring one that already exited."""
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except (ProcessLookupError, PermissionError):
        pass
    finish(process)


def go_package_graph(module):
    """Module-relative dirs, direct+test imports, and testability per package."""
    template = ('{{.ImportPath}}\t{{.Dir}}\t'
                '{{len .GoFiles}} {{len .TestGoFiles}} {{len .XTestGoFiles}}\t'
                '{{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}')
    listing = subprocess.check_output(["go", "list", "-e", "-f", template, "./..."],
                                      cwd=ROOT / module, env=check_env()).decode()
    directory, imports, buildable = {}, {}, set()
    # go reports resolved directories; resolve ours too so a symlinked checkout
    # still maps packages to repository-relative paths.
    module_root = (ROOT / module).resolve()
    for line in listing.splitlines():
        path, package_dir, counts, deps = line.split("\t")
        if not package_dir:
            continue
        directory[os.path.relpath(Path(package_dir).resolve(), module_root)] = path
        imports[path] = {dep for dep in deps.split() if dep}
        if any(int(count) for count in counts.split()):
            buildable.add(path)
    return directory, imports, buildable


def owning_package(rel, directory):
    """The package that owns a module-relative path.

    Returns its import path, None for documentation that no package encloses, or
    WIDEN when nothing in the package tree can own the file. Suffixes decide
    nothing inside a package directory: embedded assets and golden files are
    build inputs whatever they are called.
    """
    owner = Path(rel).parent
    while True:
        if str(owner) in directory:
            return directory[str(owner)]
        if str(owner) == ".":
            return None if Path(rel).suffix in INERT_SUFFIXES else WIDEN
        owner = owner.parent


def go_affected(module, changed):
    """Changed packages and their dependents as ./dir args, or None to run the module whole.

    Dependents follow test imports as well as imports, so a change in a helper
    package still runs every package whose tests consume it.
    """
    owned = sorted(p[len(module) + 1:] for p in changed if p.startswith(module + "/"))
    if not owned:
        return None  # Selected for a reason outside the module (e.g. VERSION for cli).
    directory, imports, buildable = go_package_graph(module)
    selected = set()
    for rel in owned:
        if rel in GO_MODULE_WIDE or rel.startswith(GO_MODULE_WIDE_PREFIXES):
            return None
        package = owning_package(rel, directory)
        if package is WIDEN:
            # Build-tagged, generated or non-Go trees have no listed package.
            return None
        if package is not None:
            selected.add(package)
    importers = {}
    for path, deps in imports.items():
        for dep in deps:
            importers.setdefault(dep, set()).add(path)
    dependents, frontier = set(), set(selected)
    while frontier:
        reached = {importer for path in frontier for importer in importers.get(path, ())}
        frontier = reached - selected - dependents
        dependents |= frontier
    rel_of = {path: rel for rel, path in directory.items()}
    def args(packages):
        return sorted("./" + rel_of[p] for p in packages & buildable)
    if selected and not args(selected):
        return None  # Nothing testable owns the change; do not silently skip it.
    return args(selected), args(dependents)


def contract_drift(staged=False):
    """Generate in scratch space and compare content AND file sets without mutations."""
    with tempfile.TemporaryDirectory(prefix="anx-contract-check-") as scratch:
        out = Path(scratch) / "contracts/gen"
        run("go", "run", "./cmd/contract-gen", "--out", out,
            "--source-label", "contracts/anx-openapi.yaml",
            "--go-module", "agent-nexus-contracts-go-client",
            "--ts-package", "agent-nexus-contracts-ts-client", cwd=ROOT / "core")
        run("go", "run", "./cmd/route-inventory", "--out", out / "meta/routes.json",
            cwd=ROOT / "core")
        run("pnpm", "exec", "tsc", "--project", out / "ts/tsconfig.json", "--pretty", "false")
        for source, dest, pattern in (("meta", "cli/internal/registry", "*.json"),
                                      ("docs", "cli/docs/generated", "*.md")):
            target = Path(scratch) / dest
            target.mkdir(parents=True)
            for file in (out / source).glob(pattern):
                if source == "meta" and file.name == "routes.json":
                    continue
                shutil.copyfile(file, target / file.name)
        target = Path(scratch) / "web-ui/src/lib/generated"
        target.mkdir(parents=True)
        for name in ("event_ref_rules.json", "taxonomy.json"):
            shutil.copyfile(out / "meta" / name, target / name)

        def managed(path):
            return (path.startswith("contracts/gen/")
                    or (path.startswith("cli/internal/registry/") and path.endswith(".json"))
                    or (path.startswith("web-ui/src/lib/generated/") and path.endswith(".json"))
                    or (path.startswith("cli/docs/generated/") and path.endswith(".md")
                        and path != "cli/docs/generated/runtime-help.md"))

        expected = {str(p.relative_to(scratch)): p for p in Path(scratch).rglob("*") if p.is_file()}
        actual = {p for p in paths(git("ls-files", "-z")) if managed(p)}
        if not staged:
            actual |= {p for p in paths(git("ls-files", "--others", "--exclude-standard", "-z")) if managed(p)}
        stale = set(expected) ^ actual
        for path in set(expected) & actual:
            content = git("show", ":" + path) if staged else (ROOT / path).read_bytes() if (ROOT / path).is_file() else None
            if content != expected[path].read_bytes():
                stale.add(path)
        if stale:
            raise RuntimeError("Generated contract drift; run make contract-gen and stage outputs:\n"
                               + "\n".join(sorted(stale)))
        print("Generated contracts and mirrors match.", flush=True)


def static_checks(changed, all_files=False):
    if not all_files and paths(git("diff", "--name-only", "-z")):
        raise RuntimeError("Static checks require a staged snapshot. Stage or revert the "
                           "unstaged tracked edits, or have the hook framework stash them as a "
                           "commit would: make setup, then .venv/bin/pre-commit run "
                           "--hook-stage pre-commit.")
    if not all_files:
        untracked = paths(git("ls-files", "--others", "--exclude-standard", "-z"))
        # Git-ignore rules do not hide Go sources from the compiler. Hidden,
        # underscore, vendor and testdata directories are excluded by go ./....
        go_untracked = paths(git("ls-files", "--others", "-z", "--", "core", "cli", "mcp", "contracts"))
        untracked |= {p for p in go_untracked if
                      (Path(p).suffix == ".go" or Path(p).name in {"go.mod", "go.sum"}
                       or p in {"contracts/anx-openapi.yaml", "contracts/anx-schema.yaml",
                                "contracts/non-openapi-endpoints.yaml"})
                      and not any(part.startswith((".", "_")) or part in {"vendor", "testdata"}
                                  for part in Path(p).parts)}
        inputs = sorted(p for p in untracked if
                        (p.startswith((*[m + "/" for m in MODULES], "contracts/", "scripts/git-hooks/"))
                         and Path(p).suffix in {".go", ".mod", ".sum", ".js", ".mjs", ".cjs",
                                               ".ts", ".svelte", ".css", ".json", ".yaml", ".yml", ".py"})
                        or p in {"go.work", "go.work.sum", "package.json", "pnpm-lock.yaml",
                                 "pnpm-workspace.yaml"})
        if inputs:
            raise RuntimeError("Untracked build inputs can mask staged errors; stage or move them:\n"
                               + "\n".join(inputs))
    existing = sorted(p for p in changed if (ROOT / p).is_file())
    go_files = [p for p in existing if p.endswith(".go")]
    if go_files:
        unformatted = subprocess.check_output(["gofmt", "-l", *go_files], cwd=ROOT).decode()
        if unformatted:
            raise RuntimeError("gofmt required:\n" + unformatted)
    format_files = [p for p in existing if Path(p).suffix in
                    {".md", ".yaml", ".yml", ".json", ".js", ".mjs", ".cjs", ".css", ".svelte", ".ts"}
                    and not p.startswith(("contracts/", "cli/internal/registry/",
                                          "cli/docs/generated/", "web-ui/src/lib/generated/"))
                and p != "mcp/docs/tool-coverage.md"]  # Verified byte-for-byte by check-tool-policy below.
    if format_files:
        run("pnpm", "-C", "web-ui", "exec", "prettier", "--check",
            *["../" + p for p in format_files])
    # Hook/build configuration changes test all modules at push, but do not
    # require linting untouched source at commit. Contract/dependency changes do.
    static_changed = {p for p in changed if p not in
                      {"Makefile", ".pre-commit-config.yaml", ".github/workflows/ci.yml"}
                      and not p.startswith("scripts/git-hooks/")}
    modules = selected_modules(static_changed)
    for module in MODULES[:-1]:
        if module in modules:
            run("go", "vet", "./...", cwd=ROOT / module)
    if "web-ui" in modules:
        ui_files = [p[len("web-ui/"):] for p in existing if p.startswith("web-ui/")
                    and Path(p).suffix in {".js", ".mjs", ".cjs", ".svelte"}]
        run("pnpm", "-C", "web-ui", "exec", "eslint", *(ui_files or ["."]))
        for script in ("check-svelte5-runes.js", "check-core-client-command-ids.mjs",
                       "check-floating-layers.mjs"):
            run("node", "scripts/" + script, cwd=ROOT / "web-ui")
    if "mcp" in modules:
        run("node", "mcp/scripts/check-tool-policy.mjs")
    for file in existing:
        if file.endswith(".sh") or file in {"scripts/git-hooks/pre-commit", "scripts/git-hooks/pre-push"}:
            run("bash", "-n", file)
        if file.endswith(".py"):
            compile((ROOT / file).read_bytes(), file, "exec")
    run("./scripts/sync-version.sh", "--check")
    if (ROOT / "scripts/check-oss-boundary.sh").is_file():
        run("bash", "scripts/check-oss-boundary.sh")
    run("node", "scripts/check-commercial-boundary.mjs", cwd=ROOT / "web-ui")
    if any(p.startswith(".github/workflows/") for p in changed):
        if not (ROOT / ".bin/actionlint").is_file():
            raise RuntimeError("Missing .bin/actionlint; run make setup before committing workflows.")
        run(ROOT / ".bin/actionlint")
    contract_drift(staged=not all_files)
    route_notice()


def pinned_package_parallelism():
    """An explicit `go test -p` cap from GOFLAGS, if the caller set one.

    CI pins `-p=1` for core while its fixture concurrency is unconfirmed. Two
    concurrent `go test` processes would quietly double whatever cap the caller
    asked for, so a pinned `-p` makes the phases run one after the other.
    """
    flags = shlex.split(os.environ.get("GOFLAGS", ""))
    for index, flag in enumerate(flags):
        if flag.startswith("-p="):
            return flag[len("-p="):]
        if flag == "-p" and index + 1 < len(flags):
            return flags[index + 1]
    return None


def go_fast_tests(module, changed, whole_module):
    """Report the changed packages first, then their dependents.

    The dependents run alongside the changed packages but their output is held
    back, so a break in what you edited is reported (and aborts the rest) at the
    cost of the changed packages alone, while a green run still finishes in the
    wall clock of one parallel `go test`. A pinned `-p` serializes them instead.
    """
    affected = None if whole_module else go_affected(module, changed)
    if affected is None:
        print(f"{module}: every package (shared root or unattributable change).", flush=True)
        run("go", "test", "-short", "./...", cwd=ROOT / module)
        return
    selected, dependents = affected
    if not selected and not dependents:
        print(f"{module}: no package affected (docs or non-Go files only).", flush=True)
        return
    print(f"{module}: {len(selected)} changed package(s) plus {len(dependents)} dependent(s).",
          flush=True)
    if not selected:
        run("go", "test", "-short", *dependents, cwd=ROOT / module)
        return
    if not dependents:
        run("go", "test", "-short", *selected, cwd=ROOT / module)
        return
    pinned = pinned_package_parallelism()
    if pinned is not None:
        print(f"{module}: GOFLAGS pins -p={pinned}; running the dependents after the changed "
              "packages rather than beside them.", flush=True)
        run("go", "test", "-short", *selected, cwd=ROOT / module)
        run("go", "test", "-short", *dependents, cwd=ROOT / module)
        return
    argv = ("go", "test", "-short", *dependents)
    print("+ " + " ".join(argv) + "  (dependents, reported after the changed packages)", flush=True)
    later = start(argv, cwd=ROOT / module)
    try:
        run("go", "test", "-short", *selected, cwd=ROOT / module)
        output = finish(later)
    except BaseException:
        end(later)
        raise
    print(output.decode(errors="replace"), end="", flush=True)
    if later.returncode:
        raise subprocess.CalledProcessError(later.returncode, argv)


def ui_source_scanning_tests():
    """Unit tests that read the tree from disk instead of importing it.

    No module graph relates these to a change, so a related-tests run would
    skip exactly the repo-wide guards (single time formatter, CSS invariants,
    generated-registry and CLI conformance). They cost seconds; always run them.
    """
    found = []
    for root in (ROOT / "web-ui/tests/unit", ROOT / "web-ui/src"):
        for path in sorted(root.rglob("*.test.js")):
            text = path.read_bytes()
            if b"node:fs" in text or b"node:child_process" in text:
                found.append(str(path.relative_to(ROOT / "web-ui")))
    return found


def ui_fast_tests(changed, whole_module, base):
    # Reuse the CI unit runner's single-fork configuration on shared hosts.
    env = {"ANX_TEST_FAST": "1", "CI": "1"}
    owned = [p for p in changed if p.startswith("web-ui/")]
    if (whole_module or base is None or not owned
            or any(p in UI_WIDE or p.startswith(UI_WIDE_PREFIXES) for p in owned)):
        print("web-ui: every unit test (shared root or unattributable change).", flush=True)
        run("pnpm", "-C", "web-ui", "run", "test:unit", extra_env=env)
        return
    # Vitest resolves the module graph itself, and reads the same committed,
    # staged, unstaged and untracked changes this selector does.
    print("web-ui: unit tests related to the changed files, plus the source-scanning guards.",
          flush=True)
    run("pnpm", "-C", "web-ui", "exec", "vitest", "run", "--changed", base,
        "--passWithNoTests", extra_env=env)
    scanning = ui_source_scanning_tests()
    if scanning:
        run("pnpm", "-C", "web-ui", "exec", "vitest", "run", *scanning, extra_env=env)


def fast_tests(changed, base=None):
    if release_bump_only(changed, base):
        print("release version bump only; source commit verified by CI", flush=True)
        return
    modules = selected_modules(changed)
    whole_module = fans_out(changed)
    print("Fast modules: " + (", ".join(m for m in MODULES if m in modules) or "none"), flush=True)
    # Verify our selector and drift failure paths whenever hook/build plumbing changes.
    if changed & SHARED or any(p.startswith("scripts/git-hooks/") for p in changed):
        run("python3", "-B", "-m", "unittest", "discover", "-s", "scripts/git-hooks", "-p", "test_*.py")
    for module in GO_MODULES:
        if module in modules:
            go_fast_tests(module, changed, whole_module)
    if "web-ui" in modules:
        ui_fast_tests(changed, whole_module, base)
    if any(p in {"scripts/owned_process.py", "scripts/e2e-smoke",
                 "scripts/tests/test_owned_process.py",
                 "scripts/tests/test_e2e_smoke_lifecycle.sh"} or p.startswith("scripts/git-hooks/")
           for p in changed):
        run("python3", "-B", "-m", "unittest", "scripts/tests/test_owned_process.py")
        run("bash", "scripts/tests/test_e2e_smoke_lifecycle.sh")
    if any(p.startswith(("contracts/", "scripts/contract-", "core/cmd/contract-gen/",
                         "core/cmd/route-inventory/")) for p in changed):
        contract_drift()
        run("go", "test", "-short", "./...", cwd=ROOT / "contracts/gen/go")
        run("go", "test", "-short", "./...", cwd=ROOT / "contracts/visualreport")
        run("node", "scripts/check-visual-report-conformance.mjs")


def route_classes(document):
    return {(route.get("method"), route.get("path")): route.get("access_class", "")
            for route in document.get("routes", [])}


def route_changes(base=None):
    """Report HTTP route additions, removals and reclassifications against the base.

    Advisory: route changes are legitimate, but every deployment has to classify
    them, so they must be visible before review rather than after. Every failure
    mode here degrades to a printed note; this must never block a commit.
    """
    reference = merge_base(base) or base or "origin/main"
    path = ROOT / ROUTE_INVENTORY
    try:
        before = route_classes(json.loads(git("show", f"{reference}:{ROUTE_INVENTORY}")))
        after = route_classes(json.loads(path.read_bytes()))
    except (subprocess.CalledProcessError, OSError, ValueError, TypeError,
            AttributeError) as error:
        print(f"Could not compare {ROUTE_INVENTORY} with {reference} ({error}); "
              "list any route changes in the PR description yourself.", flush=True)
        return
    notices = []
    for route in sorted(set(after) - set(before)):
        notices.append(f"  new route {route[0]} {route[1]} (access class {after[route]})")
    for route in sorted(set(before) - set(after)):
        notices.append(f"  removed route {route[0]} {route[1]}")
    for route in sorted(set(before) & set(after)):
        if before[route] != after[route]:
            notices.append(f"  reclassified {route[0]} {route[1]}: "
                           f"{before[route]} -> {after[route]}")
    if not notices:
        print("HTTP routes unchanged.", flush=True)
        return
    print("HTTP routes changed; list them in the PR description so deployments can "
          "classify them:", flush=True)
    print("\n".join(notices), flush=True)


def route_notice(base=None):
    """route_changes is advisory: never let it fail a commit."""
    try:
        route_changes(base)
    except Exception as error:  # noqa: BLE001 - advisory output, never a gate
        print(f"Skipped the route comparison ({error}).", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tier", choices=("static", "fast", "routes"))
    parser.add_argument("--all", action="store_true", help="check all tracked files/modules")
    parser.add_argument("--base", default=os.environ.get("TEST_FAST_BASE"), help="fast suite merge-base ref (default origin/main)")
    args = parser.parse_args()
    if args.tier == "routes":
        route_notice(args.base)
        return
    changed = changed_paths(args.tier, args.all, args.base)
    if args.tier == "static":
        static_checks(changed, args.all)
    else:
        fast_tests(changed, merge_base(args.base))


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error))
