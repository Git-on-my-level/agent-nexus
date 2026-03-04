"""Minimal HTTP server skeleton for oar-core."""

from __future__ import annotations

import argparse
import json
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from .schema import DEFAULT_SCHEMA_PATH, read_schema_version
from .storage import WorkspaceStorage


DEFAULT_WORKSPACE_ROOT = Path(".oar-workspace")


@dataclass(frozen=True)
class AppState:
    schema_version: str
    storage: WorkspaceStorage


def create_app_state(schema_path: Path, workspace_root: Path) -> AppState:
    """Initialize app dependencies and return immutable runtime state."""
    storage = WorkspaceStorage(workspace_root)
    storage.initialize()
    return AppState(schema_version=read_schema_version(schema_path), storage=storage)


def make_handler(app_state: AppState) -> type[BaseHTTPRequestHandler]:
    """Build a request handler bound to app state."""

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
                ok, error = app_state.storage.check_connectivity()
                if ok:
                    self._send_json({"ok": True})
                else:
                    self._send_json(
                        {"ok": False, "error": "storage_unavailable", "detail": error},
                        status_code=503,
                    )
                return

            if self.path == "/version":
                self._send_json({"schema_version": app_state.schema_version})
                return

            self._send_json({"error": "not_found"}, status_code=404)

        def log_message(self, format: str, *args: object) -> None:
            # Keep test output clean.
            return

    return Handler


def run_server(host: str, port: int, schema_path: Path, workspace_root: Path) -> None:
    app_state = create_app_state(schema_path=schema_path, workspace_root=workspace_root)
    server = ThreadingHTTPServer((host, port), make_handler(app_state))
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
    parser.add_argument(
        "--workspace-root",
        type=Path,
        default=DEFAULT_WORKSPACE_ROOT,
        help="Root directory for SQLite DB and artifact content",
    )
    return parser.parse_args()


if __name__ == "__main__":
    args = parse_args()
    run_server(
        host=args.host,
        port=args.port,
        schema_path=args.schema_path,
        workspace_root=args.workspace_root,
    )
