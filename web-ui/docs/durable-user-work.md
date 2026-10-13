# Durable user work

The invariant is **never lose user input**. Core remains the source of truth for
confirmed data. Drafts and unconfirmed requests are private device-side work,
not a second workspace database. See the hard rule in root `AGENTS.md`.

## Rollout and acceptance

SCA-781 permits reviewable stages. The first change covers the human-attention
response path, the production timeout trigger. It does not claim that every UI
mutation already satisfies the invariant.

| Stage                     | Coverage                                                                                                              | Acceptance gate                                                                                                                                                 |
| ------------------------- | --------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Inbox response foundation | Freeform responses, suggested picks, acknowledgement/access/review outcomes, and context notes in both Inbox surfaces | IndexedDB/journal recovery, persisted requests, original-key retries, attention UX, cross-tab lock, privacy cleanup, timeout/524/offline/reload/duplicate tests |
| Complete urgent surfaces  | Discussion replies, comments, and Inbox PM decision responses                                                         | Reusable mutation outbox adapters; atomic replay audit for event creation, document/card comments and PM decisions; same recovery tests per adapter             |
| Remaining authoring       | Ask PM chat, cards, documents, plans, settings and all other mutation forms                                           | Explicit draft target keys, replay-safe server endpoints, every mutation in the outbox, complete surface inventory guard                                        |

The issue remains in progress until all stages pass. Do not enable blind retries
for an unaudited endpoint: an HTTP timeout can occur after the mutation committed.

## Current implementation

`localWorkStore.js` stores records in IndexedDB, with indexes on scope and
principal. The scope is the existing organization/workspace/agent/actor tuple.
A separate synchronous localStorage entry for each record checkpoints each input
event before yielding; it closes the tab-close gap and is the fallback when
IndexedDB is denied. IndexedDB still works when localStorage hits quota. If both
fail, the UI warns and does not send; text stays in the open tab with a copy path.
An unavailable device store cannot honestly promise recovery after closing a tab.
If no checkpoint has succeeded yet, closing or reloading prompts the reader to
keep the tab open. JSON snapshots make reactive state cloneable in IndexedDB, and
submitted request bodies are frozen so later edits cannot change a retry.

`durableDraft` restores a field by explicit resource/field key and dispatches
input so Svelte state follows the restored value. It ignores stale reads after
typing, identity changes or unmount. Both Inbox surfaces share
`inbox:<item-id>:reply` and `inbox:<item-id>:context` keys. Suggested picks are
checkpointed before their visual confirmation.

The Inbox queue records the entire request, item, restore data, original UUID,
undo deadline, attempt count and retry deadline before sending. After reload it
rebuilds the current principal's work and resumes due requests. Network errors,
408, 429 and 5xx retry with exponential backoff capped at one minute. Attention
states retain text and never auto-retry. Focus and reconnect resume draining.
Successful server confirmation tombstones the request; a matching submitted
draft is cleared without clearing newer text written during sending. Explicit
Undo transfers the request back to a draft. Editing retains the older request
as recovery history and a newly submitted correction uses a new key.

Web Locks serialize sends across tabs for one scope. BroadcastChannel and
storage events synchronize saved changes and privacy revocation. A browser
without Web Locks keeps work in attention rather than sending without a lock.
The next adapters must use the same scope-wide lock; do not create independently
draining per-surface queues.

Logout and principal switches clear the principal's records across workspaces,
erase visible drafts in other tabs, and fence late saves with an identity epoch.
Temporary session loss pauses and hides private work; it does not delete it.
The same owner can sign in and recover the original request/key, while another
validated owner replaces the previous owner and clears that owner's copies.
Device work is never sent to a hosted control plane. The old detail-page draft
timer used a workspace/item key without a principal; that timer and automatic
restore are removed. Existing legacy keys are retained until sign-out or a principal switch, when
unattributed drafts are cleared for shared-device privacy. They are never
automatically exposed to a new principal. A future legacy recovery path must obtain
explicit ownership rather than silently assigning those drafts to the next user.

User-facing failures are classified in `mutationStatus.js`. Raw upstream bodies
appear only as bounded plain text inside a closed technical-details disclosure.
The global unsent indicator includes attention states, and each failed item
offers the saved response, copy, edit and retry.
Pending responses remain open with a Pending badge until confirmed. The detail
page keeps the delivery state visible after submission; the reader can navigate
back to the Inbox and answer a different item while the first sends. The same
item's composer is held while an uncertain response is pending or retrying.

## Replay audit

No HTTP routes or canonical contracts change in this stage.

| Path                                | Current evidence                                                                                                                                                                              | Next action                                                                                                           |
| ----------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `POST /inbox/{id}/respond`          | Existing `idempotency_key` contract; `AppendHumanAttentionResponse` atomically commits response and replay; concurrent eight-request test proves one event and rejects changed-body key reuse | Preserve full original request/key through device recovery; covered here                                              |
| `POST /events` (discussion replies) | `request_key` replay handling and event-ID conflict recovery in `primitives_handlers.go`                                                                                                      | Audit actor/body matching and transaction/crash boundaries before outbox migration                                    |
| Document/card comments              | Dedicated comment endpoints are separate from discussion events                                                                                                                               | Audit contract, replay records and atomicity, then migrate every comment composer                                     |
| PM decision answers and chat        | Separate PM commands and lifecycle                                                                                                                                                            | Audit retry keys and replay behavior before automatic retry                                                           |
| Card/doc/plan edits and other forms | Diverse create/patch/action commands                                                                                                                                                          | Inventory each command, freeze original mutation intent and concurrency token, add atomic replay and durable adapters |

## Checks and extension rules

`durableInputContract.test.js` parses the Svelte AST to require durable draft
actions on every authored Inbox response field and rejects direct Inbox sends
in any Svelte surface. This is a guard for the migrated scope; subsequent stages
must extend it to all mutation forms. Do not present it as whole-UI enforcement.

`durable-inbox.spec.js` covers reload/navigation/new-tab drafts, timeout and 524,
closing an offline tab, interrupted confirmation with a duplicate retry across
tabs, definitive conflicts, storage denial and delivery-state screenshots.
The browser mock checks exact body/key replay; the real core concurrency test
checks that duplicate retries produce one canonical response event.

IndexedDB lists use the scope index, O(records in the device scope), and target
lookups use the primary record key, O(1). The fallback
journal scan is O(device records), with no workspace server scan. Existing Inbox
point/list requests and authorization paths are unchanged. Recovery does not add
server polling; it sends due persisted requests and uses normal refresh listeners
after confirmation.
