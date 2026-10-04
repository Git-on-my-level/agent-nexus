"""Host-local adapter helpers. Credentials remain in gh/anx/local environment."""
import datetime as dt
import json
import math
import os
import subprocess


def timestamp(seconds=None):
    value = dt.datetime.now(dt.timezone.utc) if seconds is None else dt.datetime.fromtimestamp(seconds, dt.timezone.utc)
    return value.isoformat().replace("+00:00", "Z")


def push(series, value, labels=None, ts=None):
    if not math.isfinite(float(value)):
        raise ValueError("non-finite source sample")
    point = {"series": series, "value": float(value), "labels": labels or {}, "ts": ts or timestamp()}
    # argv, never shell interpolation. anx exchanges the owner's ordinary host
    # identity for a short-lived token scoped to this declared adapter.
    argv = ["anx", "series", "push", "--adapter", os.environ["ANX_ADAPTER"], "--from-command", "--", "python3", "-c", "import sys; print(sys.argv[1])", json.dumps(point)]
    subprocess.run(argv, check=True, stdout=subprocess.DEVNULL)
