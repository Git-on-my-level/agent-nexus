# Agent workflow ergonomics measurement

The opt-in `TestAgentErgonomicsAgainstCore` scenario builds a real core and CLI,
uses a temporary workspace and host enrollment, and exercises orientation,
pickup, session registration, participation, progress, an ask and answered
await, blocking/unblocking, plan add/update/read, completion with evidence, and
report publishing. It logs every measured argv, exit code, stdout, stderr and
byte count. Enrollment, the simulated human's inbox lookup/answer, and creating
the report topic are setup fixtures excluded from the workflow counts. The ask
uses an explicit lint override because it is a synthetic decision.

The deliberate invalid `--status in_progress` probes agent error recovery. The
script reads its error and calls help only if accepted values are absent, then
retries with `active`. This measures a scripted recovery policy, not a model's
success rate. It does not predict how often an agent makes this mistake.

## Observed findings and shipped slice

| Finding                                                                                                      | Change or follow-up                                                                                                                                                                                                                                      |
| ------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Plan add/update/read repeat computed, authored and executive summary fields across many dotted-key lines.    | Compact plan header, one row per authored step with computed status taking precedence, health reason/timestamp, dependencies, critical path, next steps and mismatch/truncation flags. Complete fields remain in `--verbose`, `--headers`, and `--json`. |
| `invalid step status` does not name accepted values; recovery needs help.                                    | Error lists `done`, `active`, `blocked`, `not_started` and explains that work in progress uses `active`.                                                                                                                                                 |
| Individual plan help prints every plan verb.                                                                 | Focused verb usage, relevant flags and concurrency guidance.                                                                                                                                                                                             |
| Missing or duplicate step errors omit a runnable read command; conflicts require locating the plan.          | Return an exact read-only `anx plan show <requested-card>` action. No automatic mutation retry.                                                                                                                                                          |
| Session/participation JSON authoring still requires schema discovery beyond normal help.                     | Follow-up: include small validated registration examples in command help.                                                                                                                                                                                |
| Progress message and answered-await readbacks expand the entire event/response.                              | Follow-up: compact their normal text while retaining refs, decision, provenance and full JSON.                                                                                                                                                           |
| Concurrent local scenarios encountered an await timeout and card-move HTTP 500 before the changed plan code. | [Investigation #372](https://github.com/Git-on-my-level/agent-nexus/issues/372). Preserve failed logs; benchmark remains opt-in. Ten-second await and checked human fixture improve diagnostics without claiming the move failure is fixed.              |

## Comparison

Sequential runs against baseline `84e1a5ccfea0c139d65f929b2ec9dfdf61f68c43`
and the updated CLI used the identical scenario and an unchanged core.

| Measured agent workflow                     | Baseline | Updated |
| ------------------------------------------- | -------: | ------: |
| CLI calls                                   |       17 |      16 |
| Errors (intentional invalid status, exit 2) |        1 |       1 |
| stdout + stderr bytes                       |   17,623 |  13,264 |
| Successful plan add/update/show bytes       |    5,417 |   1,682 |

Both complete test commands exited 0. Output decreased 24.7% for this workflow
and 68.9% for its three plan commands. Generated ids, timestamps and report
temporary paths can vary byte counts slightly between runs. No latency or
server-cost improvement is claimed.

## Reproduce

From the repository root, build the baseline without altering the current tree:

```sh
git worktree add --detach ../anx-ergonomics-baseline 84e1a5ccfea0c139d65f929b2ec9dfdf61f68c43
(cd ../anx-ergonomics-baseline/cli && go build -o ../../anx-ergonomics-baseline-bin ./cmd/anx)
```

Run these sequentially from `cli/`, saving full output and each command's exit
code; do not use a pipeline that replaces the test exit code:

```sh
ANX_ERGONOMICS_CLI_BINARY="$PWD/../../anx-ergonomics-baseline-bin" go test -tags=integration,ergonomics ./integration -run '^TestAgentErgonomicsAgainstCore$' -count=1 -v
go test -tags=integration,ergonomics ./integration -run '^TestAgentErgonomicsAgainstCore$' -count=1 -v
```

Local validation also runs `make test-fast` and targeted output/app/skill tests.
The measurement lane has an extra `ergonomics` build tag to quarantine its
core instability under #372; it is not silently retried or skipped inside the
scenario. API routes, server reads, authorization and durable mutations are
unchanged. CLI projection work is linear in the response's steps and dependency
entries (server caps: 200 steps, 50 dependencies per step), with one bounded map
for computed states and no additional HTTP calls.
