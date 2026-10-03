"""Read-only Hermes cron jobs and open incidents. Prompts are dropped immediately."""

from __future__ import annotations

import json
import re
from datetime import datetime, timezone
from typing import Any

from .run import HostExec

JOB_FIELDS = (
    "id", "name", "schedule", "enabled", "state", "paused_at", "paused_reason",
    "last_run", "last_status", "last_error", "next_run", "workdir", "deliver",
)
_TOKEN = re.compile(
    r"(?i)(bearer\s+[a-z0-9\-._~+/]+=*|ghp_[a-z0-9]{20,}|github_pat_[a-z0-9_]{20,}|"
    r"sk-[a-z0-9]{16,}|xox[baprs]-[a-z0-9-]{10,}|AKIA[0-9A-Z]{16}|"
    r"(?:api[_-]?key|token|secret|password)\s*[=:]\s*\S+)"
)
_HEX = re.compile(r"\b[a-f0-9]{32,}\b", re.IGNORECASE)
_DIR_SCRIPT = (
    "while IFS= read -r p; do "
    "if [ -d \"$p\" ]; then printf 'yes\\n'; else printf 'no\\n'; fi; "
    "done"
)
_INCIDENT = re.compile(r"^  (\S+)\s+(alerted|detected)\s*$", re.MULTILINE)
_ANSI = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")
_EMPTY_INCIDENTS = "No cron failure incidents recorded."


def scrub(value: Any, limit: int = 200) -> str:
    text = "" if value is None else str(value)
    text = _TOKEN.sub("[redacted]", text)
    text = _HEX.sub("[redacted]", text)
    text = re.sub(r"\s+", " ", text).strip()
    return text[:limit]


def schedule_text(value: Any) -> str:
    if isinstance(value, dict):
        raw = value.get("display") or value.get("expr") or ""
        return scrub(raw, 120)
    return scrub(value, 120)


def project_job(raw: dict) -> dict:
    """Copy the allowlisted job fields. Prompt, script, env, and model never leave this function."""
    job = {key: raw.get(key) for key in JOB_FIELDS if key in raw}
    if "last_run" not in job and "last_run_at" in raw:
        job["last_run"] = raw.get("last_run_at")
    if "next_run" not in job and "next_run_at" in raw:
        job["next_run"] = raw.get("next_run_at")
    if "schedule" not in job and isinstance(raw.get("schedule"), dict):
        job["schedule"] = raw.get("schedule")
    job["last_error"] = scrub(job.get("last_error"), 200)
    job["paused_reason"] = scrub(job.get("paused_reason"), 200)
    job["name"] = scrub(job.get("name"), 160)
    job["id"] = str(raw.get("id") or "")
    job["schedule"] = schedule_text(job.get("schedule"))
    workdir = raw.get("workdir")
    job["workdir"] = workdir if isinstance(workdir, str) and workdir.strip() else None
    return job


def job_problems(job: dict, *, workdir_missing: bool | None) -> list[str]:
    problems = []
    paused = bool(job.get("paused_at")) or str(job.get("state") or "").lower() == "paused"
    if paused and not str(job.get("paused_reason") or "").strip():
        problems.append("paused-no-reason")
    if str(job.get("last_status") or "").lower() in {"error", "failed"}:
        problems.append("error")
    if workdir_missing is True:
        problems.append("missing-workdir")
    return problems


def parse_jobs(payload: Any) -> list[dict]:
    jobs = payload.get("jobs") if isinstance(payload, dict) else payload
    if not isinstance(jobs, list):
        raise ValueError("jobs.json is missing a jobs list")
    return [project_job(job) for job in jobs if isinstance(job, dict) and job.get("id")]


def parse_incidents(text: str) -> list[dict]:
    """Parse `hermes cron incidents` text.

    Empty output and the known empty sentence are zero incidents. Any other
    text that does not match the incident header is an unrecognized format:
    the caller must treat the read as incomplete instead of as an empty list.
    """
    raw = _ANSI.sub("", text or "")
    incidents = []
    matches = list(_INCIDENT.finditer(raw))
    if not matches:
        if not raw.strip() or _EMPTY_INCIDENTS in raw:
            return []
        raise ValueError("hermes cron incidents output was not recognized")
    for index, match in enumerate(matches):
        end = matches[index + 1].start() if index + 1 < len(matches) else len(raw)
        block = raw[match.end():end]
        job = re.search(r"^\s+Job:\s+(\S+)\s*$", block, re.MULTILINE)
        kind = re.search(r"^\s+Type:\s+(\S+)\s*$", block, re.MULTILINE)
        error = re.search(r"^\s+Error:\s+(.*)$", block, re.MULTILINE)
        incidents.append({
            "id": match.group(1),
            "state": match.group(2),
            "job_id": job.group(1) if job else None,
            "type": kind.group(1) if kind else None,
            "error": scrub(error.group(1) if error else "", 200),
        })
    return incidents


