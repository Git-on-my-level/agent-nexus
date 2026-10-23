# Watch dashboards

Watch dashboards are ordinary version 1 visual-report documents. Their live
panels read current, authorized records; ANX does not run models or agents.

A collection panel stores this query in its `data` field (or the equivalent
`query` field, without `data`):

```json
{ "collection": "loops", "group_by": "project", "limit": 24 }
```

Use panel type `live-collection`. Collections are `initiatives`, `loops`, and
`agents`. Optional `project_ref` selects a topic; `group_by` is `project` (the
default) or `none`; `status` is an array of collection state keys. The response
contains ordered `states`, `groups` of rows, per-state `counts`, and `has_more`.
Rows include owner, next actor, fourteen UTC daily activity counts (oldest first),
progress, time, and update time. Project group keys use `project:<topic handle>`;
`none` means no project and `all` means ungrouped.

Initiatives are open native cards on boards with role `initiatives`. Their
states are `waiting_on_you`, `at_risk`, `stalled`, and `on_track`. An open ask
addressed to the viewer about or blocking the initiative makes it waiting on
them. A slipped promise or high risk makes it at risk; no promise or blocked
work is stalled. Progress comes from the reader-visible plan and time from the
current promise. Hidden linked plan text remains hidden.

Loops are open cards with a cadence. Existing health maps failing → `failing`,
overdue → `stopped_reporting`, stalled → `flat`, healthy → `improving`, and
unknown → `unknown`. A goal gives a fourteen-day sparkline when it has points.
Agents use the bounded workspace roster: waiting on a human → `stuck`, stale →
`offline`, idle → `idle`, and working → `working`. Their current task supplies
progress and project membership. Presence does not prove progress.

The maximum displayed collection size is 48 (default 24). Admission is bounded
to 200 initiatives, 50 loops, or 100 agents. Counts describe that reader-visible
cohort before status/display filtering, rather than workspace totals. Agents'
project filtering applies within the admitted roster. `has_more` reports either
admission or display truncation; an unavailable panel is never an empty success.

`live-activity` accepts `subject_refs` (up to 100 unique refs) and `collection`
(`initiatives` or `loops`). Both together intersect. Subject activity includes
its backing-thread events and explicit subject events, including asks posted on
another thread. Resource IDs and handles normalize before subject comparison.
The existing 200-event candidate budget still applies, with `truncated` signaling
additional candidates. Inaccessible subjects and events contribute no content.

Each panel may declare `role: primary`, `secondary`, or `context`. Collections,
loops, charts and other data panels default to primary; activity defaults to
secondary; explanation, callout, artifact preview and comparison default to
context. Render output always includes the resolved role.

## Personal dashboard tabs

```sh
anx workspace dashboard pins list
anx workspace dashboard pins set document:operations document:delivery
anx workspace dashboard pins set none
```

`GET /workspace/dashboard/pins` returns `{document_refs,source}` in pin order.
`PUT` replaces all pins atomically with `{document_refs:[...],request_key}`.
Clients persist and reuse `request_key` until confirmed; keyed writes atomically
replay their original result without replacing newer pins. The CLI allocates
the key once before transport. Each authenticated
human or agent owns their list; no actor override is accepted. Up to twelve
unique active visual reports may be pinned. Empty lists return to the explicit
workspace default, marked `source: workspace_default`; personal lists use
`source: personal`. Inaccessible, archived, trashed or no-longer-valid report
heads are omitted. Clearing a personal list does not change the workspace default.

`anx workspace dashboard set` remains the workspace default write.

## Independent panel loading and goal labels

```sh
anx report render document:operations --panels loops,activity
anx work goal card:energy --metric drain --direction down --label "Battery drain" --unit "%/h"
```

`GET /docs/{document_id}/report?panels=loops,activity` returns only those panel
IDs, in authored order. Duplicate, empty and unknown IDs are invalid. Rendering
uses at most four workers. Identical queries share work within a request.
Native live panel results have a two-second, principal/authorization-epoch and
revision keyed server cache. Source-bound series, fleet and agent roster panels
stay fresh through their own readers. Browser responses retain `Cache-Control:
no-store`. Cache capacity is 64 entries of at most 128 KiB each; unavailable
results are not cached. Goal labels are optional strings of at most 200 characters;
units remain optional strings of at most 80 characters.
