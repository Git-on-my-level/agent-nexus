#!/usr/bin/env python3
"""Exercise the boundary guard using synthetic, temporary Git repositories."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile


GUARD = Path(__file__).with_name("check-oss-boundary.sh")


def check_fixture(content, *, extra=None, file_extra=None, expected=0,
                  mutate_guard=False, git=True, binary="", missing_file=False):
    with tempfile.TemporaryDirectory(prefix="oss-boundary-test-") as directory:
        root = Path(directory) / "repo"
        scripts = root / "scripts"
        scripts.mkdir(parents=True)
        script = scripts / GUARD.name
        shutil.copyfile(GUARD, script)
        (root / "fixture.txt").write_text(content, encoding="utf-8")
        # A binary containing the same bytes must not trigger the text guard.
        (root / "binary.dat").write_bytes(b"\x00" + binary.encode())
        if mutate_guard:
            with script.open("a", encoding="utf-8") as handle:
                handle.write("\n# " + content)
            (root / "fixture.txt").write_text("neutral\n", encoding="utf-8")
        if git:
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            subprocess.run(["git", "-C", str(root), "add", "."], check=True)
        env = os.environ.copy()
        env.pop("OSS_BOUNDARY_EXTRA_PATTERNS", None)
        env.pop("OSS_BOUNDARY_EXTRA_FILE", None)
        if extra is not None:
            env["OSS_BOUNDARY_EXTRA_PATTERNS"] = extra
        if file_extra is not None:
            patterns = Path(directory) / "extra-patterns.txt"
            patterns.write_text(file_extra, encoding="utf-8")
            env["OSS_BOUNDARY_EXTRA_FILE"] = str(patterns)
        if missing_file:
            env["OSS_BOUNDARY_EXTRA_FILE"] = str(Path(directory) / "absent.txt")
        result = subprocess.run(
            ["bash", str(script)], env=env, capture_output=True, text=True, timeout=30
        )
        assert result.returncode == expected, (
            f"Expected exit {expected}, got {result.returncode}: {result.stdout}{result.stderr}"
        )
        if expected == 1:
            assert "fixture.txt:1:" in result.stdout or "scripts/" in result.stdout
        return result.stdout + result.stderr


def main():
    # Concatenate synthetic path parts so this test source passes the guard too.
    denied = [
        "/" + "Users/fixture/project",
        "/" + "home/fixture/project",
        "/" + "Volumes/fixture/project",
        "node." + "tail123abc" + ".ts.net",
        "NODE." + "TAIL123ABC" + ".TS.NET",
    ]
    check_fixture("neutral\n")  # Fork/no-secret behavior.
    check_fixture("neutral\n", binary=denied[0])
    check_fixture("neutral\n", extra="\n \n", file_extra="\n\t\n")
    check_fixture("/System/Volumes/Preboot/library\n/home/read\n$HOME/project\n")
    for text in denied:
        check_fixture(text + "\n", expected=1)
    check_fixture(denied[0] + "\n", expected=1, mutate_guard=True)
    expression = "external-example-[0-9]+"
    output = check_fixture("external-example-42\n", extra=expression, expected=1)
    assert expression not in output
    check_fixture("file-example-42\n", file_extra="\nfile-example-[0-9]+\n", expected=1)
    check_fixture("file-example-42\n", extra=expression, file_extra="file-example-[0-9]+", expected=1)
    check_fixture("external-example-42\n", extra=expression, file_extra="file-example-[0-9]+", expected=1)
    malformed = "confidential-synthetic-["
    output = check_fixture("neutral\n", extra=malformed, expected=128)
    assert malformed not in output
    check_fixture("neutral\n", git=False, expected=128)
    check_fixture("neutral\n", missing_file=True, expected=2)
    print("OSS boundary guard fixtures passed.")


if __name__ == "__main__":
    main()
