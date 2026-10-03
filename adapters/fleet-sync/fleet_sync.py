"""Read-only fleet projector. Sources are listed; only Agent Nexus is written."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timedelta, timezone
from pathlib import Path

from anx_client import AnxClient, AnxError
from project import ADAPTER_VERSION, operator_name, plan_reads
from readers.agentctl import read_agentctl
from readers.fleetctl import read_fleetctl
from readers.github import confirm_disappeared, read_github
from readers.hermes import read_hermes
from readers.multica import read_multica
from readers.nexus import read_nexus
from readers.prometheus import read_prometheus
from readers.run import Budget, BudgetRunner, HostExec, Runner
from report import build_report, changed_besides_generated_at, headline_snapshot, record_history

READERS = {
    "multica": read_multica,
    "github": read_github,
    "hermes": read_hermes,
    "agentctl": read_agentctl,
    "fleetctl": read_fleetctl,
    "prometheus": read_prometheus,
    "nexus": read_nexus,
}
DEFAULT_CONFIG = Path.home() / ".config" / "anx-fleet-sync" / "config.json"
DEFAULT_STATE = Path.home() / ".local" / "state" / "anx-fleet-sync" / "state.json"
DASHBOARD_TITLE = "Fleet Dashboard"
DONE_RETENTION = timedelta(days=30)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Project fleet sources into Agent Nexus and publish the fleet dashboard.")
    parser.add_argument("--config", default=str(DEFAULT_CONFIG))
    parser.add_argument("--state", default=str(DEFAULT_STATE))
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--only", default="")
    parser.add_argument("--quiet", action="store_true", help="print nothing on success; one line per reader failure")
    args = parser.parse_args(argv)
    try:
        config = load_config(args.config)
        selected = parse_only(args.only)
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    now = datetime.now(timezone.utc).replace(microsecond=0)
    runner = BudgetRunner(Runner(), Budget())
    reads = collect(config, selected, now, runner)
    state = load_state(args.state)
    known = {} if args.dry_run else cards_from_state(state)
    if not args.dry_run:
        client = AnxClient(config.get("anx_binary") or "anx", config["base_url"], config["agent"], runner=runner)
        prune_cards(known, now=now)
        merge_known(client, known, selected, now=now)
        prune_cards(known, now=now)
    else:
        client = None
    github_connection = str((config.get("github") or {}).get("connection_id") or "github.com")
    for read in reads:
        if read.get("name") == "github":
            confirm_disappeared(runner, read, known, connection_id=github_connection)
    plans = plan_reads(reads, known, config, now=now)
    generated_at = now.strftime("%Y-%m-%dT%H:%M:%SZ")
    snapshot = headline_snapshot({read["name"]: read for read in reads}, now)
    shown_history = record_history(list(state.get("history") or []), at=generated_at, metrics=snapshot)
    report = build_report(
        reads, generated_at=generated_at, now=now, hosts=config.get("hosts") or [], history=shown_history,
        operator=operator_name(config),
    )
    node_bin = node_binary(config)
    validator = validator_script(config)
    valid, diagnostics = validate_report(report, node_bin, validator)
    if args.dry_run:
        if args.quiet:
            return _quiet_status(reads, valid=valid, diagnostics=diagnostics, errors=[])
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
        if args.quiet:
            print("report failed validation: " + "; ".join(diagnostics[:4]), file=sys.stderr)
        else:
            print(json.dumps({"valid": False, "errors": diagnostics}), file=sys.stderr)
        return 1
    assert client is not None
    state["history"] = shown_history
    summary = apply_plans(client, plans, known, now=now)
    summary["readers"] = [_reader_summary(read) for read in reads]
    try:
        summary.update(publish(client, config, state, report, node_bin, validator))
    except (AnxError, OSError, ValueError) as exc:
        summary["errors"].append(str(exc)[:300])
        save_state(args.state, state, known)
        return _finish(summary, reads, quiet=args.quiet)
    save_state(args.state, state, known)
    return _finish(summary, reads, quiet=args.quiet)


def collect(config: dict, selected: list[str], now: datetime, runner: Runner | None = None) -> list[dict]:
    host_exec = HostExec(runner)
    reads = []
    for name in selected:
        reads.append(READERS[name](host_exec, config, now=now))
    return reads


def apply_plans(client: AnxClient, plans: list[dict], known: dict, *, now: datetime | None = None) -> dict:
    now = now or datetime.now(timezone.utc)
    summary = {"created": 0, "observations": 0, "skipped": 0, "closed": 0, "conflicts": 0, "errors": []}
    for plan in plans:
        if plan["action"] == "skip":
            summary["skipped"] += 1
            _note_phase(known, plan, now)
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
            _note_phase(known, plan, now)
        except AnxError as exc:
            if exc.code == "conflict" and ref and observation_already_recorded(client, ref, plan["digest"]):
                summary["already_recorded"] = summary.get("already_recorded", 0) + 1
                known[key] = {"ref": ref, "digest": plan["digest"], "title": plan["title"], "owner": plan["owner"]}
                _note_phase(known, plan, now)
                continue
            if exc.code == "conflict":
                summary["conflicts"] += 1
            summary["errors"].append(f"{plan['authority']} {plan['native_id']}: {exc.message}")
    if summary.get("already_recorded"):
        print(
            f"observation already recorded for {summary['already_recorded']} unchanged fact digest(s)",
            file=sys.stderr,
        )
    return summary


def observation_already_recorded(client: AnxClient, ref: str, digest: str) -> bool:
    """True when a stored observation already uses this facts digest as its idempotency key."""
    cursor = ""
    for _ in range(4):
        try:
            page = client.observations(ref, cursor=cursor)
        except AnxError:
            return False
        observations = page.get("observations") if isinstance(page, dict) else None
        if not isinstance(observations, list):
            return False
        for observation in observations:
            if isinstance(observation, dict) and observation.get("idempotency_key") == digest:
                return True
        cursor = str(page.get("next_cursor") or "")
        if not cursor:
            return False
    return False


def _finish(summary: dict, reads: list[dict], *, quiet: bool) -> int:
    failures = _failure_lines(reads)
    errors = list(summary.get("errors") or [])
    if quiet:
        for line in failures:
            print(line, file=sys.stderr)
        for err in errors:
            print(err, file=sys.stderr)
        return 1 if failures or errors else 0
    json.dump(summary, sys.stdout, indent=2)
    sys.stdout.write("\n")
    return 1 if errors else 0


def _quiet_status(reads: list[dict], *, valid: bool, diagnostics: list[str], errors: list[str]) -> int:
    if not valid:
        print("report failed validation: " + "; ".join(diagnostics[:4]), file=sys.stderr)
    for line in _failure_lines(reads):
        print(line, file=sys.stderr)
    for err in errors:
        print(err, file=sys.stderr)
    failed = (not valid) or bool(_failure_lines(reads)) or bool(errors)
    return 1 if failed else 0


def _failure_lines(reads: list[dict]) -> list[str]:
    return [f"{read['name']}: {read.get('error') or 'unavailable'}" for read in reads if not read.get("ok")]


def publish(client: AnxClient, config: dict, state: dict, report: dict, node_bin: str, validator: Path) -> dict:
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
    valid, diagnostics = validate_text(readback, node_bin=node_bin, validator=validator)
    if not valid:
        raise ValueError("dashboard readback failed validation: " + "; ".join(diagnostics[:8]))
    state["dashboard_ref"] = ref
    return {"document_ref": ref, "report_revised": revised, "readback_valid": True}


def find_dashboard(documents: list[dict]) -> str | None:
    for document in documents:
        if document.get("title") == DASHBOARD_TITLE and document.get("ref"):
            return str(document["ref"])
    return None


def merge_known(client: AnxClient, known: dict, selected: list[str], *, now: datetime | None = None) -> None:
    now = now or datetime.now(timezone.utc)
    authorities = {
        "multica": ["multica"],
        "github": ["github"],
        "hermes": ["hermes-cron"],
        "agentctl": ["agentctl"],
        "fleetctl": ["fleetctl"],
        "prometheus": ["prometheus"],
        "nexus": [],
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
            if key not in known and _stale_done(card, now):
                continue
            current = known.get(key, {})
            current.setdefault("digest", None)
            if str(card.get("phase") or "") == "done":
                stamp = _done_stamp(card)
                if stamp and _stamp_before(stamp, current.get("done_at")):
                    current["done_at"] = stamp
                    current["phase"] = "done"
            if not current.get("digest"):
                latest = card.get("latest_observation")
                if isinstance(latest, dict) and isinstance(latest.get("idempotency_key"), str) and latest["idempotency_key"]:
                    current["digest"] = latest["idempotency_key"]
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


def _note_phase(known: dict, plan: dict, now: datetime) -> None:
    key = (plan["authority"], plan["connection_id"], plan["native_id"])
    current = known.get(key)
    if current is None:
        return
    phase = plan["facts"]["phase"]
    current["phase"] = phase
    if phase == "done":
        current.setdefault("done_at", _format_stamp(now))
    else:
        current.pop("done_at", None)


def prune_cards(known: dict, *, now: datetime) -> None:
    """Drop cache entries for cards that have been done for more than 30 days."""
    stale = [key for key, card in known.items() if isinstance(card, dict) and _stale_done(card, now)]
    for key in stale:
        del known[key]


def _stale_done(card: dict, now: datetime) -> bool:
    if str(card.get("phase") or "") != "done":
        return False
    parsed = _parse_stamp(card.get("done_at")) or _parse_stamp(_done_stamp(card) or "")
    if parsed is None:
        return False
    return now - parsed > DONE_RETENTION


def _done_stamp(card: dict) -> str | None:
    if str(card.get("phase") or "") != "done":
        return None
    latest = card.get("latest_observation")
    if isinstance(latest, dict):
        stamp = _normalize_stamp(latest.get("observed_at"))
        if stamp:
            return stamp
    return _normalize_stamp(card.get("updated_at")) or _normalize_stamp(card.get("done_at"))


def _stamp_before(stamp: str, current: str | None) -> bool:
    parsed = _parse_stamp(stamp)
    if parsed is None:
        return False
    if not current:
        return True
    other = _parse_stamp(current)
    return other is None or parsed < other


def _state_card(key: tuple, value: dict) -> dict:
    entry = {
        "authority": key[0],
        "connection_id": key[1],
        "native_id": key[2],
        "ref": value.get("ref"),
        "digest": value.get("digest"),
        "title": value.get("title"),
        "owner": value.get("owner"),
    }
    if value.get("phase"):
        entry["phase"] = value["phase"]
    if value.get("done_at"):
        entry["done_at"] = value["done_at"]
    return entry


def _format_stamp(now: datetime) -> str:
    return now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _normalize_stamp(value) -> str | None:
    parsed = _parse_stamp(value)
    if parsed is None:
        return None
    return _format_stamp(parsed)


def _parse_stamp(value) -> datetime | None:
    if not isinstance(value, str) or not value:
        return None
    try:
        stamp = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if stamp.tzinfo is None:
        stamp = stamp.replace(tzinfo=timezone.utc)
    return stamp.astimezone(timezone.utc)


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
            entry = {
                "ref": card.get("ref"),
                "digest": card.get("digest"),
                "title": card.get("title"),
                "owner": card.get("owner"),
            }
            if isinstance(card.get("phase"), str):
                entry["phase"] = card["phase"]
            if isinstance(card.get("done_at"), str):
                entry["done_at"] = card["done_at"]
            known[(authority, connection, native)] = entry
    return known


def save_state(path: str, state: dict, known: dict, *, now: datetime | None = None) -> None:
    prune_cards(known, now=now or datetime.now(timezone.utc))
    file = Path(path).expanduser()
    file.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    payload = {
        "version": 1,
        "dashboard_ref": state.get("dashboard_ref"),
        "history": state.get("history") or [],
        "cards": [_state_card(key, value) for key, value in sorted(known.items())],
    }
    temporary = file.with_suffix(".json.tmp")
    temporary.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    os.chmod(temporary, 0o600)
    temporary.replace(file)


def node_binary(config: dict) -> str:
    return str(Path(str(config.get("node_bin") or "node")).expanduser())


def validator_script(config: dict | None = None) -> Path:
    checkout = Path(__file__).resolve().parents[2] / "web-ui" / "scripts" / "validate-visual-report.mjs"
    if checkout.is_file():
        return checkout
    root = str((config or {}).get("repo_root") or "")
    if root:
        return Path(root).expanduser() / "web-ui" / "scripts" / "validate-visual-report.mjs"
    return checkout


def validate_report(report: dict, node_bin: str = "node", validator: Path | None = None) -> tuple[bool, list[str]]:
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as handle:
        json.dump(report, handle)
        path = handle.name
    try:
        return validate_text(Path(path).read_text(encoding="utf-8"), path, node_bin=node_bin, validator=validator)
    finally:
        os.unlink(path)


def validate_text(content: str, path: str | None = None, node_bin: str = "node",
                  validator: Path | None = None) -> tuple[bool, list[str]]:
    validator = validator or validator_script()
    if path is None:
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as handle:
            handle.write(content)
            path = handle.name
            owned = True
    else:
        owned = False
    if not validator.is_file():
        if owned:
            os.unlink(path)
        return False, [f"visual report validator not found at {validator}"]
    try:
        completed = subprocess.run(
            [node_bin, str(validator), path],
            capture_output=True, text=True, timeout=30, shell=False,
        )
    except FileNotFoundError:
        return False, [f"{node_bin} is not installed; the report was not validated"]
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
