#!/usr/bin/env python3
"""Usage: command.py <series> -- <command> [args...]. No shell is involved."""
import os
import subprocess
import sys

if len(sys.argv) < 4 or sys.argv[2] != "--":
    raise SystemExit("usage: command.py <series> -- <command> [args...]")
subprocess.run(["anx", "series", "push", sys.argv[1], "--adapter", os.environ["ANX_ADAPTER"], "--from-command", "--", *sys.argv[3:]], check=True)
