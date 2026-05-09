---
title: "Give card lifecycle event rows distinct labels"
agent: codex
done: true
ticket_id: "tkt_webui_card_event_labels_062"
---

## Triage

Priority: P3. This is a low-risk operator clarity quick win that can be picked up opportunistically.

## Problem

Card lifecycle event rows collapse `card_closed`, `card_resolved`, `card_archived`, and `card_trashed` into the label `Card closed`. Operators scanning Home or Events cannot tell whether a card was resolved, archived, or moved to trash without opening raw details.

## Evidence

- `web-ui/src/lib/events/eventRows.js:134`-`:140` groups four card lifecycle event types and sets `label = "Card closed"` for all of them.
- `web-ui/tests/unit/eventRows.test.js:49`-`:68` covers card movement links but does not cover distinct lifecycle labels.
- Command used: `nl -ba web-ui/src/lib/events/eventRows.js | sed -n '82,140p' && nl -ba web-ui/tests/unit/eventRows.test.js | sed -n '1,80p'`.

## Proposed Fix

Replace the grouped label with a small event-type-to-label map, for example:

- `card_closed` -> `Card closed`
- `card_resolved` -> `Card resolved`
- `card_archived` -> `Card archived`
- `card_trashed` -> `Card trashed`

Keep the existing detail, source label, and card modal href behavior.

## Validation

- Extend `web-ui/tests/unit/eventRows.test.js` with a table test for these four event types.
- Run the event row unit test and the normal web-ui check target if practical.

## Progress

- Added a `CARD_LIFECYCLE_LABELS` map so `card_closed`, `card_resolved`, `card_archived`, and `card_trashed` render distinct labels while preserving existing detail, source label, and card modal href behavior.
- Added table-driven unit coverage for the four lifecycle event labels.
- Validation passed:
  - `pnpm vitest run tests/unit/eventRows.test.js`
  - `make -C web-ui check`
