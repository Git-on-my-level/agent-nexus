# Data-only visual reports

Visual reports turn an existing ANX text document into a bounded, inspectable
operator report. Authors supply JSON data; the web UI owns rendering. Core remains
the source of truth, and normal document revisions, authorization, history, and
optimistic concurrency still apply. This is a UI presentation format, not a new
core resource, API contract, agent runtime, or proof of external truth.

## Author and publish a report

1. Read the underlying evidence through authorized tools. Record the actual
   observation time and links; do not replace old observation times with the time
   you generated a report.
2. Write one raw JSON object matching schema version 1 below. Do not wrap it in
   Markdown fences. Keep actual project observations separate from illustrative
   examples, and state the outcome and remaining qualification boundaries.
3. Validate locally before writing to an authorized workspace. From the repository
   root, export and validate the checked-in public example:

   ```sh
   node --input-type=module - <<'JS'
   import { writeFileSync } from 'node:fs';
   import { parseVisualReport } from './web-ui/src/lib/visualReports.js';
   import { visualReportExampleContent } from './web-ui/src/lib/fixtures/visualReportExample.js';
   const parsed = parseVisualReport(visualReportExampleContent);
   if (!parsed.report) throw new Error(parsed.errors.join('\n'));
   writeFileSync('/tmp/anx-report.json', visualReportExampleContent + '\n');
   console.log(`Validated ${parsed.report.panels.length} panels`);
   JS
   ```

   For your own file, use the local validator from the repository root. It exits
   nonzero for malformed, unsupported, or oversized reports and prints bounded JSON
   diagnostics. File and stdin inputs use the same parser as the web UI:

   ```sh
   node web-ui/scripts/validate-visual-report.mjs /tmp/anx-report.json
   cat /tmp/anx-report.json | node web-ui/scripts/validate-visual-report.mjs -
   ```

4. Use the existing `docs create` helper. Replace the topic placeholder with an
   authorized, existing topic. Check the resolved actor/workspace first. A dry run
   builds the request without sending it:

   ```sh
   anx docs create --topic topic:YOUR-TOPIC --title "Project report" --body-file /tmp/anx-report.json --dry-run
   anx --json docs create --topic topic:YOUR-TOPIC --title "Project report" --body-file /tmp/anx-report.json
   ```

   These flags create a normal `content_type: "text"` document whose `content` is
   the JSON **string**. Do not change the outer content type to `structured` or
   store the report as an executable artifact. Save the returned document ref.

5. Read it back, validate the returned text, and open that document in Docs:

   ```sh
   anx docs get doc:YOUR-DOCUMENT --format md > /tmp/anx-report-readback.json
   ```

   ```sh
   node web-ui/scripts/validate-visual-report.mjs /tmp/anx-report-readback.json
   ```

   Run the local validation command against the readback file. In Docs, the
   recognized valid document renders as a visual report; its source remains
   inspectable. Verify the project filter, outcome, panel metadata, and evidence.
   An unsupported or malformed report keeps a safe text/source fallback.

6. Revise the same document rather than creating a second source of truth:

   ```sh
   anx docs revise doc:YOUR-DOCUMENT --body-file /tmp/anx-report.json
   anx docs revise --apply --proposal-id YOUR-PROPOSAL-ID
   ```

   `docs revise` stages a diff proposal by default. Apply only within the caller's
   authorization. An already-authorized immediate revision can instead use
   `anx docs revise doc:YOUR-DOCUMENT --apply --body-file /tmp/anx-report.json`.
   The helper discovers the current base revision; the existing API enforces
   `if_base_revision`. A conflict requires a fresh read and reconciliation, not a
   blind overwrite. Read back and revalidate the new head after a successful write.

Command spellings and transport behavior come from
[`cli/docs/generated/runtime-help.md`](../../cli/docs/generated/runtime-help.md)
(`docs create`, `docs get`, `docs revise`) and the existing CLI helpers. The
ordinary HTTP equivalents remain `POST /docs`, `GET /docs/{document_id}`, and
`POST /docs/{document_id}/revisions`; no shared contract changes are required.

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

## Public fixture provenance

The example was observed through public GitHub metadata at
`2026-10-03T06:38:37Z`. It contains no private operational data.

- [ANX v0.12.0](https://github.com/Git-on-my-level/agent-nexus/releases/tag/v0.12.0):
  stable release, published `2026-10-02T17:11:49Z`; six platform archives plus
  `checksums.txt` listed
- [agentctl v0.12.0](https://github.com/Git-on-my-level/agentctl/releases/tag/v0.12.0):
  stable release, published `2026-10-02T16:17:10Z`; the ANX release notes identify
  this as the tested optional identity and skill-pack baseline
- [agentctl v0.13.0](https://github.com/Git-on-my-level/agentctl/releases/tag/v0.13.0):
  newer stable release, published `2026-10-02T17:42:18Z`; its existence does not
  change the ANX v0.12.0 tested baseline
- [ANX PR #233](https://github.com/Git-on-my-level/agent-nexus/pull/233): merged
  `2026-10-02T17:04:33Z`, release preparation and acceptance ledger

Publication and metadata were checked; artifact bytes, deployment state, and
native runtime behavior were not independently tested for this fixture. Its
outcome is therefore **Released; operational qualification pending**. The separate
`reporting-example` project contains plainly labeled synthetic chart values.

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
