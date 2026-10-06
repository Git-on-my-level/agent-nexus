# Data-only visual reports

Visual reports turn an existing ANX text document into a bounded, inspectable
operator report. Authors supply JSON data; the web UI owns rendering. Core remains
the source of truth, and normal document revisions, authorization, history, and
optimistic concurrency still apply. This is a presentation format carried by existing documents. Core materializes
optional live queries; authored snapshots remain attributed claims, not proof of
external truth.

## Author and publish a report

1. Read the underlying evidence through authorized tools. Record the actual
   observation time and links; do not replace old observation times with the time
   you generated a report.
2. Start with `anx report templates`, then generate a live report skeleton:

   ```sh
   anx report init --template workspace-overview --topic topic:YOUR-TOPIC > report.json
   anx report init --template initiative --card card:YOUR-INITIATIVE > initiative.json
   ```

   Templates keep workspace facts as bounded live queries. Add narrative only
   where judgment or context is needed; do not paste live numbers into the report.

3. Read supported panel types, limits, and the minimal example with
   `anx report schema`. Keep actual observations separate from illustrative
   examples, and state the outcome and remaining qualification boundaries.
4. Validate the file locally before previewing:

   ```sh
   anx report validate /path/to/report.json
   cat /path/to/report.json | anx report validate -
   ```

   The command is available with only the `anx` binary and returns bounded
   diagnostics for malformed, unsupported, or oversized reports.

5. Preview before telling a human what the report shows:

   ```sh
   anx report preview /path/to/report.json
   ```

   The command prints what each panel shows, its source, and freshness. It also
   writes `report-preview.png` with the same web renderer used by the UI. If no
   browser is available, the text summary still identifies each panel and its
   live source status.

6. Publish to an existing, authorized topic:

   ```sh
   anx report publish /path/to/report.json --topic topic:YOUR-TOPIC
   anx report publish /path/to/report.json --topic topic:YOUR-TOPIC --title "Fleet Dashboard" --doc doc:fleet-dashboard
   ```

   `report publish` validates before writing, stores `content_type: "text"`, and
   reuses a document with the same title/slug in that topic. With `--doc`, it
   revises that document directly. It reads the head back, validates it again, and
   prints the document ref and web URL when known. This avoids the proposal-only
   default of `docs revise` for an agent that intends to publish immediately.

7. Open the document in Docs and check its project filter, outcome, panel metadata,
   and evidence. The Overview prefers report titles beginning with `Dashboard` or
   `Fleet Dashboard`; the saved report remains an ordinary text document.

Command spellings and transport behavior come from
[`cli/docs/generated/runtime-help.md`](../../cli/docs/generated/runtime-help.md)
(`docs create`, `docs get`, `docs revise`) and the existing CLI helpers. The
ordinary HTTP equivalents remain `POST /docs`, `GET /docs/{document_id}`, and
`POST /docs/{document_id}/revisions`. Live readback uses `GET /docs/{document_id}/report`.

## Workspace Overview

The workspace Overview lists documents whose current text parses as a
recognized valid visual report (`parseVisualReport` returns a report) and
renders the preferred one inline. It scans the 50 most recently updated
active documents. Among valid reports, newest `updated_at` wins. A document
opts to be preferred by a title that starts with `Dashboard` or
`Fleet Dashboard` (the match is case-insensitive and stops at a word
boundary, so `Dashboard notes` qualifies and `Dashboarding` does not).
Preferred reports sort ahead of newer reports that do not opt in. This is a
title convention only; it is not a tag, a new document kind, or a core field.

## Live dashboards

Schema version stays **1**. Static panels and live panels share the same project
and layout grammar. A live panel’s `data` is a query, not an observation. Keep the
usual metadata fields for compatibility, using `observed_at: null`,
`freshness: "unknown"`, `provenance: "reported"`, and `source_ids: []`. The reader
replaces freshness and observation time with the authorized materialization.
The panel’s `project_id` groups presentation; use `data.project_ref` to scope a
query to an actual workspace project (a topic).

