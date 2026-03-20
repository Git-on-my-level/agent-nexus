#!/usr/bin/env python3

import argparse
import json
import sys


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Small JSON helper for hosted shell harnesses."
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    get_parser = subparsers.add_parser("get")
    get_parser.add_argument("path")

    count_parser = subparsers.add_parser("count-key-value")
    count_parser.add_argument("key")
    count_parser.add_argument("value")

    return parser.parse_args()


def load_json() -> object:
    try:
        return json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        print(f"invalid JSON input: {exc}", file=sys.stderr)
        raise SystemExit(2) from exc


def resolve_path(payload: object, path: str) -> object:
    if path in ("", "."):
        return payload

    current = payload
    for part in path.split("."):
        if isinstance(current, list):
            try:
                index = int(part)
            except ValueError as exc:
                raise KeyError(part) from exc
            current = current[index]
            continue
        if not isinstance(current, dict) or part not in current:
            raise KeyError(part)
        current = current[part]
    return current


def emit_value(value: object) -> None:
    if isinstance(value, (dict, list)):
        json.dump(value, sys.stdout, separators=(",", ":"))
        sys.stdout.write("\n")
        return
    if value is True:
        sys.stdout.write("true\n")
        return
    if value is False:
        sys.stdout.write("false\n")
        return
    if value is None:
        sys.stdout.write("null\n")
        return
    sys.stdout.write(f"{value}\n")


def normalize_value(value: object) -> str:
    if isinstance(value, (dict, list)):
        return json.dumps(value, separators=(",", ":"), sort_keys=True)
    if value is True:
        return "true"
    if value is False:
        return "false"
    if value is None:
        return "null"
    return str(value)


def count_key_value(payload: object, key: str, expected: str) -> int:
    if isinstance(payload, dict):
        count = 1 if key in payload and normalize_value(payload[key]) == expected else 0
        for value in payload.values():
            count += count_key_value(value, key, expected)
        return count

    if isinstance(payload, list):
        return sum(count_key_value(item, key, expected) for item in payload)

    return 0


def main() -> int:
    args = parse_args()
    payload = load_json()

    if args.command == "get":
        try:
            value = resolve_path(payload, args.path)
        except (KeyError, IndexError):
            return 1
        emit_value(value)
        return 0

    if args.command == "count-key-value":
        print(count_key_value(payload, args.key, args.value))
        return 0

    return 1


if __name__ == "__main__":
    raise SystemExit(main())
