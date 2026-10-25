# Rewriting an open ask

A clarity rewrite changes how an existing ask reads while keeping its owner and
answer buttons. It never creates a replacement ask or transfers the answer.

Read its presentation and token:

```sh
anx ask presentation event:ask-id
```

Save a patch in `presentation.json`, using the returned `presentation_revision`
as `if_revision`:

```json
{
  "if_revision": 0,
  "request_key": "clarity-review-1",
  "presentation": {
    "title": "Approve the deletion check?",
    "situation": "Deleted users' data can reappear after a background job runs.",
    "impact": "That breaks our deletion promise.",
    "recommendation": {
      "action": "Add the existing deletion check before writing.",
      "why": "It prevents old jobs from restoring deleted data."
    },
    "alternatives": [
      { "action": "Wait", "consequence": "The fix stays blocked." }
    ],
    "if_no_answer": "The change stays blocked.",
    "details": "Implementation evidence and explanations belong here."
  }
}
```

```sh
anx ask revise event:ask-id --from-file presentation.json
anx ask revise event:ask-id --undo --if-revision 1 --request-key clarity-undo-1
```

Omitted fields are preserved. Null clears an optional field; title cannot be
cleared. The title has the existing 160-character, single-line limit. Structured
fields retain #480's bounds: 4,000 characters for prose/reasons/consequences,
240 for actions, five alternatives, and 32,000 for details. Unknown request,
presentation and nested fields are rejected. Recommendation and alternatives
are explanatory copy: answer buttons are always the original options. If an
option is unclear, explain it in alternatives/details; do not rename it.
Evidence, body, options/proposals (including their text, order, count and
recommended index), requester, subject, decision kind, routing, deadlines and
semantic revisions are immutable through this API.

The HTTP endpoints are `GET /asks/{ask_id}/presentation` and
`POST /asks/{ask_id}/revise`. MCP exposes `anx_asks_presentation_get` and
`anx_asks_revise` with the same path and body. The CLI derives a replay key from
the exact ref/token/body if one is omitted. After an uncertain send, reuse the
same file, token and key. A changed body with the same key conflicts. Undo may
read the current token when `--if-revision` is omitted; explicit tokens make
retries reviewable. Both undo and ordinary revisions increment only
`presentation_revision`, initially zero. A stale token or a closed ask returns 409. Exact retries replay their committed result, even if the ask has since
closed, subject to current permission and visibility.

## Authority and audit

The requester, an active human workspace member, or an explicitly granted
auth-admin agent can revise. This uses the existing grant rather than adding a
second permission system. Non-requesters' durable authority is checked inside
the write transaction, so a revoked grant cannot survive an upload. Auth-admin
never grants visibility: ask, thread and subject card rules apply to reads,
writes and replay receipts. No auth grant is added by this feature.

Each successful revision appends `human_attention_revised`, with actor, time,
previous presentation, new presentation and its counter. The original ask event
is immutable. A pointer/counter in `ask_presentation_heads` selects the current
audit event; no historical scan is needed. The event, pointer and replay receipt
commit together. Ordinary `POST /events` cannot forge revision events.
Decide shows the curator and a Show original / Show rewrite toggle, including
batch members. The toggle only switches copy; it never changes answer buttons.
Undo restores the preceding presentation as another audit event. Repeating undo
on a later token restores that event's predecessor; history is never erased.

## HTTP authorization classes

All three routes accept authenticated human members and agent bearers, with
current ask/card visibility enforced in core. Hosted proxy rows should use the
**agent-bearer** class (which also permits human sessions), with anonymous access
disabled. None is an admin-only or human-only route.

| Method | Path                          | Proxy auth class                           | Core authorization                                                                     |
| ------ | ----------------------------- | ------------------------------------------ | -------------------------------------------------------------------------------------- |
| GET    | `/asks/{ask_id}/presentation` | agent-bearer (human sessions also allowed) | Any authenticated principal that can see the ask and its card.                         |
| POST   | `/asks/{ask_id}/revise`       | agent-bearer (human sessions also allowed) | Active human member, requester, or explicitly granted auth-admin agent.                |
| POST   | `/asks/{ask_id}/retire`       | agent-bearer (human sessions also allowed) | Active human member, requester, or explicitly granted auth-admin agent; includes undo. |

The proxy admits an agent token; it does not grant curation authority. Core
still denies another non-admin agent on either write route. Admin agents use the
same bearer route and do not bypass resource visibility.

## Answer-channel compatibility

The existing core inbox and event refs, proposals, recommended index and semantic
revision remain unchanged. Current channel integration was inspected in the
separate deployment repository: Telegram ask callbacks retain ask/inbox identity,
option index and exact option text; their semantic revision comparison belongs
to PM decisions (`decision:`). Email links open Decide by the existing event ref.
The new presentation counter changes neither binding. The OSS acceptance test
captures the inbox/options before a rewrite, answers afterward and checks that
the notification targets the original requester. No deployment-specific code or
endpoint is introduced here.

## Retiring stale asks

The same curator set can retire an ask that no longer needs a decision:

```sh
anx ask retire event:ask-id --reason "The old rollout was cancelled." --evidence card:rollout
anx ask retire event:ask-id --undo --if-retirement event:retirement-id --request-key retirement-undo-1
```

`POST /asks/{ask_id}/retire` and MCP `anx_asks_retire` accept the same request.
Retire requires a reason (at most 2,000 characters), 1–32 evidence resource refs
or HTTP(S) URLs, and a request_key. The CLI derives the key when omitted; use a
fresh key for a later intentional retirement. Undo reads the current retirement
when its token is omitted. For a reviewable retry after an uncertain result,
retain both the exact `--if-retirement` token and `--request-key`.

Retirement appends `human_attention_retired`, naming the actor, timestamp,
original requester, reason and evidence. The canonical resolution is a withdrawal;
ask outcomes expose `withdrawn_as_stale` and `retirement_event_ref`. It never
records a human answer, changes work phase, or enters answer counts. Decide
hides the ask immediately. Cached inbox reads and answer submissions fail
closed with “no longer open”; external adapters must treat 404/409 as closure.
A generic durable notification targets the original requester, includes the
reason and links the audit event. Undo queues a reopening notification as well.
Notifications inherit the audit's resource visibility rules.

Undo appends `human_attention_reopened` and releases only the exact current
retirement claim. It cannot undo a human answer, ordinary withdrawal, later
retirement, expired ask or closed subject. The original options, requester,
presentation, deadlines and semantic revisions stay intact. Brief reopens the
ask atomically; the maintained inbox projection then restores its original item.
The audit replay recognizes retirement refs by both canonical IDs and public
handles, independent of event ordering. Access-request enrollment/grant asks
retain their separate lifecycle and cannot be retired or reopened here.

A retirement and an in-flight answer compete for the same unique resolution
claim in one transaction. Only one can commit. Exact request-key retries replay
the original result, subject to current curator authority and resource visibility.
