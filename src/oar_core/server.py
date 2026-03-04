"""Minimal HTTP server skeleton for oar-core."""

from __future__ import annotations

import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from .schema import DEFAULT_SCHEMA_PATH, read_schema_version


def make_handler(schema_version: str) -> type[BaseHTTPRequestHandler]:
    """Build a request handler bound to a specific schema version."""

    class Handler(BaseHTTPRequestHandler):
        def _send_json(self, payload: dict[str, object], status_code: int = 200) -> None:
            body = json.dumps(payload).encode("utf-8")
            self.send_response(status_code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self) -> None:  # noqa: N802 (BaseHTTPRequestHandler API)
            if self.path == "/health":
                self._send_json({"ok": True})
                return

            if self.path == "/version":
                self._send_json({"schema_version": schema_version})
                return

            self._send_json({"error": "not_found"}, status_code=404)

        def log_message(self, format: str, *args: object) -> None:
            # Keep test output clean.
            return

    return Handler


def run_server(host: str, port: int, schema_path: Path) -> None:
    schema_version = read_schema_version(schema_path)
    server = ThreadingHTTPServer((host, port), make_handler(schema_version))
    print(f"oar-core server listening on http://{host}:{port}")
    server.serve_forever()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run the oar-core development server.")
    parser.add_argument("--host", default="127.0.0.1", help="Host interface to bind")
    parser.add_argument("--port", type=int, default=8000, help="Port to listen on")
    parser.add_argument(
        "--schema-path",
        type=Path,
        default=DEFAULT_SCHEMA_PATH,
        help="Path to contracts/oar-schema.yaml",
    )
    return parser.parse_args()


if __name__ == "__main__":
    args = parse_args()
    run_server(host=args.host, port=args.port, schema_path=args.schema_path)