| Type                | Query fields                                                                      | Default                                                                                                     |
| ------------------- | --------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `live-initiatives`  | `board_refs`, `project_ref`, `card_ref`, `limit`, `sort`                          | All active boards; 10 rows; priority then newest update                                                     |
| `live-asks`         | `limit`, `include_answered`, `answered_only`, `answered_within_hours`, `card_ref` | 10 oldest open asks; recent answers are optional, and `answered_only` returns answers only                  |
| `live-work-mix`     | `board_refs`, `project_ref`, `card_ref`, `group_by`                               | Open work by phase; `group_by: "board"` also supported                                                      |
| `live-activity`     | `limit`                                                                           | 10 newest meaningful events, with same-actor board edits collapsed within five minutes                      |
| `live-fleet-health` | none                                                                              | Declared `fleet.*` series and native host inventory; enrollment requests require human or auth-admin access |

Limits are 1–100 displayed rows, at most 16 unique `board:<handle>` refs, and
1–720 hours for recent answers (168 by default). Sort is `priority`, `updated`,
or `title`. Other fields, URLs, and null query values are rejected. Archived or
trashed boards and their cards are excluded; `done` and `cancelled` phases are
closed. Checklist progress counts `- [ ]`, `- [x]`, and `- [X]` outside fenced
code; no checklist means unknown progress, not zero completed work. The first
nonempty prose line and every `Needs <human>:` line remain visible.

The UI refreshes every 30 seconds while visible and immediately on returning to
the tab. Every live panel's header shows **Live · updated 2m ago** with the
actual read time, and the exact instant on hover. Failed refreshes remove
previous successful values. Readers without access see an
unavailable panel; other live panels and authored snapshots remain usable.
Work materialization is bounded to 2,000 candidates per board/project scope;
event and decision reads each have a 2,000-row source cap. A displayed-row
limit or a source cap sets `truncated: true` and shows **Partial view**. This is
not a complete workspace count. Open asks outside the bounded event history may
be omitted; the partial flag preserves that uncertainty. Activity includes new
asks, PM decision proposals, answers, phase changes, completion, and collapsed edits.
Decision proposals are read through the same authorized service as the PM API;
activity shows the work title and creation time without copying private instructions.
Private PM history is filtered using ordinary event access rules.

The `initiative` template scopes its plan query and answered decision history to
one `card_ref`. Answered asks appear only when the request and response belong to
that card and thread; other initiatives' decisions remain outside the panel.

Agents read the same projection:

```sh
anx report render document:YOUR-DASHBOARD --json
# Same workspace authentication and access rules as other reads:
# GET /docs/document:YOUR-DASHBOARD/report
```

For an unsaved local report, `POST /reports/preview` applies the same bounded
live materializer without creating a document or revision.

The response includes `document_ref`, `revision_ref`, `observed_at`, and only the
live `panels`. Each panel has `id`, `type`, `status` (`ok` or `unavailable`),
`observed_at`, `truncated`, and `data`. Never treat an unavailable panel as an
empty workspace. Rendering is read-only and does not append a document revision.
The endpoint uses the current head query; a Docs historical revision whose ref
no longer matches the head fails safely instead of showing data for another
query definition.

A minimal mixed dashboard:

