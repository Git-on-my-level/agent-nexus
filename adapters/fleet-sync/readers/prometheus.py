"""Read-only Prometheus alerts. Firing alerts become cards; pending alerts are counted."""

from __future__ import annotations

import hashlib
import json
import re
from datetime import datetime, timezone
from typing import Any
from urllib.parse import urlsplit

from .run import HostExec, valid_ssh_alias

SEVERITY_RANK = {"critical": 0, "error": 1, "warning": 2, "info": 3, "none": 4}
CHART_SEVERITIES = ("critical", "warning", "info", "other")
_DISPLAY = ("mountpoint", "apfs_container", "name", "device", "path", "volume", "service")
# Localhost only. Query strings and userinfo are rejected so `&` and `?`
# cannot be smuggled into the remote shell even before quoting.
_ALERTS_URL = re.compile(
    r"^http://(?:127\.0\.0\.1|localhost)(?::[0-9]{1,5})?(?:/[A-Za-z0-9._~/-]*)?$"
)


def identifying_labels(labels: dict) -> dict[str, str]:
    found = {}
    for key, value in labels.items():
        if key == "alertname" or not isinstance(key, str):
            continue
        found[key] = "" if value is None else str(value)
    return found


def alert_native_id(alertname: str, labels: dict) -> str:
    body = json.dumps(identifying_labels(labels), sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    digest = hashlib.sha256(body.encode()).hexdigest()[:16]
    safe = re.sub(r"[^A-Za-z0-9._:-]+", "-", alertname).strip("-") or "alert"
    return f"{safe}/{digest}"


def host_from_instance(instance: str, suffix: str) -> str:
    host = instance.strip()
    if ":" in host:
        left, port = host.rsplit(":", 1)
        if port.isdigit():
            host = left
    if suffix and host.endswith(suffix):
        trimmed = host[: -len(suffix)].rstrip(".")
        if trimmed:
            return trimmed
    return host or instance


def alert_title(alertname: str, host: str, labels: dict) -> str:
    extra = ""
    for key in _DISPLAY:
        value = labels.get(key)
        if isinstance(value, str) and value.strip():
            extra = value.strip()
            break
    if host and extra:
        return f"{alertname} on {host} ({extra})"
    if host:
        return f"{alertname} on {host}"
    return alertname


def _severity(labels: dict) -> str:
    value = str(labels.get("severity") or "none").strip().lower()
    return value or "none"


def normalize_firing(labels: dict, *, suffix: str) -> dict | None:
    alertname = str(labels.get("alertname") or "").strip()
    if not alertname:
        return None
    instance = str(labels.get("instance") or "")
    host = host_from_instance(instance, suffix) if instance else ""
    return {
        "native_id": alert_native_id(alertname, labels),
        "title": alert_title(alertname, host, labels),
        "severity": _severity(labels),
        "host": host,
        "instance": instance,
        "state": "firing",
        "terminal": False,
    }


def parse_alerts(payload: Any, *, suffix: str) -> dict:
    if not isinstance(payload, dict) or payload.get("status") != "success":
        raise ValueError("prometheus alerts query was not successful")
    data = payload.get("data")
    alerts = data.get("alerts") if isinstance(data, dict) else None
    if not isinstance(alerts, list):
        raise ValueError("prometheus alerts payload is missing data.alerts")
    firing = []
    pending = 0
    for alert in alerts:
        if not isinstance(alert, dict):
            continue
        state = str(alert.get("state") or "")
        if state == "pending":
            pending += 1
            continue
        if state != "firing":
            continue
        labels = alert.get("labels") if isinstance(alert.get("labels"), dict) else {}
        item = normalize_firing(labels, suffix=suffix)
        if item:
            firing.append(item)
    firing.sort(key=lambda item: (SEVERITY_RANK.get(item["severity"], 9), item["title"], item["native_id"]))
    return {"firing": firing, "pending": pending}


def valid_alerts_url(url: str) -> bool:
    if not _ALERTS_URL.fullmatch(url or ""):
        return False
    parts = urlsplit(url)
    host = parts.hostname
    if parts.scheme != "http" or host not in {"127.0.0.1", "localhost"}:
        return False
    if parts.username or parts.password or parts.query or parts.fragment:
        return False
    return True


def read_prometheus(exec_: HostExec, config: dict, *, now: datetime) -> dict:
    observed = now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    prom = config.get("prometheus") or {}
    alias = str(prom.get("ssh") or "").strip()
    url = str(prom.get("alerts_url") or "http://127.0.0.1:9090/api/v1/alerts")
    suffix = str(prom.get("instance_suffix") or "")
    if not alias:
        return _fail(observed, "prometheus ssh host is required")
    if not valid_ssh_alias(alias) or not valid_alerts_url(url):
        return _fail(observed, "prometheus ssh alias must be a strict token and alerts_url must be http://127.0.0.1 or localhost")
    # curl -m 10 is the remote limit. The ssh process timeout is the hard kill.
    result = exec_.run({"ssh": alias}, ["curl", "-s", "-m", "10", url], timeout=25)
    if not result.ok:
        return _fail(observed, result.error or "prometheus alerts query failed")
    try:
        parsed = parse_alerts(json.loads(result.stdout), suffix=suffix)
    except (json.JSONDecodeError, ValueError) as exc:
        return _fail(observed, str(exc))
    return {
        "name": "prometheus",
        "ok": True,
        "complete": True,
        "observed_at": observed,
        "error": None,
        "items": parsed["firing"],
        "present_ids": [item["native_id"] for item in parsed["firing"]],
        "meta": {"pending": parsed["pending"], "firing": len(parsed["firing"]), "ssh": alias},
    }


def _fail(observed: str, error: str) -> dict:
    return {
        "name": "prometheus",
        "ok": False,
        "complete": False,
        "observed_at": observed,
        "error": error[:500],
        "items": [],
        "present_ids": [],
        "meta": {"pending": None, "firing": None},
    }
