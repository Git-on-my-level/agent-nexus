import io
import sys
import unittest
from contextlib import redirect_stderr
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from fleet_sync import _quiet_status, validate_text


class QuietTests(unittest.TestCase):
    def test_reader_failure_is_one_line_and_nonzero(self):
        reads = [
            {"name": "prometheus", "ok": False, "error": "timed out"},
            {"name": "fleetctl", "ok": True},
        ]
        with redirect_stderr(io.StringIO()) as err:
            code = _quiet_status(reads, valid=True, diagnostics=[], errors=[])
        self.assertEqual(code, 1)
        self.assertEqual(err.getvalue().strip(), "prometheus: timed out")
        with redirect_stderr(io.StringIO()) as err:
            code = _quiet_status(reads, valid=False, diagnostics=["panels: bad"], errors=[])
        self.assertEqual(code, 1)
        self.assertIn("report failed validation", err.getvalue())
        self.assertIn("prometheus: timed out", err.getvalue())

    def test_missing_node_binary_is_named(self):
        ok, errors = validate_text("{}", node_bin="/no/such/node")
        self.assertFalse(ok)
        self.assertIn("/no/such/node", errors[0])


if __name__ == "__main__":
    unittest.main()
