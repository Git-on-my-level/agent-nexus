"""Offline checks for starter source mapping; no provider or ANX credentials."""
import datetime as dt
import io
import json
from pathlib import Path
import runpy
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import common

ROOT = Path(__file__).parent

class AdaptersTest(unittest.TestCase):
    def test_github_groups_labels_and_keeps_open_and_merged_distinct(self):
        now = dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")
        rows = [
            {"state":"closed", "created_at":now,"updated_at":now,"merged_at":now,"title":"Launch", "labels":[{"name":"initiative:launch"}]},
            {"state":"open", "created_at":now,"updated_at":now,"merged_at":None,"title":"Open", "labels":[{"name":"initiative:launch"}]},
            {"state":"closed", "created_at":"2020-01-01T00:00:00Z","updated_at":"2020-01-01T00:00:00Z","merged_at":None},
        ]
        points = []
        with patch.object(sys,"argv",["github-prs.py","owner/repo","--weeks","1"]), patch("subprocess.check_output",return_value=json.dumps(rows).encode()), patch.object(common,"push",side_effect=lambda *args:points.append(args)):
            runpy.run_path(str(ROOT/"github-prs.py"),run_name="__main__")
        counts = {(p[2]["initiative"],p[2]["status"]):p[1] for p in points}
        self.assertEqual(counts,{("launch","open"):1,("launch","merged"):1,("other","open"):0,("other","merged"):0})

    def test_prometheus_keeps_source_timestamp_and_only_allowed_labels(self):
        payload = {"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"api","credential":"omit"},"value":[1700000000,"2.5"]}]}}
        points = []
        with patch.object(sys,"argv",["prometheus-query.py","latency","sum(rate(x[5m]))","--labels","job"]), patch.dict("os.environ",{"PROMETHEUS_URL":"https://prometheus.example"}), patch("urllib.request.urlopen",return_value=io.BytesIO(json.dumps(payload).encode())), patch.object(common,"push",side_effect=lambda *args:points.append(args)):
            runpy.run_path(str(ROOT/"prometheus-query.py"),run_name="__main__")
        self.assertEqual(points,[("latency",2.5,{"job":"api"},"2023-11-14T22:13:20Z")])

    def test_generic_command_passes_argv_without_a_shell(self):
        with patch.object(sys,"argv",["command.py","count","--","printf","12; echo literal"]), patch.dict("os.environ",{"ANX_ADAPTER":"command"}), patch("subprocess.run") as run:
            runpy.run_path(str(ROOT/"command.py"),run_name="__main__")
        self.assertEqual(run.call_args.args[0][-3:],["--","printf","12; echo literal"])
        self.assertNotIn("shell",run.call_args.kwargs)

if __name__ == "__main__":
    unittest.main()
