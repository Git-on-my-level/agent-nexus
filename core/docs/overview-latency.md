# Overview and Inbox latency

The largest browser delay was competing shell reads. The Inbox badge started
five collection requests before the route mounted; Inbox then loaded the same
collections again. Agent and access badges added more reads. Primary page reads
now start immediately after shell authentication; badge subscriptions start
1.5 seconds later. Inbox claims and publishes its own count, preventing the
background collection reads entirely while it is mounted. Inbox no longer
revalidates `/auth/session` after the shell has already authenticated it.

Scoped SQL also repeatedly prepared recursive authorization arms for live
handles, revision handles, aliases and tombstones. Those lookups now use the
atomically maintained identity index. Statement-level epoch validation,
canonical fallback, transactional authorization and legacy empty-handle
inheritance remain intact. No result cache or visibility decision was added.

## Measurements

Local production UI, Chromium, loopback core, synthetic data. The server was
warmed once before navigation; the first browser had an empty HTTP cache.
Browser readiness is a MutationObserver mark when primary content renders,
independent of the test runner's polling delay. Three hard navigations per page
are descriptive samples, not a p95 estimate. Initial process/module startup is
excluded and can exceed 500 ms.

| Browser, 10 cards               |     Before |      After |
| ------------------------------- | ---------: | ---------: |
| Overview ready, median          |     671 ms |     159 ms |
| Overview ready, sample range    | 655–791 ms | 159–232 ms |
| Inbox ready, median             |     752 ms |     413 ms |
| Inbox ready, sample range       | 751–840 ms | 408–446 ms |
| Overview initial fetch requests |         17 |          8 |
| Inbox initial fetch requests    |         23 |         13 |
| First Overview contentful paint |      56 ms |      36 ms |
| First Inbox contentful paint    |      44 ms |      36 ms |

Overview's main request was 590 ms in the second baseline navigation and 110 ms
afterwards. Inbox's work request fell from 578 ms (plus a duplicate 395 ms read)
to one 367 ms read. Its six source collections still run in parallel; cursors
within a source remain sequential. Deferred badge reads are outside the initial
request counts, rather than removed from other pages altogether.

Authenticated full-handler measurements use 10 warm samples for small/medium
fixtures and 25 for the scale fixture. SQL instrumentation includes row
consumption; phase timings and SQL timings overlap and must not be added.

| Core request p95                          | Before |  After |
| ----------------------------------------- | -----: | -----: |
| 10 cards: `/overview`                     |  95 ms |  79 ms |
| 10 cards: `/inbox`                        |  31 ms |  26 ms |
| 10 cards: `/inbox/summary`                |   8 ms |   7 ms |
| 49 cards, 23 principals: `/overview`      | 104 ms |  83 ms |
| 49 cards, 23 principals: `/inbox`         |  34 ms |  26 ms |
| 49 cards, 23 principals: `/inbox/summary` |   9 ms |   7 ms |
| 4,096-card scale fixture: `/overview`     | 203 ms | 170 ms |
| Scale fixture: `/inbox`                   |  38 ms |  28 ms |
| Scale fixture: `/inbox/summary`           |  22 ms |  21 ms |

The medium fixture matches live card/principal cardinalities, with synthetic
content. It does not reproduce an entire historical workspace. The larger
existing scale fixture adds 10 private boards, 10,001 events, thousands of
documents/artifacts/PM records and a populated bounded decision window.

One representative warm medium Overview request:

| Phase                                |   Before |    After |
| ------------------------------------ | -------: | -------: |
| Authentication/selectors             |  0.06 ms |  0.06 ms |
| Authorization snapshot SQL           |  1.69 ms |  1.40 ms |
| Other SQL, including row consumption | 88.71 ms | 74.14 ms |
| Projection/enrichment                | 47.65 ms | 40.21 ms |
| Changes                              |  6.87 ms |  5.18 ms |
| Inbox enrichment                     |  6.66 ms |  5.76 ms |
| PM decisions                         |  5.36 ms |  4.62 ms |
| Agent roster                         | 28.61 ms | 24.32 ms |
| Visit validation/write               |  1.10 ms |  1.08 ms |
| Serialization                        |  0.62 ms |  0.66 ms |
| Response bytes                       |   91,429 |   91,419 |
| SQL statements / returned rows       | 22 / 131 | 22 / 131 |

Serialization and payload size were minor contributors. Overview remains
`no-store`: it includes principal-specific authority and records a visit.

## Reproduce and separate proxy cost

From `core/`:

```sh
go test ./internal/server -run 'TestOverview(Small|PersonalSized)WorkspaceLatency' -count=1 -v
go test ./internal/server -run '^TestResourceAccessCommonReadPerformance$//(overview|inbox)$' -count=1 -v
```

From the repository root, using an isolated local core:

```sh
ANX_MEASURE_LATENCY=1 PLAYWRIGHT_PREVIEW=1 \
  PLAYWRIGHT_PORT=4283 PLAYWRIGHT_CORE_PORT=8283 \
  pnpm -C web-ui exec playwright test workspace-latency.spec.js --project=default --workers=1
```

The browser measurement attaches its JSON waterfall. It performs synthetic
seeding and must only run against a disposable development workspace.

`GET /overview`, `/inbox` and `/inbox/summary` expose an inclusive `core` duration
and authentication/serialization through `Server-Timing`; Overview also exposes
its assembly phases. The hosted UI proxy preserves those values and appends
inclusive `bff` time through upstream headers. `bff - core` estimates the combined
proxy/network portion; it does not individually isolate the control-plane hop.
The local Overview sample had 109.9 ms browser duration versus 106.0 ms core time.
That approximately 3.9 ms remainder is a local proxy/network measurement only.

The hosted proxy split and hosted browser target still need deployment-time
verification. Read-only hosted CLI samples against the prior deployment were
1.90–2.38 s for Overview and 0.74–0.84 s for Inbox; these include CLI overhead and
cannot be presented as HTTP-only or post-change measurements. Direct unauthenticated
hosted probes returned 403; no hosted runtime, configuration or projection changes were made.
