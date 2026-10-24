# Attention records and dashboard visits

Agents choose which updates deserve a person's attention. ANX stores those
choices and human feedback; it runs no models.

```sh
anx attention place card:launch --for actor:alex --to watch --reason "Launch is progressing; no decision needed" --rank 10
anx attention feedback list --since 24h --for actor:alex
anx attention items --surface all
anx attention changes --scope-ref builtin:initiatives
```

Principal refs are `actor:<actor_id>`. Clients holding authentication identity IDs
may also use `human:<principal_id>` or `agent:<principal_id>`; core normalizes them
to actor refs. Placements are latest-per-subject, recipient and placer. A human
can designate an agent with `anx attention agent set <agent-principal-ref>` and
clear it with `none`. That agent's placement wins when present; otherwise the
newest placement wins. Defaults retain active work on Watch in ordinary work
order. Lower rank sorts first within each page.

An open ask addressed to a human always appears on that person's Decide. A
placement cannot demote it, and `since` never filters out an outstanding ask.
Asks use the event as their subject, preserving separate decisions about the
same card. Placements on the card remain separate updates.

All records stay inside the deployment's workspace. Readers get their own
placements; any human or auth-admin may select another principal with `for`.
Any human may read feedback; an omitted `for` means `me` for humans and all
workspace feedback for agents. Resource
visibility still applies, including private references inside reasons and
feedback notes. Workspace agents can read feedback without impersonating its
human author. Only humans may write feedback or designate the attention agent.

## HTTP interface

- `POST /attention/placements`: `{subject_ref, for, to, reason, rank?, request_key?}`.
- `GET /attention/items`: `for=me`, `surface=all|decide|watch|digest|routine`,
  optional RFC3339 or positive-duration `since`, `scope_ref` (default `builtin:initiatives`) and `cursor`.
- `GET/PUT /attention/agent`: `{attention_agent: <principal-ref>|null}`.
- `POST /attention/feedback`: `{subject_ref, signal, surface, note?, placed_by?,
seconds_visible?, request_key?}`. Human recipient/author is server-derived.
- `GET /attention/feedback`: optional `for`, RFC3339 or positive-duration `since`, and `cursor`.
- `POST /attention/visits`: `{scope_ref}` for the authenticated principal only.
- `GET /attention/changes?scope_ref=...`: `{since, items, changed_refs, truncated}`.

List results are `{items, next_cursor}`. Follow `next_cursor` until null, including
on empty pages: fixed candidate windows preserve bounded work when records are
hidden or filtered out. Attention items traverse mandatory asks first, then
placements by stable subject identity, then ordinary work defaults. This keeps
all outstanding asks reachable rather than silently truncating a large queue.
Cursors are encrypted, authenticated, expire in 24 hours, survive restarts, and
bind the reader and filters. A duration window is resolved once and retained
in its continuation. Rank orders each page; clients combining pages may
sort their collected placements by rank. Persist one `request_key` per submitted
placement/feedback action and reuse it until confirmed. Canonical placement/feedback record, routing
index and original response are committed atomically; a changed body with the
same key conflicts. Generic `POST /events` cannot forge feedback. Placement records are principal-scoped
and do not publish a public placement event.

Reasons are at most 200 Unicode characters; notes are at most 500. Signals are
`more_like_this`, `less_like_this`, `not_relevant`, `should_be_decide`,
`should_be_watch`, `already_knew`, `didnt_need_me`, `needed_sooner`,
`not_enough_context`, `wrong_person`, `snooze`, and `wanted_to_see`. These are
feedback records, including `snooze`; ANX does not infer a snooze duration or
change a placement from a feedback signal.

## Since you last looked

Visits are keyed by principal and stable dashboard document identity, or
`builtin:initiatives`. Document aliases resolve to the same marker. First reads
return null `since` and empty changes. Reads never advance the marker. Agents may mark their own scope only; no request accepts another principal or
a caller timestamp.
The old Overview behavior retains its separate `builtin:overview` baseline.
Out-of-order requests cannot replace a newer visit.

The builtin baseline contains at most 50 authorized work subjects, including
recently closed work, with computed plan-step statuses and loop goal values.
Document baselines use that report's authorized rendered subjects, bounded to
100 subjects, and fail closed if a panel cannot be read. Changes include state,
plan/progress, and loop metric transitions, with before/after values. Deltas
contain only currently readable subjects and never include removed/private
subjects merely because they existed in an older visit. `truncated` reports
bounded coverage. Item flags use the selected dashboard marker and the newer of
placement and subject update time.

The canonical contract is [anx-openapi.yaml](../contracts/anx-openapi.yaml).
