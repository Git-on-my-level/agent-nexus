"""Validate executed route artifacts, independently of Go test name filtering."""

import argparse
import hashlib
import json
import math
from pathlib import Path


def case_key(route):
    return route["method"] + " " + route["path"] + (
        " [" + route["case"] + "]" if route.get("case") else ""
    )


def assignments(routes, weights):
    expected = {case_key(route) for route in routes}
    keys = [weight["key"] for weight in weights]
    assert len(keys) == len(set(keys)) and set(keys) == expected, "stale shard weights"
    assert all(type(w["milliseconds"]) is int and w["milliseconds"] > 0 for w in weights)
    loads = [0] * 4
    result = {}
    for weight in sorted(weights, key=lambda w: (-w["milliseconds"], w["key"])):
        shard = min(range(4), key=lambda i: (loads[i], i))
        loads[shard] += weight["milliseconds"]
        result[weight["key"]] = shard + 1
    assert set(result.values()) == {1, 2, 3, 4}, "empty shard"
    return result


def source_hash(root):
    paths = []
    helpers = {
        "auth_integration_test.go", "stream_privacy_integration_test.go",
        "notifications_integration_test.go", "resource_access_performance_test.go",
        "resource_access_prepare_test.go",
    }
    for directory in ("core", "tests/channels", "contracts"):
        for path in (root / directory).rglob("*"):
            relative = path.relative_to(root)
            if not path.is_file() or any(p.startswith(".") for p in relative.parts):
                continue
            if relative.as_posix() in {
                "core/internal/buildinfo/version_generated.go",
                "core/internal/server/testdata/performance_budget_allowlist.json",
            }:
                continue
            if path.name.endswith("_test.go") and path.name not in helpers and not path.name.startswith("performance_"):
                continue
            if path.suffix in {".go", ".mod", ".sum", ".json", ".yaml", ".yml", ".sql", ".html", ".js", ".css", ".tmpl"}:
                paths.append(relative.as_posix())
    for relative in ("scripts/check-performance-shards.py", "scripts/tests/test_performance_shards.py", ".github/workflows/ci.yml"):
        if (root / relative).is_file():
            paths.append(relative)
    digest = hashlib.sha256()
    for relative in sorted(paths):
        digest.update(relative.encode() + b"\0")
        digest.update(hashlib.sha256((root / relative).read_bytes()).digest())
    return digest.hexdigest()


def validate(routes, weights, allowances, reports, expected_hash):
    assert len(routes) >= 109, "route inventory shrank below 109 cases"
    by_key = {case_key(route): route for route in routes}
    assert len(by_key) == len(routes), "duplicate inventory case"
    shards = assignments(routes, weights)
    exceptions = {}
    for allowance in allowances:
        phase = allowance.get("cache_phase", "warm")
        key = (case_key(allowance), allowance["principal"], phase)
        assert key not in exceptions, "duplicate allowance"
        assert allowance["core_source_sha256"] == expected_hash, "expired allowance"
        exceptions[key] = allowance
    assert len(reports) == 4, "exactly four reports required"
    seen_shards, seen_samples, seen_completed = set(), set(), set()
    metrics = ("elapsed_ms", "queries", "rows", "vm_steps", "fullscan_steps", "sorts", "autoindex_rows")
    summaries = []
    for report in reports:
        shard = report["shard"]
        assert type(shard) is int and shard in {1, 2, 3, 4} and shard not in seen_shards, "wrong/duplicate shard"
        seen_shards.add(shard)
        assert report["diagnostic"] is False, "diagnostic report cannot establish acceptance"
        assert report["shard_count"] == 4 and report["core_source_sha256"] == expected_hash, "stale report"
        durations = {}
        for sample in report["samples"]:
            key, principal, phase, index = case_key(sample), sample["principal"], sample["cache_phase"], sample["sample"]
            assert key in by_key and shards[key] == shard, "unknown/wrong-shard case"
            assert principal in {"authorized", "unauthorized"}, "unknown principal"
            assert type(index) is int and (phase, index) in {
                ("first_read", 0), ("post_invalidation", 6), *(('warm', i) for i in range(1, 6)),
            }, "incorrect cache phase/sample"
            identity = (key, principal, phase, index)
            assert identity not in seen_samples, "duplicate sample"
            seen_samples.add(identity)
            assert sample["measured"] is True and sample["sampling_policy"] == "one-first-read/five-warm/one-post-invalidation", "diagnostic/unmeasured sample"
            assert sample["fixture_policy"] == "fresh-pool-and-handler-per-case-principal/shared-4096-distinct-thread-corpus", "unisolated fixture"
            assert all(type(sample[m]) in {int, float} and math.isfinite(sample[m]) and sample[m] >= 0 for m in metrics), "invalid metrics"
            assert sample["deadline_exceeded"] is False, "request deadline"
            route = by_key[key]
            assert sample["status"] == route[principal + "_status"], "incorrect status"
            if route["path"].startswith("/stream/") and sample["status"] == 200:
                assert sample["stream_polls"] == 2, "missing stream poll"
            if phase == "post_invalidation":
                assert sample["invalidation_epoch_after"] > sample["invalidation_epoch_before"] > 0, "missing invalidation"
            else:
                assert sample["invalidation_epoch_before"] == sample["invalidation_epoch_after"] == 0, "unexpected invalidation"
            budget = exceptions.get((key, principal, phase), route)
            for metric, limit in (("queries", "max_queries"), ("rows", "max_rows"), ("vm_steps", "max_vm_steps")):
                assert sample[metric] <= budget[limit], "budget exceeded: " + str(identity) + " " + metric
            durations.setdefault((key, principal, phase), []).append(sample["elapsed_ms"])
        expected_completed = {key + " " + principal for key in by_key if shards[key] == shard for principal in ("authorized", "unauthorized")}
        completed = report["completed"]
        assert len(completed) == len(set(completed)) and set(completed) == expected_completed, "failed/omitted case completion"
        assert not seen_completed.intersection(completed), "overlapping shards"
        seen_completed.update(completed)
        for (key, principal, phase), values in durations.items():
            budget = exceptions.get((key, principal, phase), by_key[key])
            assert max(values) <= max(5000, budget["latency_ms"] * 4), "secondary maximum latency"
            if phase == "warm":
                assert sorted(values)[2] <= budget["latency_ms"], "warm median latency"
        summaries.append({"shard": shard, "cases": len(expected_completed) // 2, "requests": len(report["samples"]), "request_seconds": sum(s["elapsed_ms"] for s in report["samples"]) / 1000})
    expected = {(key, principal, phase, index) for key in by_key for principal in ("authorized", "unauthorized") for phase,index in [("first_read", 0), *(('warm', i) for i in range(1, 6)), ("post_invalidation", 6)]}
    assert seen_samples == expected, "executed shards omit case/principal/cache-state/sample coverage"
    return sorted(summaries, key=lambda s: s["shard"])


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("reports", type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    data = root / "core/internal/server/testdata"
    read = lambda name: json.loads((data / name).read_text())
    reports = [json.loads(path.read_text()) for path in sorted(args.reports.rglob("route-report.json"))]
    summaries = validate(read("performance_routes.json"), read("performance_shard_weights.json"), read("performance_budget_allowlist.json"), reports, source_hash(root))
    print(json.dumps({"cases": sum(s["cases"] for s in summaries), "shards": summaries}, indent=2))
