import json
import threading
import unittest
from pathlib import Path
from urllib.request import urlopen

from http.server import ThreadingHTTPServer

from oar_core.schema import read_schema_version
from oar_core.server import make_handler


class ServerEndpointsTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        contract_path = Path(__file__).resolve().parents[1] / "contracts" / "oar-schema.yaml"
        schema_version = read_schema_version(contract_path)
        cls.httpd = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(schema_version))
        cls.thread = threading.Thread(target=cls.httpd.serve_forever, daemon=True)
        cls.thread.start()
        cls.base_url = f"http://127.0.0.1:{cls.httpd.server_port}"

    @classmethod
    def tearDownClass(cls) -> None:
        cls.httpd.shutdown()
        cls.httpd.server_close()
        cls.thread.join(timeout=2)

    def _get_json(self, path: str) -> dict[str, object]:
        with urlopen(f"{self.base_url}{path}") as response:
            body = response.read().decode("utf-8")
            return json.loads(body)

    def test_health_endpoint(self) -> None:
        payload = self._get_json("/health")
        self.assertEqual(payload, {"ok": True})

    def test_version_endpoint(self) -> None:
        payload = self._get_json("/version")
        self.assertEqual(payload, {"schema_version": "0.2.2"})


if __name__ == "__main__":
    unittest.main()
