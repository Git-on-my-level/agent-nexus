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
from project import ADAPTER_VERSION, operator_name
from initiatives import validate_mapping, source_items, plan_ingestion, apply_ingestion, add_unsorted_panel
from readers.agentctl import read_agentctl
from readers.fleetctl import read_fleetctl
from readers.github import read_github
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


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Project fleet sources into Agent Nexus and publish the fleet dashboard.")
    parser.add_argument("--config", default=str(DEFAULT_CONFIG))
    parser.add_argument("--state", default=str(DEFAULT_STATE))
    parser.add_argument("--dry-run", "--plan", dest="dry_run", action="store_true")
    parser.add_argument("--mapping-file", help="draft JSON mapping; preview only")
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
    client = AnxClient(config.get("anx_binary") or "anx", config["base_url"], config["agent"], runner=runner)
    try:
        if args.mapping_file:
            if not args.dry_run:
                raise ValueError("--mapping-file is preview only; publish reviewed rules to mapping_doc before applying")
            mapping = json.loads(Path(args.mapping_file).read_text(encoding="utf-8"))
        else:
            ref = config.get("mapping_doc")
            if not isinstance(ref, str) or not ref.startswith("doc:"):
                raise ValueError("config requires mapping_doc: a workspace-local doc:<handle>; legacy per-item creation is disabled")
            mapping = json.loads(client.docs_content(ref))
        validate_mapping(mapping, config["base_url"])
        reads = collect(config, selected, now, runner)
        plans = plan_ingestion(source_items(reads, config, now), mapping, client)
    except (AnxError, ValueError, OSError) as exc:
        print(str(exc), file=sys.stderr)
        return 1
    state = load_state(args.state)
    if state.get("workspace") != config["base_url"].rstrip("/"):
        state = {}
    state["workspace"] = config["base_url"].rstrip("/")
    generated_at = now.strftime("%Y-%m-%dT%H:%M:%SZ")
    snapshot = headline_snapshot({read["name"]: read for read in reads}, now)
    shown_history = record_history(list(state.get("history") or []), at=generated_at, metrics=snapshot)
    report = build_report(
        reads, generated_at=generated_at, now=now, hosts=config.get("hosts") or [], history=shown_history,
        operator=operator_name(config),
    )
    add_unsorted_panel(report, plans, reads)
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
            "mapping": mapping,
            "planned_writes": plans,
            "report_valid": valid,
            "report_diagnostics": diagnostics,
            "report": report,
        }, sys.stdout, indent=2)
        sys.stdout.write("\n")
        return 0 if valid and not _failure_lines(reads) else 1
    if not valid:
        if args.quiet:
            print("report failed validation: " + "; ".join(diagnostics[:4]), file=sys.stderr)
        else:
            print(json.dumps({"valid": False, "errors": diagnostics}), file=sys.stderr)
        return 1
    assert client is not None
    state["history"] = shown_history
    summary = apply_ingestion(client, plans)
    summary["readers"] = [_reader_summary(read) for read in reads]
    try:
        summary.update(publish(client, config, state, report, node_bin, validator))
    except (AnxError, OSError, ValueError) as exc:
        summary["errors"].append(str(exc)[:300])
        save_state(args.state, state)
        return _finish(summary, reads, quiet=args.quiet)
    save_state(args.state, state)
    return _finish(summary, reads, quiet=args.quiet)


def collect(config: dict, selected: list[str], now: datetime, runner: Runner | None = None) -> list[dict]:
    host_exec = HostExec(runner)
    reads = []
    for name in selected:
        reads.append(READERS[name](host_exec, config, now=now))
    return reads


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
    return 1 if errors or failures else 0


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
    for key in ("base_url", "agent", "topic"):
        if not data.get(key):
            raise ValueError(f"config is missing {key}")
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
        return {}
    try:
        data = json.loads(file.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    return data if isinstance(data, dict) else {"version": 1, "cards": []}


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


def save_state(path: str, state: dict) -> None:
    file = Path(path).expanduser()
    file.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile("w", dir=file.parent, delete=False) as handle:
        json.dump(state, handle, indent=2)
        temp = handle.name
    os.replace(temp, file)


if __name__ == "__main__":
    raise SystemExit(main())
