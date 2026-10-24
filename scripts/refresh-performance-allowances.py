"""Re-pin existing route allowances from executed advisory route reports.

Never widens: each refreshed ceiling is the measurement plus the documented
headroom (core/docs/performance.md), capped at the reviewed ceiling it
replaces. An entry whose measurement exceeds its reviewed ceiling stays
expired and is reported; an entry the route's ordinary budget already covers
is retired. New allowances are never added.

An entry is renewed or retired only from complete, valid measurements: every
sample of every phase that phase's ceiling rests on, the response status the
route contract expects, no exceeded request deadline, the case's fixture and
stream assertions completed (the harness records a sample before asserting on
it, so a failed case keeps its samples without its completion marker), the
fresh-data stream controls measured, and a real invalidation for the
post-invalidation phase. Anything less refuses that entry with the reason.

Usage, from the repository root, after downloading every route shard artifact
of one run at the current source:
  python3 scripts/refresh-performance-allowances.py .tmp/performance/shards \
      --write --summary .tmp/performance/allowances.md
"""

import argparse
import importlib.util
import json
import math
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DATA = ROOT / "core/internal/server/testdata"
spec = importlib.util.spec_from_file_location(
    "coverage", ROOT / "scripts/check-performance-shards.py"
)
coverage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(coverage)

COUNTS = (
    # metric, limit field, minimum headroom
    ("queries", "max_queries", 8),
    ("rows", "max_rows", 64),
    ("vm_steps", "max_vm_steps", 1000),
)


def count_ceiling(measured, minimum):
    return measured + max(minimum, math.ceil(measured * 0.02))


def latency_ceiling(needed_ms):
    return int(math.ceil(needed_ms * 1.2 / 50) * 50)


# The executed samples an entry's ceiling rests on, per limit phase: the warm
# median needs all five warm samples, first-read limits cover both independent
# first_read and post_preparation captures, and post-invalidation needs its
# single invalidated read.
REQUIRED_PHASES = {
    "warm": {"warm": frozenset(range(1, 6))},
    "first_read": {"first_read": frozenset({0}), "post_preparation": frozenset({0})},
    "post_invalidation": {"post_invalidation": frozenset({6})},
}


def completion_label(sample):
    stage = (
        " post-invalidation"
        if sample["worker"] == 3
        else " first-and-warm"
        if sample["worker"] != 0
        else ""
    )
    return coverage.case_key(sample) + " " + sample["principal"] + stage


def sample_problem(route, sample, completed):
    """Why this sample cannot support a ceiling; None when it is valid."""
    phase = sample["cache_phase"]
    expected = route[sample["principal"] + "_status"]
    if not sample.get("measured"):
        return "unmeasured sample"
    if sample.get("deadline_exceeded"):
        return "request exceeded its deadline"
    if sample["status"] != expected:
        return f"status {sample['status']} (route expects {expected})"
    if completion_label(sample) not in completed:
        return "case failed its fixture/stream assertions"
    if route["path"].startswith("/stream/") and expected == 200:
        if sample.get("stream_polls") != 2:
            return (
                "missing fresh-data control "
                f"({sample.get('stream_polls')} of 2 measured polls)"
            )
    before = sample["invalidation_epoch_before"]
    after = sample["invalidation_epoch_after"]
    if phase == "post_invalidation":
        if not after > before > 0:
            return "invalidation epoch did not advance"
        if route["path"] == "/stream/agent-notification-receipts" and not (
            sample["receipt_invalidation_epoch_after"]
            > sample["receipt_invalidation_epoch_before"]
            > 0
        ):
            return "receipt invalidation epoch did not advance"
    elif (before, after) != (0, 0):
        return "unexpected invalidation"
    return None


def evidence_problems(route, phase, samples, completed):
    """Join every reason these samples cannot renew or retire the entry."""
    problems = []
    by_phase = {}
    for sample in samples:
        by_phase.setdefault(sample["cache_phase"], []).append(sample)
    for raw_phase, indexes in REQUIRED_PHASES[phase].items():
        have = by_phase.get(raw_phase, [])
        missing = indexes - {s["sample"] for s in have}
        if missing:
            problems.append(f"missing {raw_phase} sample(s) {sorted(missing)}")
        for sample in have:
            problem = sample_problem(route, sample, completed)
            if problem:
                problems.append(f"{raw_phase}#{sample['sample']}: {problem}")
    return "; ".join(dict.fromkeys(problems))