```json
{
  "kind": "anx.visual-report",
  "schema_version": 1,
  "title": "Workspace dashboard",
  "summary": "Current initiatives with our authored focus alongside them.",
  "generated_at": "2026-10-04T12:00:00Z",
  "projects": [
    {
      "id": "workspace",
      "title": "Workspace",
      "summary": "Our priorities",
      "outcome": "Ship the launch"
    }
  ],
  "sources": [],
  "panels": [
    {
      "id": "initiatives",
      "project_id": "workspace",
      "type": "live-initiatives",
      "title": "In flight",
      "author": "Workspace",
      "provenance": "reported",
      "observed_at": null,
      "freshness": "unknown",
      "source_ids": [],
      "data": { "limit": 7, "sort": "priority" }
    },
    {
      "id": "focus",
      "project_id": "workspace",
      "type": "callout",
      "title": "Our focus",
      "author": "Product team",
      "provenance": "reported",
      "observed_at": "2026-10-04T12:00:00Z",
      "freshness": "current",
      "source_ids": [],
      "data": {
        "tone": "info",
        "text": "Launch first, then measure adoption. This is an authored snapshot."
      }
    }
  ],
  "layout": {
    "type": "grid",
    "columns": 2,
    "children": [
      { "type": "panel", "panel_id": "initiatives" },
      { "type": "panel", "panel_id": "focus" }
    ]
  }
}
```

For all four live types in one layout, export the checked-in example:

```sh
node --input-type=module -e 'import { liveDashboardExample } from "./web-ui/src/lib/fixtures/liveDashboardExample.js"; console.log(JSON.stringify(liveDashboardExample, null, 2))' > dashboard.json
node web-ui/scripts/validate-visual-report.mjs dashboard.json
anx docs create --topic topic:YOUR-TOPIC --title "Dashboard" --body-file dashboard.json
```

The JS validator and core query parser run the same accepted/rejected fixtures in
`contracts/fixtures/visual-reports/queries.json`. The publishing validator in
SCA-597 can reuse these fixtures for CLI validation. `LiveInitiatives.svelte` is
the shared display component for report rows and SCA-595’s Overview initiatives.

## Version 1 shape

All properties shown below are required except the artifact's optional `url`.
Unknown fields, versions, component types, duplicate IDs, and dangling references
are rejected. Use `null` for unknown timestamps and metric values, never a guessed
date or a fabricated zero. Strings are rendered literally, including characters
that resemble HTML or Markdown.

```json
{
  "kind": "anx.visual-report",
  "schema_version": 1,
  "title": "Project evidence report",
  "summary": "A report with an explicit evidence boundary.",
  "generated_at": "2026-10-03T06:38:37Z",
  "projects": [
    {
      "id": "project-a",
      "title": "Project A",
      "summary": "No operational evidence has been collected.",
      "outcome": "Qualification unknown"
    }
  ],
  "sources": [],
  "panels": [
    {
      "id": "qualification",
      "project_id": "project-a",
      "type": "explanation",
      "title": "Evidence still needed",
      "author": "unknown",
      "provenance": "reported",
      "observed_at": null,
      "freshness": "unavailable",
      "source_ids": [],
      "data": {
        "text": "No observation is available. This does not establish health or completion."
      }
    }
  ]
}
```

The full six-component example is exported as `visualReportExample` and
`visualReportExampleContent` from
[`src/lib/fixtures/visualReportExample.js`](../src/lib/fixtures/visualReportExample.js).
Root `panels` refer to projects through `project_id`; panels are not nested under
projects. The project ID `all` is reserved for the all-project filter.
Project selection must never combine another project's evidence into
the selected project's outcome.

### Evidence and authorship

Each source has exactly:

```json
{
  "id": "source-a",
  "label": "Public source label",
  "url": "https://example.org/evidence",
  "observed_at": "2026-10-03T06:38:37Z",
  "kind": "release"
}
```

`kind` is descriptive text, such as `release`, `pull-request`, or `observation`.
It does not select a renderer or trigger a fetch. `observed_at` can be `null` when
unknown. Use stable, meaningful IDs and safe HTTP(S) links without credentials.
An external link opens only on operator action; rendering never fetches it.

Every panel includes `author`, `provenance`, `observed_at`, `freshness`, and
`source_ids`. `author` is an author-supplied label, not an authenticated principal
or an authorization grant. Use `"unknown"` if the author is unknown.

- `reported`: an attributed statement or synthesis; not independently verified
- `verified`: the author reports checking the cited evidence for this narrow
  claim; at least one source is required. The renderer does not itself verify an
  external source or authenticate this label
