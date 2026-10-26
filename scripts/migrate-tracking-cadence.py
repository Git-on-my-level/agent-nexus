#!/usr/bin/env python3
"""Report first, then infer cadence through the ordinary scoped ANX API."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from datetime import datetime, timezone


ALGORITHM = "median-interval-spread-20pct-v1"


def stamp(raw):
    parsed = datetime.fromisoformat(raw.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("receipt timestamps require an explicit time zone")
    return parsed.astimezone(timezone.utc)


def infer(receipts):
    """Return a whole-second median only when the entire sample is regular."""
    if len(receipts) < 3:
        return None, "fewer_than_three_receipts"
    times = sorted(stamp(row["at"]) for row in receipts)
    gaps = []
    for a, b in zip(times, times[1:]):
        delta = b - a
        gaps.append((delta.days * 86400 + delta.seconds) * 1_000_000 + delta.microseconds)
    ordered = sorted(gaps)
    middle = len(gaps) // 2
    median = ordered[middle] if len(gaps) % 2 else (ordered[middle - 1] + ordered[middle]) // 2
    if min(gaps) <= 0 or (max(gaps) - min(gaps)) * 5 > median:
        return None, "irregular_intervals"
    seconds = (median + 500_000) // 1_000_000
    if not 60 <= seconds <= 3_155_760_000:
        return None, "outside_supported_cadence_range"
    return seconds, "regular_receipts"


def fingerprint(receipts):
    raw = json.dumps(receipts, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(raw).hexdigest()


def atomic_json(path, value, exclusive=False):
    data = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
    path = Path(path)
    if exclusive:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "wb") as out:
            out.write(data)
            out.flush()
            os.fsync(out.fileno())
    else:
        fd, temporary = tempfile.mkstemp(prefix=path.name + ".", dir=path.parent)
        try:
            with os.fdopen(fd, "wb") as out:
                out.write(data)
                out.flush()
                os.fsync(out.fileno())
            os.replace(temporary, path)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
    parent_fd = os.open(path.parent, os.O_RDONLY)
    try:
        os.fsync(parent_fd)
    finally:
        os.close(parent_fd)


class Migration:
    def __init__(self, workspace, actor, call=None):
        self.workspace = workspace
        self.actor = actor
        self.call = call or self.command

    def command(self, args):
        argv = ["anx", "--workspace", self.workspace, "--as", self.actor, "--json", *args]
        result = subprocess.run(argv, check=True, stdout=subprocess.PIPE, text=True)
        body = json.loads(result.stdout)
        if not body.get("ok"):
            raise RuntimeError("ANX command failed; preserve the report and apply journal")
        return body["result"]

    def api(self, operation, params=None, body=None):
        args = ["api", operation]
        for key, value in (params or {}).items():
            args += ["--param", f"{key}={value}"]
        if body is not None:
            args += ["--json-body", json.dumps(body, separators=(",", ":"))]
        return self.call(args)

    def binding(self):
        config = self.call(["config", "workspaces"])
        matches = [item for item in config["workspaces"] if item["alias"] == self.workspace]
        if len(matches) != 1 or not matches[0].get("workspace_id"):
            raise ValueError("choose one enrolled workspace alias")
        item = matches[0]
        return {"alias": self.workspace, "workspace_id": item["workspace_id"], "base_url": item["base_url"], "actor": self.actor}

    def receipts(self, work, now):
        rows = []
        seen = set()
        cursors = set()
        cursor = ""
        while True:
            params = {"type": "run_recorded", "limit": 50}
            if cursor:
                params["cursor"] = cursor
            # listEvents has no subject_ref query parameter. Filter the public
            # event field after paging; public work has no backing thread ID.
            page = self.api("listEvents", params)
            for event in page.get("events", []):
                if event.get("subject_ref") != work["ref"]:
                    continue
                # Match canonical loop lifecycle filtering.
                if event.get("archived_at") or event.get("trashed_at"):
                    continue
                run = event.get("payload", {})["run_id"]
                at = stamp(event["ts"])
                if at > now:
                    continue
                if run in seen:
                    raise ValueError("duplicate receipt run identity")
                seen.add(run)
                rows.append({"event_id": event["id"], "run_id": run, "at": at.isoformat()})
                if len(rows) > 50:
                    break
            if len(rows) > 50 or not page.get("page_info", {}).get("has_more"):
                break
            cursor = page["page_info"].get("next_cursor", "")
            if not cursor or cursor in cursors:
                raise ValueError("event cursor missing or repeated; no complete sample was read")
            cursors.add(cursor)
        truncated = len(rows) > 50
        rows = rows[:50]
        rows.sort(key=lambda row: (row["at"], row["event_id"]))
        return rows, truncated

    def report(self, now):
        report = {"schema": 1, "mode": "dry_run", "algorithm": ALGORITHM, "workspace": self.binding(), "generated_at": now.isoformat(), "cards": []}
        cursor = ""
        seen = set()
        while True:
            params = {"limit": 50}
            if cursor:
                params["cursor"] = cursor
            page = self.api("workList", params)
            for item in page.get("work", []):
                if item.get("phase") in ("done", "cancelled") or item.get("archived_at") or item.get("trashed_at"):
                    continue
                work = self.api("workGet", {"card_ref": item["ref"]})["work"]
                entry = {"ref": work["ref"], "title": work["title"], "owner": work.get("owner"), "version": work["version"], "action": "owner_review"}
                if work.get("cadence"):
                    entry.update(action="already_tracked", reason="cadence_present")
                else:
                    receipts, truncated = self.receipts(work, now)
                    cadence, reason = infer(receipts)
                    entry.update(receipts=receipts, receipts_sha256=fingerprint(receipts), sample_truncated=truncated, reason=reason)
                    if cadence:
                        entry.update(action="infer", every_seconds=cadence)
                report["cards"].append(entry)
            cursor = page.get("next_cursor", "")
            if not cursor:
                return report
            if cursor in seen:
                raise ValueError("work cursor repeated; no complete report was written")
            seen.add(cursor)

    def apply(self, report, journal_path, now):
        if report.get("schema") != 1 or report.get("mode") != "dry_run" or report.get("algorithm") != ALGORITHM:
            raise ValueError("a complete v1 dry-run report is required")
        if report.get("workspace") != self.binding():
            raise ValueError("workspace or principal changed since the dry run")
        report_hash = fingerprint(report)
        path = Path(journal_path)
        journal = json.loads(path.read_text()) if path.exists() else {"report_sha256": report_hash, "cards": {}}
        if journal.get("report_sha256") != report_hash:
            raise ValueError("apply journal belongs to another report")
        for entry in report["cards"]:
            if entry["action"] != "infer":
                continue
            ref = entry["ref"]
            work = self.api("workGet", {"card_ref": ref})["work"]
            if work["ref"] != ref:
                raise ValueError(f"{ref}: identity changed; make a new dry-run report")
            if work.get("cadence"):
                status = "matching_cadence_observed" if work["cadence"].get("every_seconds") == entry["every_seconds"] else "conflict"
                journal["cards"][ref] = {"status": status}
                atomic_json(path, journal)
                continue
            receipts, _ = self.receipts(work, now)
            every, _ = infer(receipts)
            if work["version"] != entry["version"] or fingerprint(receipts) != entry["receipts_sha256"] or every != entry["every_seconds"]:
                journal["cards"][ref] = {"status": "conflict"}
                atomic_json(path, journal)
                continue
            request = {"if_version": entry["version"], "patch": {"cadence": {"every_seconds": every}}}
            journal["cards"][ref] = {"status": "pending", "request": request}
            atomic_json(path, journal)  # Persistence failure prevents the write.
            try:
                self.api("workPatch", {"card_ref": ref}, request)
            except Exception:
                journal["cards"][ref]["status"] = "uncertain"
                atomic_json(path, journal)
                raise
            journal["cards"][ref]["status"] = "applied"
            atomic_json(path, journal)
        return journal


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workspace", required=True, help="Enrolled workspace alias")
    parser.add_argument("--as", dest="actor", required=True, help="Agent principal")
    parser.add_argument("--report", required=True, type=Path, help="New dry-run report, or the saved report to apply")
    parser.add_argument("--apply", action="store_true", help="Apply regular cadences from the saved report; never guesses the rest")
    args = parser.parse_args()
    migration = Migration(args.workspace, args.actor)
    now = datetime.now(timezone.utc)
    if args.apply:
        result = migration.apply(json.loads(args.report.read_text()), str(args.report) + ".apply.json", now)
        print(json.dumps(result, sort_keys=True))
    else:
        result = migration.report(now)
        atomic_json(args.report, result, exclusive=True)
        print(json.dumps({"report": str(args.report), "cards": len(result["cards"]), "infer": sum(row["action"] == "infer" for row in result["cards"]), "owner_review": [row for row in result["cards"] if row["action"] == "owner_review"]}, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, RuntimeError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