def needed_latency(phase, samples):
    """Smallest latency_ms the harness would accept for these samples."""
    elapsed = sorted(s["elapsed_ms"] for s in samples)
    # The secondary maximum is max(5s, 4x latency_ms) for every phase.
    needed = elapsed[-1] / 4 if elapsed[-1] > 5000 else 0
    if phase == "warm":
        needed = max(needed, elapsed[len(elapsed) // 2])
    return needed


def refresh(routes, allowances, reports, source_hash):
    by_key = {coverage.case_key(route): route for route in routes}
    measured, completed = {}, set()
    for report in reports:
        completed.update(report.get("completed") or [])
        for sample in report["samples"]:
            key = (coverage.case_key(sample), sample["principal"])
            measured.setdefault(key, []).append(sample)
    kept, rows = [], []
    for entry in allowances:
        phase = entry.get("cache_phase", "warm")
        key = (coverage.case_key(entry), entry["principal"])
        samples = [
            s
            for s in measured.get(key, [])
            if coverage.limit_phase(s["cache_phase"]) == phase
        ]
        route = by_key[coverage.case_key(entry)]
        label = " ".join((*key, phase))
        if not samples:
            kept.append(entry)
            rows.append((label, "not measured", entry, None, None))
            continue
        problems = evidence_problems(route, phase, samples, completed)
        if problems:
            # Incomplete or failed evidence must neither renew nor retire;
            # the entry stays expired at its last reviewed ceiling.
            kept.append(entry)
            rows.append((label, "invalid evidence: " + problems, entry, None, None))
            continue
        updated = dict(entry, core_source_sha256=source_hash)
        over, ordinary = [], True
        for metric, field, minimum in COUNTS:
            value = max(s[metric] for s in samples)
            if value > entry[field]:
                over.append(f"{metric} {value} > {entry[field]}")
            ceiling = max(route[field], count_ceiling(value, minimum))
            updated[field] = min(entry[field], ceiling)
            ordinary &= value <= route[field]
        latency = needed_latency(phase, samples)
        if latency > entry["latency_ms"]:
            over.append(f"latency {latency:.0f}ms > {entry['latency_ms']}ms")
        ceiling = max(route["latency_ms"], latency_ceiling(latency))
        updated["latency_ms"] = min(entry["latency_ms"], ceiling)
        ordinary &= latency <= route["latency_ms"]
        if over:
            kept.append(entry)  # stays expired; remeasure the linked P1
            outcome = "over reviewed ceiling: " + "; ".join(over)
            rows.append((label, outcome, entry, None, samples))
        elif ordinary:
            outcome = "retired (ordinary budget covers it)"
            rows.append((label, outcome, entry, None, samples))
        else:
            kept.append(updated)
            rows.append((label, "refreshed", entry, updated, samples))
    return kept, rows


def summary(rows):
    lines = [
        "| Case / principal / phase | Outcome | Measured SQL / rows / VM / ms "
        "| Ceiling before | Ceiling after |",
        "|---|---|---|---|---|",
    ]

    def ceiling(e):
        if e is None:
            return "—"
        fields = ("max_queries", "max_rows", "max_vm_steps", "latency_ms")
        return " / ".join(str(e[f]) for f in fields)

    for label, outcome, before, after, samples in rows:
        if samples:
            phase = label.rsplit(" ", 1)[1]
            value = (
                f"{max(s['queries'] for s in samples)} / "
                f"{max(s['rows'] for s in samples)} / "
                f"{max(s['vm_steps'] for s in samples)} / "
                f"{needed_latency(phase, samples):.0f}"
            )
        else:
            value = "—"
        cells = (label, outcome, value, ceiling(before), ceiling(after))
        lines.append("| " + " | ".join(cells) + " |")
    return "\n".join(lines) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("reports", type=Path)
    parser.add_argument("--write", action="store_true")
    parser.add_argument("--summary", type=Path)
    args = parser.parse_args()
    read = lambda name: json.loads((DATA / name).read_text())
    reports = [
        json.loads(path.read_text())
        for path in sorted(args.reports.rglob("route-report.json"))
    ]
    source_hash = coverage.source_hash(ROOT)
    stale = {r["core_source_sha256"] for r in reports} - {source_hash}
    assert reports and not stale, "reports must come from the current source"
    assert not any(r["diagnostic"] for r in reports), "diagnostic reports"
    kept, rows = refresh(
        read("performance_routes.json"),
        read("performance_budget_allowlist.json"),
        reports,
        source_hash,
    )
    text = summary(rows)
    if args.summary:
        args.summary.write_text(text)
    else:
        print(text)
    if args.write:
        target = DATA / "performance_budget_allowlist.json"
        target.write_text(json.dumps(kept, indent=2) + "\n")
    counts = {}
    for row in rows:
        outcome = row[1].split(":")[0]
        counts[outcome] = counts.get(outcome, 0) + 1
    print(json.dumps(counts, sort_keys=True))


if __name__ == "__main__":
    main()