- `illustrative`: invented example data, never actual performance or health

Row and milestone `source_ids` must also occur in their containing panel's
`source_ids`, so every cited source is available in the panel's evidence inspector.
Do not promote publication, installation, or a successful static check into a
stronger claim about deployed behavior or operational qualification.

### Provenance: live or authored

Every panel resolves to one of two provenance classes, stated in its header so a
reader can tell a computed panel from a hand-written one before reading it:

- **live** — a live query panel or a bound series. Its header reads
  **Live · updated 2m ago**: the age of the read, not of the document. A read
  that is still running says so; a read that failed says so instead of showing
  the last age it had. A bound series whose publisher has missed its interval
  turns amber.
- **authored** — static content. Its header reads
  **Written by \<principal\> · 3d ago**, and the panel body is a step quieter
  than a live panel's.

An authored panel carries a review deadline. Past it, the header reads
**May be stale · written 9d ago** in amber, with an amber edge down the whole
panel. A panel that does not declare `review_by` is reviewed seven days after it
was written, and the tooltip says the deadline was defaulted. A panel with no
writing time at all asks for no review: a deadline anchored to nothing would
read as a fact.

Provenance is always conveyed in text. Colour repeats the words, never replaces
them, so the signal survives greyscale and a colour-blind reader.

An authored panel is also overdue the moment its document declares
`freshness: "stale"`. That is the author warning a reader deliberately, and it
is a stronger statement than a review date nobody has reached yet. Being merely
older than the 24-hour evidence window is not: that drives the freshness filter,
not this line.

A bound series falling back to its authored snapshot is authored, not live: it
is showing the document's own numbers with their original as-of time, and it is
always overdue, because the binding did not answer. While the first read is
still in flight the panel stays live rather than flipping class on every load. A
series that has missed its expected publishing interval reads **May be stale ·
last read 3d ago**.

`panelProvenance(panel, freshness, now)` in `src/lib/reportProvenance.js` is the
single place this is decided, and the Overview embed, the document report view
and the initiative page all render it through `ProvenanceChip`. The class is read
from `provenance_class` when the panel declares it, and derived from the panel's
shape otherwise, so a live type this build has never heard of still renders as
live data rather than as an authored panel with no body.

`provenance_class`, `authored_at` and `review_by` are read wherever they appear,
but the report schema still gates them: `parseVisualReport` rejects a panel
field it does not know, and one rejected field drops the whole report to its
text fallback. A stored document can therefore only carry them once
`visualReports.js` and `contracts/visualreport/report.go` accept them together.
The same applies to a new `live-` panel type. Until then these fields reach the
UI only through the rendered report response, which is not schema-validated.

Mind the asymmetry while that is true: the Overview renders its pinned report
from the `/overview` snapshot without validating it, so a stored document
carrying `authored_at` renders normally there and drops to a text fallback on
the document view.

### Freshness and incomplete observations

- `current`: observed within the last 24 hours at the displayed clock time
- `stale`: declared stale or observed more than 24 hours ago
- `unknown`: freshness or observation time is unknown
- `unavailable`: the observation could not be obtained; never display as healthy

`getPanelFreshness(panel, now)` demotes old current observations to stale without
editing the stored document. An unknown/invalid timestamp or a timestamp more
than five minutes in the future produces unknown. Explicit unavailable remains
unavailable; explicit unknown stays unknown. Current and stale authored panels
require a valid observation timestamp. Freshness is observation age, not a
completion state, availability guarantee, or health signal. `generated_at` is the
document generation time and must not be used to refresh old evidence.

Timestamps use ISO 8601 with seconds and an explicit timezone (`Z` or a numeric
offset); up to three fractional digits are accepted. Calendar dates are validated.

### Six supported panel data shapes

