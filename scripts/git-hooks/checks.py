#!/usr/bin/env python3
"""Offline static checks and merge-base-selected unit tests; never browser tests."""

import argparse
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parents[2]
MODULES = ("core", "cli", "mcp", "web-ui")
SHARED = {"Makefile", ".pre-commit-config.yaml", "package.json",
          "pnpm-lock.yaml", "pnpm-workspace.yaml", ".github/workflows/ci.yml"}


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, stderr=subprocess.PIPE)


def paths(data):
    return {os.fsdecode(p) for p in data.split(b"\0") if p}


def changed_paths(tier, all_files=False, base=None):
    if all_files:
        return paths(git("ls-files", "-z"))
    if tier == "static":
        return paths(git("diff", "--cached", "--name-only", "-z", "--no-renames"))
    try:
        merge_base = git("merge-base", base or "origin/main", "HEAD").decode().strip()
    except subprocess.CalledProcessError:
        if base:
            raise
        print("No origin/main merge base; checking every module (no fetch).", flush=True)
        return paths(git("ls-files", "-z")) | SHARED
    # Include commits AND staged/unstaged/new work for standalone invocations.
    return (paths(git("diff", "--name-only", "-z", "--no-renames", merge_base, "HEAD"))
            | paths(git("diff", "--cached", "--name-only", "-z", "--no-renames"))
            | paths(git("diff", "--name-only", "-z", "--no-renames"))
            | paths(git("ls-files", "--others", "--exclude-standard", "-z")))


def selected_modules(changed):
    if changed & SHARED or any(p.startswith(("contracts/", "scripts/git-hooks/",
                                             "scripts/contract-", "core/cmd/contract-gen/",
                                             "core/cmd/route-inventory/")) for p in changed):
        return set(MODULES)
    modules = {m for m in MODULES if any(p.startswith(m + "/") for p in changed)}
    if "VERSION" in changed or any(p.startswith(("scripts/read-version", "scripts/sync-version",
                                                "scripts/set-version")) for p in changed):
        modules.add("cli")
    return modules


def run(*argv, cwd=ROOT, extra_env=None):
    print("+ " + " ".join(str(arg) for arg in argv), flush=True)
    # Setup owns downloads. Hooks must fail with missing dependencies, not fetch them.
    env = {**os.environ, "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local",
           **(extra_env or {})}
    subprocess.run([str(arg) for arg in argv], cwd=cwd, env=env, check=True)


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
        raise RuntimeError("Static checks require a staged snapshot. Run .venv/bin/pre-commit run "
                           "--hook-stage pre-commit (stashes unstaged tracked edits), "
                           "or stage/revert those edits first.")
    if not all_files:
        untracked = paths(git("ls-files", "--others", "--exclude-standard", "-z"))
        # Git-ignore rules do not hide Go sources from the compiler. Hidden,
        # underscore, vendor and testdata directories are excluded by go ./....
        go_untracked = paths(git("ls-files", "--others", "-z", "--", "core", "cli", "mcp", "contracts"))
        untracked |= {p for p in go_untracked if Path(p).suffix == ".go"
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
                                          "cli/docs/generated/", "web-ui/src/lib/generated/"))]
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


def fast_tests(changed):
    modules = selected_modules(changed)
    print("Fast modules: " + (", ".join(m for m in MODULES if m in modules) or "none"), flush=True)
    # Verify our selector and drift failure paths whenever hook/build plumbing changes.
    if changed & SHARED or any(p.startswith("scripts/git-hooks/") for p in changed):
        run("python3", "-B", "-m", "unittest", "discover", "-s", "scripts/git-hooks", "-p", "test_*.py")
    for module in MODULES[:-1]:
        if module in modules:
            run("go", "test", "-short", "./...", cwd=ROOT / module)
    if "web-ui" in modules:
        # Reuse the CI unit runner's single-fork configuration on shared hosts.
        run("pnpm", "-C", "web-ui", "run", "test:unit",
            extra_env={"ANX_TEST_FAST": "1", "CI": "1"})
    if any(p.startswith(("contracts/", "scripts/contract-", "core/cmd/contract-gen/",
                         "core/cmd/route-inventory/")) for p in changed):
        contract_drift()
        run("go", "test", "-short", "./...", cwd=ROOT / "contracts/gen/go")
        run("go", "test", "-short", "./...", cwd=ROOT / "contracts/visualreport")
        run("node", "scripts/check-visual-report-conformance.mjs")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tier", choices=("static", "fast"))
    parser.add_argument("--all", action="store_true", help="check all tracked files/modules")
    parser.add_argument("--base", default=os.environ.get("TEST_FAST_BASE"), help="fast suite merge-base ref (default origin/main)")
    args = parser.parse_args()
    changed = changed_paths(args.tier, args.all, args.base)
    if args.tier == "static":
        static_checks(changed, args.all)
    else:
        fast_tests(changed)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error))
