from __future__ import annotations

import argparse
import json
import logging
import sys

from . import __version__
from .bridge import Bridge
from .config import load_config


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="anx-agent-bridge")
    parser.add_argument("--version", action="version", version=__version__)
    sub = parser.add_subparsers(dest="command", required=True)
    for command in ("run", "once", "doctor"):
        child = sub.add_parser(command)
        child.add_argument("--config", required=True)
    args = parser.parse_args(argv)
    logging.basicConfig(level=logging.INFO)
    try:
        config = load_config(args.config)
        bridge = Bridge(config)
        if args.command == "doctor":
            names = bridge.validate_roster()
            print(json.dumps({"ok": True, "host": config.host_slug, "agents": names,
                              "agentctl": bool(__import__("shutil").which(config.agentctl))}))
        elif args.command == "once":
            print(json.dumps({"handled": bridge.run_once()}))
        else:
            bridge.run_forever()
        return 0
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
