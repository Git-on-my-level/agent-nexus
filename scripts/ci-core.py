#!/usr/bin/env python3
"""Discover core tests and run a disjoint, exhaustive CI partition.

Hash top-level names, including examples/fuzz seeds, so new tests cannot escape
CI. go test still runs every subtest. No Go test source or local targets change.
"""
import argparse
import hashlib
import json
import re
import subprocess


# Explicit measured fixtures only. New tests default to the correctness gate,
# even when their names mention performance or latency.
MEASURED = (
    'TestPerformanceRoutes',
    'TestPerformancePMPresence',
    'TestPerformanceColdStartReads',
    'TestPerformanceWorkSummaryBudgetAndPlans',
    'TestPerformanceInboxAskStalenessBudgetAndPlans',
    'TestResourceAccessCommonReadPerformance',
    'TestResourceAccessLargeDenialPrepareAndPMRoutes',
    'TestPerformanceStartupAndMigrations',
    'TestOverviewSmallWorkspaceLatency',
    'TestOverviewPersonalSizedWorkspaceLatency',
    'TestOverviewDenseAccessWorkspaceLatency',
    'TestEventsStreamPublicProbeLatencyDoesNotWaitForHiddenPollTicks',
)
ADVISORY = '^(' + '|'.join(MEASURED) + ')$'


def other_packages(packages):
    return [p for p in packages if not p.endswith('/internal/server')
            and not re.search(r'/internal/primitives(/|$)', p)]


def shard_names(names, index, count):
    return sorted(name for name in names
                  if int.from_bytes(hashlib.sha256(name.encode()).digest()[:8], 'big') % count == index - 1)


def discover(package):
    result = subprocess.run(['go', 'test', '-list', '.', package], check=True,
                            capture_output=True, text=True)
    return sorted(set(line for line in result.stdout.splitlines()
                      if re.fullmatch(r'(Test|Example|Fuzz)\w*', line)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('partition', choices=['server', 'primitives', 'other', 'advisory'])
    parser.add_argument('--shard', type=int, default=1)
    parser.add_argument('--count', type=int, default=3)
    parser.add_argument('--plan', action='store_true')
    args = parser.parse_args()
    if not 1 <= args.shard <= args.count:
        parser.error('shard must be within count')
    if args.partition == 'server':
        names = [name for name in discover('./internal/server') if not re.search(ADVISORY, name)]
        if not names:
            raise RuntimeError('server discovery returned no tests')
        plan = [shard_names(names, index, args.count) for index in range(1, args.count + 1)]
        if any(not shard for shard in plan):
            raise RuntimeError('empty server shard')
        print(json.dumps({'counts': [len(shard) for shard in plan], 'names': plan}, indent=2), flush=True)
        command = ['go', 'test', '-parallel=2', '-timeout=25m', './internal/server',
                   '-run', '^(' + '|'.join(plan[args.shard - 1]) + ')$']
    elif args.partition == 'advisory':
        # Route scale fixtures retain their dedicated measured/sharded lane.
        command = ['go', 'test', '-parallel=2', '-timeout=25m', './...',
                   '-run', '^Test.*(Performance|Latency)|^TestResourceAccess(CommonReadPerformance|LargeDenialPrepareAndPMRoutes)$',
                   '-skip', '^TestPerformanceRoutes$']
    elif args.partition == 'primitives':
        command = ['go', 'test', '-parallel=2', '-timeout=25m', './internal/primitives/...', '-skip', ADVISORY]
    else:
        packages = subprocess.check_output(['go', 'list', './...'], text=True).splitlines()
        packages = other_packages(packages)
        command = ['go', 'test', '-parallel=2', '-timeout=25m', *packages, '-skip', ADVISORY]
    print(' '.join(command), flush=True)
    if not args.plan:
        subprocess.run(command, check=True)


if __name__ == '__main__':
    main()
