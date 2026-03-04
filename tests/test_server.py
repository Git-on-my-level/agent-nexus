import json
import sqlite3
import threading
import tempfile
import unittest
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import urlopen

from http.server import ThreadingHTTPServer

from oar_core.server import create_app_state, make_handler


class ServerEndpointsTests(unittest.TestCase):
    def setUp(self) -> None:
        self.contract_path = Path(__file__).resolve().parents[1] / "contracts" / "oar-schema.yaml"
        self._tmp_dir = tempfile.TemporaryDirectory()
        self.workspace_root = Path(self._tmp_dir.name) / "workspace"
        self.httpd: ThreadingHTTPServer | None = None
        self.thread: threading.Thread | None = None
        self.base_url = ""
        self.app_state = None

    def tearDown(self) -> None:
        self._stop_server()
        self._tmp_dir.cleanup()

    def _start_server(self) -> None:
        self.app_state = create_app_state(
            schema_path=self.contract_path,
            workspace_root=self.workspace_root,
        )
        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(self.app_state))
        self.thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)
        self.thread.start()
        self.base_url = f"http://127.0.0.1:{self.httpd.server_port}"

    def _stop_server(self) -> None:
        if self.httpd is None or self.thread is None:
            return
        self.httpd.shutdown()
        self.httpd.server_close()
        self.thread.join(timeout=2)
        self.httpd = None
        self.thread = None

    def _get_json_with_status(self, path: str) -> tuple[int, dict[str, object]]:
        try:
            with urlopen(f"{self.base_url}{path}") as response:
                body = response.read().decode("utf-8")
                return response.getcode(), json.loads(body)
        except HTTPError as exc:
            body = exc.read().decode("utf-8")
            return exc.code, json.loads(body)

    def test_health_endpoint(self) -> None:
        self._start_server()
        status, payload = self._get_json_with_status("/health")
        self.assertEqual(status, 200)
        self.assertEqual(payload, {"ok": True})

    def test_version_endpoint(self) -> None:
        self._start_server()
        status, payload = self._get_json_with_status("/version")
        self.assertEqual(status, 200)
        self.assertEqual(payload, {"schema_version": "0.2.2"})

    def test_workspace_initialized_and_restart_is_idempotent(self) -> None:
        self._start_server()
        assert self.app_state is not None

        db_path = self.app_state.storage.paths.db_path
        artifacts_dir = self.app_state.storage.paths.artifacts_dir
        self.assertTrue(db_path.exists())
        self.assertTrue(artifacts_dir.exists())
        self.assertTrue(artifacts_dir.is_dir())

        expected_tables = {
            "schema_migrations",
            "events",
            "snapshots",
            "artifacts",
            "actor_registry",
            "derived_views",
        }
        with sqlite3.connect(db_path) as conn:
            rows = conn.execute(
                "SELECT name FROM sqlite_master WHERE type = 'table'"
            ).fetchall()
            tables = {row[0] for row in rows}
            self.assertTrue(expected_tables.issubset(tables))
            conn.execute(
                """
                INSERT INTO events(
                    id, ts, type, actor_id, thread_id,
                    refs_json, summary, payload_json, provenance_json
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    "evt_restart_test",
                    "2026-03-04T00:00:00Z",
                    "message_posted",
                    "actor_test",
                    None,
                    "[]",
                    "restart persistence test",
                    None,
                    '{"sources":["inferred"]}',
                ),
            )
            conn.commit()

        self._stop_server()

        self._start_server()
        with sqlite3.connect(db_path) as conn:
            row = conn.execute(
                "SELECT summary FROM events WHERE id = ?",
                ("evt_restart_test",),
            ).fetchone()
            self.assertIsNotNone(row)
            assert row is not None
            self.assertEqual(row[0], "restart persistence test")

    def test_health_returns_non_ok_when_db_unavailable(self) -> None:
        self._start_server()
        assert self.app_state is not None

        db_path = self.app_state.storage.paths.db_path
        db_path.unlink()
        db_path.mkdir()

        status, payload = self._get_json_with_status("/health")
        self.assertEqual(status, 503)
        self.assertEqual(payload.get("ok"), False)
        self.assertEqual(payload.get("error"), "storage_unavailable")


if __name__ == "__main__":
    unittest.main()
