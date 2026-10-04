#!/usr/bin/env python3
"""Prometheus instant/vector/scalar or range/matrix -> declared ANX series."""
import argparse
import datetime as dt
import json
import os
import urllib.parse
import urllib.request
from common import push, timestamp

parser = argparse.ArgumentParser()
parser.add_argument("series")
parser.add_argument("query")
parser.add_argument("--range-seconds", type=int, default=0)
parser.add_argument("--step-seconds", type=int, default=300)
parser.add_argument("--labels", default="", help="Comma-separated allowlist of metric label keys; never forward arbitrary labels")
args = parser.parse_args()
if not 0 <= args.range_seconds <= 90*86400 or args.step_seconds < 1 or args.range_seconds / args.step_seconds > 199:
    raise SystemExit("range must be <=90 days and yield at most 200 samples")
base = os.environ["PROMETHEUS_URL"].rstrip("/")
parsed = urllib.parse.urlsplit(base)
if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password:
    raise SystemExit("PROMETHEUS_URL must be an HTTP(S) URL without credentials")
params = {"query": args.query}
endpoint = "query"
if args.range_seconds:
    endpoint = "query_range"
    end = dt.datetime.now(dt.timezone.utc).timestamp()
    params.update(start=end-args.range_seconds, end=end, step=args.step_seconds)
headers = {}
if os.environ.get("PROMETHEUS_TOKEN"):
    headers["Authorization"] = "Bearer " + os.environ["PROMETHEUS_TOKEN"]
request = urllib.request.Request(base + "/api/v1/" + endpoint + "?" + urllib.parse.urlencode(params), headers=headers)
with urllib.request.urlopen(request, timeout=30) as response:
    raw = response.read(2*1024*1024+1)
if len(raw) > 2*1024*1024:
    raise SystemExit("Prometheus response exceeds 2 MiB; narrow the query")
result = json.loads(raw)
if result.get("status") != "success":
    raise SystemExit("Prometheus query failed")
data = result["data"]
rows = data["result"]
if data["resultType"] == "scalar":
    rows = [{"metric": {}, "value": rows}]
elif data["resultType"] not in ("vector", "matrix"):
    raise SystemExit("query must return numeric scalar, vector or matrix")
if len(rows) > 100:
    raise SystemExit("query exceeds 100 label sets; aggregate it in PromQL")
keys = [key for key in args.labels.split(",") if key]
seen = set()
for row in rows:
    labels = {key: row["metric"][key] for key in keys if key in row["metric"]}
    identity = json.dumps(labels, sort_keys=True)
    if identity in seen:
        raise SystemExit("label allowlist collapses multiple Prometheus series; aggregate or include distinguishing keys")
    seen.add(identity)
    samples = row.get("values", [row.get("value")])
    if len(samples) > 200:
        raise SystemExit("too many samples; narrow the range")
    for ts, value in samples:
        push(args.series, float(value), labels, timestamp(float(ts)))