1. `explanation`: `{ "text": "Literal explanatory text" }`
2. `evidence-table`:

   ```json
   {
     "columns": ["Claim", "Observation"],
     "rows": [{ "cells": ["Release", "Published"], "source_ids": ["source-a"] }]
   }
   ```

   Cells must be strings and each row must match the column count. Empty strings
   are permitted; authors should prefer an explicit `unknown` when appropriate.

3. `milestone-timeline`:

   ```json
   {
     "items": [
       {
         "label": "Qualification",
         "date": null,
         "status": "unknown",
         "detail": "Evidence unavailable",
         "source_ids": []
       }
     ]
   }
   ```

   Status is `complete`, `pending`, or `unknown`. Dates may be `null`.

4. `dependency-diagram`:

   ```json
   {
     "nodes": [
       { "id": "a", "label": "Observation", "status": "unknown" },
       { "id": "b", "label": "Qualification", "status": "pending" }
     ],
     "edges": [{ "from": "a", "to": "b", "label": "Evidence required" }]
   }
   ```

   Node status uses the same three values as milestones. Nodes have unique IDs;
   edges must connect existing distinct nodes and duplicate directed edges are
   rejected. Relationships describe report data, not an executable workflow.

5. `metric-chart`:

   ```json
   {
     "unit": "example items",
     "label": "Synthetic example count",
     "points": [
       { "label": "Period 1", "value": 3 },
       { "label": "Period 2", "value": null }
     ],
     "illustrative": true
   }
   ```

   Version 1 supports finite nonnegative values from 0 to 1e12 and null for
   missing observations. Negative values are rejected rather than misleadingly
   drawn as positive bars. `illustrative` must be a boolean and agree with the
   panel's provenance. The chart must also expose readable values; null is unknown,
   not zero. Use an evidence table for signed data until a signed renderer exists.

6. `artifact-preview`:

   ```json
   {
     "label": "Release inventory",
     "media_type": "text/plain",
     "excerpt": "A bounded literal-text preview.",
     "url": "https://example.org/artifact"
   }
   ```

   `media_type` is restricted to `text/plain`, `text/markdown`, or
   `application/json`; all display as literal text. `url` is optional and only
   links to the source. No HTML rendering, image embedding, script execution,
   iframe, remote download, or generic component registry is supported.

## Limits and safe failure

The validator in [`src/lib/visualReports.js`](../src/lib/visualReports.js) enforces:

- 128 KiB UTF-8 total; 16 projects, 64 sources, 32 panels
- IDs: 1–80 characters, starting with a letter or digit, then letters, digits,
  dots, underscores, colons, or hyphens
- Titles, labels, and authors: 200 characters; source kinds and units: 80
- Summaries, explanation text, and excerpts: 12,000 characters
- Outcome text, table cells, and milestone detail: 2,000 characters
- URLs: 2,048 characters, absolute HTTP(S), no credentials, whitespace, or
  backslashes
- Tables: 12 columns and 200 rows; timelines: 100 milestones
- Diagrams: 40 nodes and 80 edges; metrics: 200 points
- At most 20 safe diagnostics; no unsupported keys or supplied payload text are
  echoed in validation errors

`parseVisualReport(content)` returns `{ recognized, report, errors }`. Ordinary
documents have `recognized: false`; recognized invalid reports have
`recognized: true`, `report: null`, and bounded diagnostics. Callers must not render
partial invalid data. Preserve the text/source fallback, so a future report
version remains inspectable and repairable without breaking document navigation.
No report-provided string may be passed to `{@html}`, dynamic imports, evaluation,
CSS, HTML attributes controlling behavior, or remote fetching. Only the six fixed
renderers handle validated data. Safe URL validation does not establish that a
destination is trustworthy; it only restricts the link protocol and credentials.

## Illustrative fixture provenance

The bundled example is a fictional release and qualification report. Its
releases, runtime dependency, URLs under `example.test`, dates, and chart values
are illustrative. Every authored panel declares `provenance: illustrative`;
these values are not operational observations or a compatibility baseline for
Agent Nexus.