def read_hermes(exec_: HostExec, config: dict, *, now: datetime) -> dict:
    observed = now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    hosts = []
    items = []
    present = []
    errors = []
    any_ok = False
    for host in config.get("hosts") or []:
        label = str(host.get("label") or "")
        snapshot = _read_host(exec_, host, label)
        hosts.append(snapshot)
        if snapshot["ok"]:
            any_ok = True
            items.extend(snapshot["items"])
            present.extend(f"{label}/{job_id}" for job_id in snapshot["job_ids"])
        else:
            errors.append(f"{label}: {snapshot['error']}")
    if not hosts:
        errors.append("no hosts configured")
    unchecked = [host.get("label") or "host" for host in hosts if not host.get("complete")]
    return {
        "name": "hermes",
        "ok": bool(hosts) and not errors,
        "complete": bool(hosts) and not errors and not unchecked,
        "observed_at": observed,
        "error": "; ".join(errors)[:500] if errors else None,
        "items": items,
        "present_ids": present,
        "meta": {
            "hosts": hosts,
            "partial": any_ok and (bool(errors) or bool(unchecked)),
            "unchecked_hosts": unchecked,
        },
    }


def _read_host(exec_: HostExec, host: dict, label: str) -> dict:
    try:
        jobs_path = _local_jobs() if host.get("local") else "$HOME/.hermes/cron/jobs.json"
        listed = exec_.run(host, ["cat", jobs_path], timeout=25)
        if not listed.ok:
            if _missing_tool(listed):
                return _host_fail(label, "hermes jobs.json is unavailable")
            return _host_fail(label, listed.error or "could not read jobs.json")
        try:
            jobs = parse_jobs(json.loads(listed.stdout))
        except json.JSONDecodeError as exc:
            raise ValueError("jobs.json is not valid JSON") from exc
        incidents = []
        incidents_complete = True
        for state in ("alerted", "detected"):
            result = exec_.run(host, ["hermes", "cron", "incidents", "--state", state], timeout=25)
            if not result.ok:
                if _missing_tool(result):
                    return _host_fail(label, "hermes is not installed")
                return _host_fail(label, result.error or f"hermes cron incidents --state {state} failed")
            try:
                incidents.extend(parse_incidents(result.stdout))
            except ValueError:
                # Unrecognized text is not an empty incident list.
                incidents_complete = False
        missing, dirs_checked = _missing_workdirs(exec_, host, jobs)
        job_ids = {job["id"] for job in jobs}
        items = []
        rows = []
        for job in jobs:
            problems = job_problems(job, workdir_missing=missing.get(job["id"]))
            row = {
                "host": label,
                "id": job["id"],
                "name": job["name"],
                "schedule": job["schedule"],
                "problems": problems,
                "last_status": job.get("last_status"),
                "paused": "paused-no-reason" in problems,
                "watchdog": "watchdog" in str(job.get("name") or "").lower(),
            }
            rows.append(row)
            if problems:
                items.append(_problem_item(label, job, problems))
        for incident in incidents:
            if incident.get("job_id") in job_ids:
                continue
            items.append({
                "native_id": f"incident:{incident['id']}",
                "host": label,
                "title": f"cron incident {incident['id']}",
                "kind": "incident",
                "problems": ["orphan-incident"],
                "summary": incident.get("error") or "Open incident whose job no longer exists",
                "native_status": incident.get("state") or "open",
                "terminal": False,
            })
        return {
            "label": label,
            "ok": True,
            "complete": dirs_checked and incidents_complete,
            "error": None,
            "jobs": len(jobs),
            "rows": rows,
            "items": items,
            "job_ids": [job["id"] for job in jobs],
        }
    except (RuntimeError, ValueError, OSError) as exc:
        return _host_fail(label, str(exc))


def _problem_item(label: str, job: dict, problems: list[str]) -> dict:
    bits = []
    if "paused-no-reason" in problems:
        bits.append("paused with no reason")
    if "error" in problems:
        detail = job.get("last_error") or job.get("last_status") or "error"
        bits.append(f"last status {detail}")
    if "missing-workdir" in problems:
        bits.append("workdir is missing")
    return {
        "native_id": job["id"],
        "host": label,
        "title": job.get("name") or job["id"],
        "kind": "job",
        "problems": problems,
        "summary": "; ".join(bits),
        "native_status": ",".join(problems),
        "schedule": job.get("schedule") or "",
        "terminal": False,
        "watchdog": "watchdog" in str(job.get("name") or "").lower(),
    }


def _missing_workdirs(exec_: HostExec, host: dict, jobs: list[dict]) -> tuple[dict[str, bool | None], bool]:
    checked = []
    for job in jobs:
        workdir = job.get("workdir")
        if not isinstance(workdir, str) or not workdir.startswith("/") or "\n" in workdir:
            continue
        checked.append(job)
        if len(checked) >= 50:
            break
    if not checked:
        return {}, True
    result = exec_.run(
        host, ["sh", "-c", _DIR_SCRIPT], timeout=20,
        input_text="".join(f"{job['workdir']}\n" for job in checked),
    )
    if not result.ok:
        return {job["id"]: None for job in checked}, False
    lines = [line.strip() for line in result.stdout.splitlines() if line.strip()]
    out: dict[str, bool | None] = {}
    for job, line in zip(checked, lines):
        out[job["id"]] = line == "no"
    if len(lines) < len(checked):
        return out, False
    return out, True


def _local_jobs() -> str:
    from pathlib import Path
    return str(Path.home() / ".hermes" / "cron" / "jobs.json")


def _missing_tool(result) -> bool:
    text = f"{result.stderr}\n{result.error or ''}".lower()
    return result.code == 127 or "not found" in text or "no such file" in text


def _host_fail(label: str, error: str) -> dict:
    return {
        "label": label,
        "ok": False,
        "error": error[:300],
        "jobs": None,
        "rows": [],
        "items": [],
        "complete": False,
        "job_ids": [],
    }
