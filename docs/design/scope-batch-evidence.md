# Disabled batch admission evidence

The batch repository passes its bounded admission tests, but the request path
still fails the unchanged 500 ms gate. Readers remain disabled. These measurements
are diagnostic preparation probes, not production HTTP or parity acceptance.

## Fixture and limits

Measured 2026-10-07 on the local development host using the SCA-661 fixture and
capture implementation from [PR #295 at db826a45](https://github.com/Git-on-my-level/agent-nexus/tree/db826a45be74319a6cf5a6b03980f1f77f166ee4/core/internal/testutil/perfguard).
The base corpus has 4,096 rows per canonical family and 1,024 per PM kind. The
10x run scales those cardinalities to 40,960 and 10,240, preserving the fixture's
canonical ownership triggers. Both add 64 scopes, four exact inbox audiences per
scope and 101 rows per stream (25,856 shadow candidates), 100-item hydration and
four exact counter buckets. The proof is injected test data; it is not minted by
a production old-policy comparison.

Each sample includes the real scope directory, old-policy denial preparation
through a scoped canonical read, batch authority/proof admission, candidate
seeks, hydration, exact shadow counters and JSON serialization. Revocation bumps
the actual legacy authorization epoch: batch admission fails before callback,
then a scoped legacy inbox page is fetched and serialized. That fallback probe
**does not include the full inbox HTTP handler or its exact legacy count**, so
its timings are a lower bound on the eventual fallback request. The ordinary
path's shadow rows also do not establish canonical inbox projection parity.

One cold, one warm and one revoked sample were taken at each scale. Other local
checks were active; these are diagnostic samples, not isolated p95 measurements.
No budget, hash pin or allowance changed. Cold/revoked results already fail by
large margins, so there is no enablement or speed-win claim.

| Corpus | Case    | SQL | Returned rows |       Elapsed |
| ------ | ------- | --: | ------------: | ------------: |
| 1x     | cold    |  74 |           657 |   1.12775175s |
| 1x     | warm    |  74 |           657 |   56.288584ms |
| 1x     | revoked |  72 |           551 |  962.197875ms |
| 10x    | cold    |  74 |           657 | 47.914322208s |
| 10x    | warm    |  74 |           657 |  122.106292ms |
| 10x    | revoked |  72 |           551 | 45.025424833s |

The batch repository itself uses **7 read queries / 527 returned rows**, including
the proof lookup and hydration. A committed maximum-selection test asserts these
numbers. The directory still uses 65 queries/128 rows. The dominant cold/revoked
cost is legacy denial preparation, despite its small returned-row count. This
shows why returned rows and a repository subtotal cannot certify request cost.

## Phase measurements

| Corpus | Case    | Phase           | SQL | Returned rows |       Elapsed |
| ------ | ------- | --------------- | --: | ------------: | ------------: |
| 1x     | cold    | directory       |  65 |           128 |     460.375µs |
| 1x     | cold    | legacy-denial   |   2 |             2 |    1.0811045s |
| 1x     | cold    | admission       |   4 |           322 |    2.570291ms |
| 1x     | cold    | candidates      |   1 |           101 |   26.141166ms |
| 1x     | cold    | hydrate         |   1 |           100 |    1.716417ms |
| 1x     | cold    | counters        |   1 |             4 |   15.479417ms |
| 1x     | cold    | serialize       |   0 |             0 |       74.25µs |
| 1x     | warm    | directory       |  65 |           128 |      463.75µs |
| 1x     | warm    | legacy-denial   |   2 |             2 |   13.341625ms |
| 1x     | warm    | admission       |   4 |           322 |    2.323708ms |
| 1x     | warm    | candidates      |   1 |           101 |   23.640125ms |
| 1x     | warm    | hydrate         |   1 |           100 |    1.389791ms |
| 1x     | warm    | counters        |   1 |             4 |   14.988583ms |
| 1x     | warm    | serialize       |   0 |             0 |      31.792µs |
| 1x     | revoked | directory       |  65 |           128 |     424.041µs |
| 1x     | revoked | legacy-denial   |   2 |             2 |   945.31625ms |
| 1x     | revoked | admission       |   4 |           321 |    3.108125ms |
| 1x     | revoked | legacy-fallback |   1 |           100 |   13.230666ms |
| 1x     | revoked | serialize       |   0 |             0 |      36.917µs |
| 10x    | cold    | directory       |  65 |           128 |     498.334µs |
| 10x    | cold    | legacy-denial   |   2 |             2 | 47.874103125s |
| 10x    | cold    | admission       |   4 |           322 |    2.261583ms |
| 10x    | cold    | candidates      |   1 |           101 |   21.832917ms |
| 10x    | cold    | hydrate         |   1 |           100 |      1.7325ms |
| 10x    | cold    | counters        |   1 |             4 |   13.691667ms |
| 10x    | cold    | serialize       |   0 |             0 |      30.417µs |
| 10x    | warm    | directory       |  65 |           128 |     414.375µs |
| 10x    | warm    | legacy-denial   |   2 |             2 |   82.353667ms |
| 10x    | warm    | admission       |   4 |           322 |      2.1945ms |
| 10x    | warm    | candidates      |   1 |           101 |   21.741042ms |
| 10x    | warm    | hydrate         |   1 |           100 |    1.333458ms |
| 10x    | warm    | counters        |   1 |             4 |   13.909625ms |
| 10x    | warm    | serialize       |   0 |             0 |      31.542µs |
| 10x    | revoked | directory       |  65 |           128 |     410.916µs |
| 10x    | revoked | legacy-denial   |   2 |             2 | 44.854495708s |
| 10x    | revoked | admission       |   4 |           321 |    3.144209ms |
| 10x    | revoked | legacy-fallback |   1 |           100 |  167.211792ms |
| 10x    | revoked | serialize       |   0 |             0 |          55µs |

## Reproduction and remaining acceptance

The exact evaluation harness is
`docs/design/proposals/batch-scale-evaluation_test.go.txt`. In an isolated checkout
of this checkpoint, copy it into `core/internal/scopedrepo/` as a `_test.go` file.
Extract `driver.go`, `fixture.go` and `plans.go` from the pinned #295 `perfguard`
directory to `core/internal/testutil/perfguard/`. In that isolated fixture only,
change `const Rows = 4096` and `const PMRows = 1024` to `var` declarations so the
harness can scale them. Run from `core`:

```sh
go test ./internal/scopedrepo -run TestBatchSCA661Evaluation -count=1 -v -timeout=30m
```

The evaluation test logs the gate comparison; passing the test means the probe
completed, not that the serving budget passed. Production acceptance still needs
B's actual canonical inbox capture/HTTP parity, trusted full-generation comparison
and disjointness minting, complete source/publication-authority invalidation,
sealed dispatcher/hooks and the SCA-661 full HTTP/count/fallback and indexed-plan
gates. No production import guard or route has changed.