The outcome **Released; operational qualification pending** demonstrates that a
published release does not verify a deployment. Unknown and unavailable evidence
remain explicit. The separate `reporting-example` project demonstrates chart
values without suggesting a healthy fleet or completed qualification.

## Verification

From `web-ui/`:

```sh
pnpm exec vitest run tests/unit/visualReports.test.js
pnpm exec playwright test tests/e2e/visual-reports.spec.js
```

Unit coverage validates all component shapes, reference integrity, safe links,
limits, adversarial inputs, explicit unknown/unavailable observations, age-based
freshness, project separation, and fixture round-tripping. Browser coverage
exercises the existing document-read rendering path, evidence/source inspection,
filters, malformed content, and narrow layouts. Full module verification remains
`make -C web-ui check` from the repository root.

## Expressive composition

Agents can choose a layout independently from the evidence-bearing panels. Omit
`layout` for the original two-column report. A layout is a bounded tree with these
nodes (every node accepts optional `span: 1..4` for direct grid children):

- `panel`: `{ "type": "panel", "panel_id": "existing-panel-id" }`
- `stack`: `{ "type": "stack", "children": [...] }`
- `grid`: `{ "type": "grid", "columns": 2, "children": [...] }`; 2, 3 or 4 columns
- `section`: `{ "type": "section", "title": "Heading", "description": "Optional context", "children": [...] }`
- `tabs`: `{ "type": "tabs", "id": "views", "items": [{ "id": "overview", "label": "Overview", "children": [...] }] }`
- `disclosure`: `{ "type": "disclosure", "title": "Details", "open": false, "children": [...] }`

At most 100 nodes, 6 nesting levels, 32 children per container and 8 tabs per group
are accepted. Panel references and tab-group IDs are unique. Referenced panels
must exist; unreferenced panels are appended so layout cannot silently omit
stored evidence. Grids collapse to reading order on narrow screens. Empty nodes
and tabs are removed by project/freshness filters. Tab selection is recorded in
`reportTab.<group-id>` so reload and Back/Forward work. Evidence deep links can
select the relevant tab and open its disclosure. No styles, component names,
actions, expressions, raw markup or executable callbacks can be supplied.

Panels also accept optional app-owned `appearance` tokens (`plain`, `soft`,
`outlined`) and `density` tokens (`compact`, `comfortable`). These vary hierarchy
and spacing without arbitrary CSS. Evidence metadata remains visible in every
surface style. Legacy reports retain their original appearance.

### Additional expressive panels

`metric-strip` accepts 1–6 `items`, each with `label`, string `value`, and `detail`.
Optional `tone` is `neutral`, `positive` or `negative`. Optional `trend` is 2–50
finite numeric observations and requires a descriptive `trend_label`. The
sparkline exposes all values as its accessible description; its magnitude is
relative to its own range, not comparable between metrics.

`callout` accepts `tone` (`info`, `success`, `warning`, `critical`), literal `text`,
and an optional `label`. A tone communicates the author's emphasis, not verified
health. `comparison` accepts 2–4 `items` with `title`, `summary`, `verdict`
(`recommended`, `neutral`, `caution`) and 1–10 `attributes` containing string
`label` and `value`. Recommendations remain authored claims.

### Chart grammar

`chart` uses a strict, data-only subset of Apache ECharts rather than executable
chart code. Its data contains `option`, optional literal `caption` and optional
`palette` (`ocean`, `forest`, `sunset`, `categorical`). Registered chart families
include mixed Cartesian line/area/bar/scatter, pie/donut, heatmap, graph, Sankey
and treemap. Line `areaStyle: {}`, named `stack`, step/smooth lines, horizontal
category axes, multiple series and paired axes compose without bespoke chart
components. For example:

