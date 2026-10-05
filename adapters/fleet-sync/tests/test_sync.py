import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import Mock, patch
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from initiatives import DuplicateSourceIdentityConflict
from fleet_sync import _quiet_status, publish, report_publication_enabled, validate_text, main


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


class ReportPublicationTests(unittest.TestCase):
    def test_report_publication_defaults_on_and_can_be_disabled(self):
        self.assertTrue(report_publication_enabled({}))
        self.assertTrue(report_publication_enabled({"report": {}}))
        self.assertFalse(report_publication_enabled({"report": {"publish": False}}))

    def test_disabled_report_does_not_call_dashboard_document_apis(self):
        client = Mock()
        result = publish(client, {"report": {"publish": False}}, {}, {}, "node", Path("validator"))
        self.assertEqual(result, {"report_published": False})
        client.docs_list.assert_not_called()
        client.docs_create.assert_not_called()
        client.docs_revise.assert_not_called()
        client.docs_content.assert_not_called()

    def test_main_skips_report_generation_and_validation_when_disabled(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "config.json"
            config.write_text(json.dumps({
                "base_url": "https://nexus.example/ws/product",
                "agent": "fleet-sync",
                "topic": "fleet-operations",
                "mapping_doc": "doc:mapping",
                "report": {"publish": False},
            }), encoding="utf-8")
            client = Mock()
            client.docs_content.return_value = json.dumps({"version": 1, "workspace": "https://nexus.example/ws/product", "rules": []})
            output = io.StringIO()
            with patch("fleet_sync.AnxClient", return_value=client), \
                 patch("fleet_sync.BudgetRunner"), \
                 patch("fleet_sync.validate_mapping"), \
                 patch("fleet_sync.collect", return_value=[]), \
                 patch("fleet_sync.source_items", return_value=[]), \
                 patch("fleet_sync.plan_ingestion", return_value=[]), \
                 patch("fleet_sync.load_state", return_value={}), \
                 patch("fleet_sync.apply_ingestion", return_value={"errors": []}) as apply_ingestion, \
                 patch("fleet_sync.save_state"), \
                 patch("fleet_sync.build_report", side_effect=AssertionError("report was built")), \
                 patch("fleet_sync.validate_report", side_effect=AssertionError("report was validated")), \
                 patch("fleet_sync.publish", side_effect=AssertionError("report was published")), \
                 redirect_stdout(output):
                code = main(["--config", str(config), "--state", str(Path(directory) / "state.json")])

            self.assertEqual(code, 0)
            summary = json.loads(output.getvalue())
            self.assertFalse(summary["report_published"])
            apply_ingestion.assert_called_once()
            client.docs_list.assert_not_called()
            client.docs_create.assert_not_called()
            client.docs_revise.assert_not_called()

    def test_planning_conflict_is_returned_in_run_summary(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "config.json"
            config.write_text(json.dumps({
                "base_url": "https://nexus.example/ws/product",
                "agent": "fleet-sync",
                "topic": "fleet-operations",
                "mapping_doc": "doc:mapping",
                "report": {"publish": False},
            }), encoding="utf-8")
            client = Mock()
            client.docs_content.return_value = json.dumps({
                "version": 1, "workspace": "https://nexus.example/ws/product", "rules": []})
            output = io.StringIO()
            conflict = DuplicateSourceIdentityConflict(
                'conflicting source item "SCA-1 · Example" (identity=["multica","local","1"]): '
                'differing fields project change its initiative route')
            with patch("fleet_sync.AnxClient", return_value=client), \
                 patch("fleet_sync.BudgetRunner"), \
                 patch("fleet_sync.validate_mapping"), \
                 patch("fleet_sync.collect", return_value=[]), \
                 patch("fleet_sync.source_items", return_value=[]), \
                 patch("fleet_sync.plan_ingestion", side_effect=conflict), \
                 patch("fleet_sync.apply_ingestion") as apply_ingestion, \
                 redirect_stdout(output):
                code = main(["--config", str(config), "--state", str(Path(directory) / "state.json")])

            self.assertEqual(code, 1)
            summary = json.loads(output.getvalue())
            self.assertIn('identity=["multica","local","1"]', summary["errors"][0])
            self.assertIn("project", summary["errors"][0])
            self.assertEqual(summary["warnings"], [])
            apply_ingestion.assert_not_called()


if __name__ == "__main__":
    unittest.main()
