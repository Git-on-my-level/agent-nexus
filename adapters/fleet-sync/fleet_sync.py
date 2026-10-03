"""Read-only fleet projector. Sources are listed; only Agent Nexus is written."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

from anx_client import AnxClient, AnxError
from project import ADAPTER_VERSION, plan_reads
from readers.agentctl import read_agentctl
from readers.fleetctl import read_fleetctl
from readers.github import read_github
from readers.hermes import read_hermes
from readers.multica import read_multica
from readers.run import HostExec
from report import build_report, changed_besides_generated_at

READERS = {
    "multica": read_multica,
    "github": read_github,
    "hermes": read_hermes,
    "agentctl": read_agentctl,
    "fleetctl": read_fleetctl,
}
DEFAULT_CONFIG = Path.home() / ".config" / "anx-fleet-sync" / "config.json"
DEFAULT_STATE = Path.home() / ".local" / "state" / "anx-fleet-sync" / "state.json"
DASHBOARD_TITLE = "Fleet Dashboard"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Project fleet sources into Agent Nexus and publish the fleet dashboard.")
    parser.add_argument("--config", default=str(DEFAULT_CONFIG))
    parser.add_argument("--state", default=str(DEFAULT_STATE))
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--only", default="")
    args = parser.parse_args(argv)
    try:
        config = load_config(args.config)
        selected = parse_only(args.only)
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    now = datetime.now(timezone.utc).replace(microsecond=0)
    reads = collect(config, selected, now)
    state = {} if args.dry_run else load_state(args.state)
    known = cards_from_state(state)
    if not args.dry_run:
        client = AnxClient(config.get("anx_binary") or "anx", config["base_url"], config["agent"])
        merge_known(client, known, selected)
    else:
        client = None
    plans = plan_reads(reads, known, config, now=now)
    generated_at = now.strftime("%Y-%m-%dT%H:%M:%SZ")
    report = build_report(reads, generated_at=generated_at, now=now, hosts=config.get("hosts") or [])
    valid, diagnostics = validate_report(report)
    if args.dry_run:
        json.dump({
            "dry_run": True,
            "adapter_version": ADAPTER_VERSION,
            "readers": [_reader_summary(read) for read in reads],
            "planned_writes": [_public_plan(plan) for plan in plans],
            "report_valid": valid,
            "report_diagnostics": diagnostics,
            "report": report,
        }, sys.stdout, indent=2)
        sys.stdout.write("\n")
        return 0 if valid else 1
    if not valid:
        print(json.dumps({"valid": False, "errors": diagnostics}), file=sys.stderr)
        return 1
    assert client is not None
    summary = apply_plans(client, plans, known)
    summary["readers"] = [_reader_summary(read) for read in reads]
    try:
        summary.update(publish(client, config, state, report))
    except (AnxError, OSError, ValueError) as exc:
        summary["errors"].append(str(exc)[:300])
        save_state(args.state, state, known)
        json.dump(summary, sys.stdout, indent=2)
        sys.stdout.write("\n")
        return 1
    save_state(args.state, state, known)
    json.dump(summary, sys.stdout, indent=2)
    sys.stdout.write("\n")
    return 1 if summary["errors"] else 0


def collect(config: dict, selected: list[str], now: datetime) -> list[dict]:
    host_exec = HostExec()
    reads = []
    for name in selected:
        reads.append(READERS[name](host_exec, config, now=now))
    return reads


def apply_plans(client: AnxClient, plans: list[dict], known: dict) -> dict:
    summary = {"created": 0, "observations": 0, "skipped": 0, "closed": 0, "conflicts": 0, "errors": []}
    for plan in plans:
        if plan["action"] == "skip":
            summary["skipped"] += 1
            continue
        key = (plan["authority"], plan["connection_id"], plan["native_id"])
        ref = None
        try:
            ref = plan.get("card_ref") or (known.get(key) or {}).get("ref")
            if plan["create"]:
                created = client.work_create(_create_body(plan))
                ref = ((created.get("work") or {}).get("ref")) or ref
                summary["created"] += 1
            if not ref:
                raise AnxError("not_found", f"no card ref for {plan['native_id']}")
            client.observe(ref, _observation_body(plan))
            summary["observations"] += 1
            if plan.get("reason") in {"absent", "cleared"} or plan["facts"]["phase"] in {"done", "cancelled"}:
                summary["closed"] += 1
            known[key] = {
                "ref": ref,
                "digest": plan["digest"],
                "title": plan["title"],
                "owner": plan["owner"],
            }
        except AnxError as exc:
            if exc.code == "conflict":
                summary["conflicts"] += 1
                if ref:
                    known[key] = {"ref": ref, "digest": plan["digest"], "title": plan["title"], "owner": plan["owner"]}
                continue
            summary["errors"].append(f"{plan['authority']} {plan['native_id']}: {exc.message}")
    return summary


def publish(client: AnxClient, config: dict, state: dict, report: dict) -> dict:
    ref = state.get("dashboard_ref") or find_dashboard(client.docs_list())
    with tempfile.TemporaryDirectory() as directory:
        path = str(Path(directory) / "fleet-dashboard.json")
        Path(path).write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        revised = False
        if not ref:
            created = client.docs_create(_topic(config), DASHBOARD_TITLE, path)
            document = created.get("document") or {}
            ref = document.get("ref")
            if not ref:
                raise AnxError("invalid_response", "docs create did not return a document ref")
            revised = True
        else:
            previous = client.docs_content(ref)
            if changed_besides_generated_at(previous, report):
                client.docs_revise(ref, path)
                revised = True
        readback = client.docs_content(ref)
    valid, diagnostics = validate_text(readback)
    if not valid:
        raise ValueError("dashboard readback failed validation: " + "; ".join(diagnostics[:8]))
    state["dashboard_ref"] = ref
    return {"document_ref": ref, "report_revised": revised, "readback_valid": True}


def find_dashboard(documents: list[dict]) -> str | None:
    for document in documents:
        if document.get("title") == DASHBOARD_TITLE and document.get("ref"):
            return str(document["ref"])
    return None


def merge_known(client: AnxClient, known: dict, selected: list[str]) -> None:
    authorities = {
        "multica": ["multica"],
        "github": ["github"],
        "hermes": ["hermes-cron"],
        "agentctl": ["agentctl"],
        "fleetctl": ["fleetctl"],
    }
    wanted = {authority for name in selected for authority in authorities[name]}
    for authority in sorted(wanted):
        try:
            cards = client.work_list(authority)
        except AnxError as exc:
            print(f"work list {authority} failed: {exc.message}", file=sys.stderr)
            continue
        for card in cards:
            source = card.get("source") or {}
            native_id = source.get("native_id")
            connection_id = source.get("connection_id")
            if source.get("authority") != authority or not native_id or not connection_id:
                continue
            key = (authority, str(connection_id), str(native_id))
            current = known.get(key, {})
            current.setdefault("digest", None)
            current["ref"] = card.get("ref") or current.get("ref")
            current["title"] = card.get("title") or current.get("title")
            current["owner"] = card.get("owner") or current.get("owner")
            known[key] = current


def _create_body(plan: dict) -> dict:
    phase = plan["facts"]["phase"]
    if phase in {"done", "cancelled"}:
        phase = "backlog"
    source = {
        "authority": plan["authority"],
        "connection_id": plan["connection_id"],
        "native_id": plan["native_id"],
    }
    if plan.get("url"):
        source["url"] = plan["url"]
    return {
        "board_ref": plan["board_ref"],
        "title": plan["title"],
        "summary": plan["summary"],
        "owner": plan["owner"],
        "phase": phase,
        "source": source,
    }


def _observation_body(plan: dict) -> dict:
    return {"observation": {
        "idempotency_key": plan["digest"],
        "reader_id": plan["reader_id"],
        "reader_revision": ADAPTER_VERSION,
        "observed_at": plan.get("observed_at"),
        "status": "reported",
        "source_revision": plan["digest"],
        "facts": plan["facts"],
        "evidence": plan["evidence"],
    }}


def _public_plan(plan: dict) -> dict:
    return {
        "action": plan["action"],
        "reason": plan.get("reason"),
        "authority": plan["authority"],
        "connection_id": plan["connection_id"],
        "native_id": plan["native_id"],
        "title": plan["title"],
        "phase": plan["facts"]["phase"],
        "owner": plan["owner"],
    }


def _reader_summary(read: dict) -> dict:
    return {
        "source": read["name"],
        "ok": read.get("ok"),
        "complete": read.get("complete"),
        "items": len(read.get("items") or []),
        "error": read.get("error"),
        "observed_at": read.get("observed_at"),
    }


def load_config(path: str) -> dict:
    file = Path(path).expanduser()
    if not file.is_file():
        raise ValueError(f"config not found: {file}")
    data = json.loads(file.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError("config must be a JSON object")
    for key in ("base_url", "agent", "topic", "boards"):
        if not data.get(key):
            raise ValueError(f"config is missing {key}")
    boards = data["boards"]
    for key in ("agent_work", "pull_requests", "ops_hygiene"):
        if not isinstance(boards, dict) or not boards.get(key):
            raise ValueError(f"config is missing boards.{key}")
    return data


def parse_only(value: str) -> list[str]:
    if not value.strip():
        return list(READERS)
    names = [part.strip() for part in value.split(",") if part.strip()]
    unknown = [name for name in names if name not in READERS]
    if unknown:
        raise ValueError("unknown source: " + ", ".join(unknown))
    return names


def load_state(path: str) -> dict:
    file = Path(path).expanduser()
    if not file.is_file():
        return {"version": 1, "cards": []}
    try:
        data = json.loads(file.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {"version": 1, "cards": []}
    return data if isinstance(data, dict) else {"version": 1, "cards": []}


def cards_from_state(state: dict) -> dict:
    known = {}
    for card in state.get("cards") or []:
        if not isinstance(card, dict):
            continue
        authority, connection, native = card.get("authority"), card.get("connection_id"), card.get("native_id")
        if authority and connection and native:
            known[(authority, connection, native)] = {
                "ref": card.get("ref"),
                "digest": card.get("digest"),
                "title": card.get("title"),
                "owner": card.get("owner"),
            }
    return known


def save_state(path: str, state: dict, known: dict) -> None:
    file = Path(path).expanduser()
    file.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    payload = {
        "version": 1,
        "dashboard_ref": state.get("dashboard_ref"),
        "cards": [
            {"authority": key[0], "connection_id": key[1], "native_id": key[2], **value}
            for key, value in sorted(known.items())
        ],
    }
    temporary = file.with_suffix(".json.tmp")
    temporary.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    os.chmod(temporary, 0o600)
    temporary.replace(file)


def validate_report(report: dict) -> tuple[bool, list[str]]:
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as handle:
        json.dump(report, handle)
        path = handle.name
    try:
        return validate_text(Path(path).read_text(encoding="utf-8"), path)
    finally:
        os.unlink(path)


def validate_text(content: str, path: str | None = None) -> tuple[bool, list[str]]:
    repo = Path(__file__).resolve().parents[2]
    validator = repo / "web-ui" / "scripts" / "validate-visual-report.mjs"
    if path is None:
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as handle:
            handle.write(content)
            path = handle.name
            owned = True
    else:
        owned = False
    try:
        completed = subprocess.run(
            ["node", str(validator), path],
            capture_output=True, text=True, timeout=30, shell=False,
        )
    except FileNotFoundError:
        return False, ["node is not installed; the report was not validated"]
    except subprocess.TimeoutExpired:
        return False, ["visual report validation timed out"]
    finally:
        if owned:
            os.unlink(path)
    if completed.returncode == 0:
        return True, []
    try:
        payload = json.loads(completed.stderr or completed.stdout or "{}")
        errors = payload.get("errors") if isinstance(payload, dict) else None
        if isinstance(errors, list):
            return False, [str(error) for error in errors]
    except json.JSONDecodeError:
        pass
    return False, [(completed.stderr or completed.stdout or "report is invalid")[:500]]


def _topic(config: dict) -> str:
    topic = str(config["topic"])
    return topic if topic.startswith("topic:") else f"topic:{topic}"


if __name__ == "__main__":
    raise SystemExit(main())
