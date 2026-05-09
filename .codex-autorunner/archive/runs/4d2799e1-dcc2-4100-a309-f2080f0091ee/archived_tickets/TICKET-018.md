---
title: "Review the completed tech-debt cleanup pass"
agent: codex
done: true
ticket_id: "tkt_final_review_cleanup_pass_018"
---

## Triage

Priority: P2. Run this after the preceding tech-debt tickets have landed, so the review evaluates the integrated result rather than isolated fixes.

## Goal

Review the work completed across this tech-debt cleanup queue and verify that the fixes landed coherently across module boundaries.

## Problem

The preceding tickets touch core invariants, contract guardrails, bridge and CLI automation behavior, and web-ui operator state. Even when each ticket passes its local validation, the combined result can leave documentation drift, duplicated patterns, missing cross-module validation, or follow-up cleanup that only becomes visible after integration.

## Review Scope

- Read the final diffs or commit history for the completed tickets in this queue.
- Confirm each ticket's stated validation was run or has a documented reason it was not run.
- Check that contract, core, CLI, bridge, and web-ui docs still describe the behavior that now exists.
- Look for inconsistent implementations of similar fixes, especially request staleness guards, config/preflight validation, generated artifact checks, and projection semantics.
- Identify any tests that are too narrow, flaky, or missing the regression implied by the original ticket.

## Proposed Work

- Produce a concise review note in the ticket body or a linked handoff describing:
  - issues found,
  - residual risks,
  - validation gaps,
  - suggested follow-up tickets if needed.
- Fix tiny documentation or test-label issues directly only when they are clearly in scope and low risk.
- Do not reopen broad implementation work inside this ticket; create follow-up tickets for anything non-trivial.

## Validation

- Run the smallest relevant checks needed for any review fixes made.
- If no code changes are made, run `python3 .codex-autorunner/bin/lint_tickets.py` and record that this was a review-only ticket.

## Review Notes

Review-only pass completed against the integrated cleanup diff from `v0.8.0` (`908eacef`) through `HEAD` (`95e21a30`).

Issues found:

- None requiring a fix or follow-up ticket before continuing the queue.

Validation coverage reviewed:

- Tickets 001-003 recorded focused core tests plus `make -C core check`.
- Tickets 004-006 recorded route/contract generator checks; the x-anx and contract mirror work also ran `./scripts/contract-gen`, `./scripts/contract-check`, or `./scripts/contract-check --committed` as appropriate.
- Tickets 007-009 recorded bridge and CLI checks (`make bridge-test`, `make cli-check`) plus focused tests for config side effects, auth fallback, and TOML parsing.
- Ticket 010 recorded CLI preflight tests plus `make cli-check`.
- Tickets 011-015 recorded focused web-ui unit tests plus `make -C web-ui check`.
- Tickets 016-017 recorded core projection tests plus `make contract-check` where OpenAPI/generated artifacts changed, and `make -C core check`.

Cross-module consistency reviewed:

- Contract docs and generated mirrors now describe materialized inbox `risk_horizon_days` semantics; core list/get/stream all consume stored derived inbox rows.
- Core docs explicitly describe event lifecycle columns as the bounded mutable exception to append-only event content.
- CLI usage preflight metadata now follows parser/help metadata for bridge restart and notifications.
- Bridge config loading/discovery now separates read-time path resolution from write/runtime directory creation, and CLI discovery uses the TOML parser already used by bridge lifecycle code.
- Web UI request staleness fixes use monotonic request/run tokens consistently across Home unread, inbox detail, topics/threads, and hosted billing polling.

Residual risks:

- The x-anx validation baseline is intentionally transitional debt; the generated validation report should keep shrinking in later contract metadata cleanup.
- Inbox `risk_horizon_days` remains a deprecated compatibility parameter. Its current behavior is documented and validated, but future removal should be a deliberate contract change.

Suggested follow-up tickets:

- None from this review. Ticket 019 is already queued for a fresh post-cleanup scout pass.

Validation run for this review:

- `python3 .codex-autorunner/bin/lint_tickets.py`