```json
{
  "caption": "Illustrative throughput; null means unknown.",
  "palette": "forest",
  "option": {
    "xAxis": { "type": "category", "data": ["Mon", "Tue", "Wed"] },
    "yAxis": { "type": "value", "name": "Questions" },
    "legend": { "show": true },
    "series": [
      { "type": "bar", "name": "Resolved", "data": [12, 18, 21] },
      { "type": "line", "name": "Remaining", "data": [30, null, 14] }
    ]
  }
}
```

Further expressive options, all validated and rebuilt by the renderer:

- Value axes accept numeric `min`, `max` and `scale: true` (fit the data rather
  than forcing zero), and `axisLabel.formatter` as literal text around one
  `{value}`, such as `"{value}%"` or `"{value} h"`. Time axes accept epoch
  millisecond `min`/`max`. Category axes accept none of these.
- Line, bar and scatter series accept `markLine: { "data": [{ "name": "Target",
"yAxis": 50 }] }` with 1–6 named reference lines. Each line sets exactly one of
  `xAxis`/`yAxis` on a value or time axis. Reference lines are listed in the
  chart's data table and are silent (no interaction).
- Graph series accept up to 12 `categories` (`[{ "name": "Stage" }]`); nodes
  reference one by zero-based `category` index. Categories color nodes and
  appear in the legend.
- The legend defaults to visible only when it distinguishes something: several
  series, a pie, or graph categories. Set `legend.show` to override.

The renderer rebuilds accepted options from a strict allowlist, owns all styling,
uses local SVG rendering, fixes rich-text tooltips, and disables animation.
Unknown fields reject the report with bounded diagnostics rather than silently
ignoring intent. There are no external images, script expressions, dataset
transforms, HTML tooltips, data-view HTML, user callbacks or remote data loaders.
Charts have readable data-table alternatives and preserve unavailable values.
Metric values must be zero or have absolute magnitude from `1e-100` through
`1e12`; nonzero subnormal values are rejected before rendering. Time coordinates
use finite epoch milliseconds within the JavaScript Date range, with the same
nonzero lower bound.
Sankey charts require at least one positive flow; use a callout/table for a
no-flow observation. Treemap internal nodes derive their area from children and
must not supply an explicit `value`. Scalar Cartesian values require exactly
one category axis; use explicit coordinate pairs with two numeric/time axes.
ECharts options outside this documented subset are deliberately unsupported.

Use the exact field whitelist and limits in `src/lib/visualReportCharts.js` and
validate before publishing. The two complete synthetic report examples in
`src/lib/fixtures/expressiveReportExamples.js` demonstrate a swarm observatory
(flow, capacity heatmap, mixed throughput, tabbed decision) and portfolio review
(treemap, scatter, stacked work mix, dependency graph, comparison, methodology).
They contain no live workspace observations or private data. Browser dogfood
round-trips each JSON through the existing document read path and captures real
1440px and 390px renders, with keyboard/history, layout, accessibility and
no-network checks.

Library rationale: ECharts provides operational flow and hierarchy charts along
with Cartesian composition, without requiring React in the Svelte UI. Its
[feature overview](https://echarts.apache.org/en/feature.html) and
[security guidance](https://echarts.apache.org/handbook/en/best-practices/security/)
inform the application-owned adapter. JSON by itself is not a security boundary;
the strict whitelist, bounds and option reconstruction are.

The Go validator, live query parser and checklist/Needs summary parser live in
`contracts/visualreport`; core and CLI consume the same module. Browser and Go
validation are checked against `contracts/fixtures/visual-reports/` by
`make visualreport-check` and CI. This includes decimal/exponent integer spellings,
RFC3339Nano observation times, URL/whitespace boundaries, and longer nested fences.
Work reads apply board/project scope and a 2,000 candidate cap in SQL before
materialization; equal scopes share one read per request. A capped read is marked
truncated, so it does not claim complete coverage.

The Overview uses compact rendering: report title and panels. Project filters,
counts, freshness controls and provenance details are available through its
**Open document** link. The reusable `LiveInitiatives.svelte` expects the shared
`progress.done/total` and `needs[]` projection for the Overview initiatives section.
