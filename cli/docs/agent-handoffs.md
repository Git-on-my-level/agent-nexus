# Addressed agent handoffs

Use a handoff to wake a registered agent in the current ANX workspace and ask
for a pickup receipt. Address the recipient by the actor ID reported by
`anx api agents.list`. A host runtime bridge must have its wake job enabled for the
recipient. An offline recipient can receive a queued handoff when its runtime
reconnects using the same actor identity.

```sh
anx agent handoffs send --to "$recipient_actor_id" --expires-at "$deadline" \
  --ref "event:$event_id" --ref "multica:issue:$issue_uuid"
anx agent handoffs get "$handoff_id"
```

Use an RFC 3339 deadline within seven days. Supply up to eight refs. ANX refs
use `event:<id>` and must be readable to both agents. Multica refs use the issue
UUID. GitHub refs use canonical issue, pull request, or full commit URLs under
`https://github.com/`. Link to context and evidence in the source systems.
Retain the recipient, deadline and refs for replay. Equivalent requests from
the same sender return the same ID and current receipt, including after expiry.

The recipient opens the refs with its authorized source tools. ANX checks native
event access and never requests the external URLs. Report the first ref that
cannot be opened under the recipient's credentials.

```sh
anx agent handoffs ack "$handoff_id" --outcome refs_verified
anx agent handoffs ack "$handoff_id" --outcome ref_blocked --blocked-ref "$ref"
```

The first pickup result is final. Repeating that result returns the original
acknowledgment timestamp. The sender and recipient can read the receipt.
`bridge_delivered` means the bridge callback returned successfully.
`acknowledged` means the recipient explicitly checked the references or named a
blocker. The recipient may acknowledge during the callback before the bridge
reports success. Delivery and pickup carry no approval, task ownership or task
completion. Ordinary handoffs create no human Inbox items.

Unclaimed work can move with a recipient who's enrolled under the same actor
identity. A restarting bridge uses its retained local receipt to resume a
definitely unstarted callback or record a known outcome. When the original
claim's outcome is unknown, the bridge leaves it visible rather than rerunning
the callback on a replacement. New claims and pickup stop at the deadline.
Automatic fallback and role resolution are deferred. Operators can inspect
expired or blocked receipts and make a separate decision about redispatch.

Deploy the core first and update the receiving bridge to a CLI with the handoff
commands. The updated runtime opts in on the existing wake stream. Older
bridges continue to receive ordinary and answer wakes.
