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
