from pathlib import Path
import tempfile
import unittest

from oar_core.schema import read_schema_version


class ReadSchemaVersionTests(unittest.TestCase):
    def test_reads_version_from_contract(self) -> None:
        contract_path = Path(__file__).resolve().parents[1] / "contracts" / "oar-schema.yaml"
        self.assertEqual(read_schema_version(contract_path), "0.2.2")

    def test_raises_for_missing_version(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            schema_path = Path(tmp_dir) / "schema.yaml"
            schema_path.write_text("name: test\n", encoding="utf-8")
            with self.assertRaises(ValueError):
                read_schema_version(schema_path)


if __name__ == "__main__":
    unittest.main()
