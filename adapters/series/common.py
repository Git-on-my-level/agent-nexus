"""Host-local adapter helpers. Credentials remain in gh/anx/local environment."""
import datetime as dt
import json
import math
import os
import subprocess
import time


def timestamp(seconds=None):
    value = dt.datetime.now(dt.timezone.utc) if seconds is None else dt.datetime.fromtimestamp(seconds, dt.timezone.utc)
    return value.isoformat().replace("+00:00", "Z")


def push(series, value, labels=None, ts=None):
    if not math.isfinite(float(value)):
        raise ValueError("non-finite source sample")
    point = {"series": series, "value": float(value), "labels": labels or {}, "ts": ts or timestamp()}
    # argv, never shell interpolation. anx exchanges the owner's ordinary host
    # identity for a short-lived token scoped to this declared adapter.
    argv = ["anx", "--json", "series", "push", "--adapter", os.environ["ANX_ADAPTER"], "--from-command", "--", "python3", "-c", "import sys; print(sys.argv[1])", json.dumps(point)]
    for attempt in range(4):
        result = subprocess.run(argv, capture_output=True, text=True, timeout=90)
        if result.returncode == 0:
            return
        try:
            code = json.loads(result.stdout).get("error", {}).get("code", "")
        except (ValueError, AttributeError):
            code = ""
        if code == "series_rate_limited" and attempt < 3:
            # Retry the same observation, with a full UTC-minute delay. Never
            # retry cardinality/daily caps, auth failures or provider commands.
            time.sleep(60)
            continue
        raise RuntimeError(f"ANX push failed: {code or result.returncode}")
