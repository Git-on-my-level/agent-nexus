# OAR UI Style Guide

Reference for visual conventions, color usage, and component patterns.
Follow this guide when adding or modifying UI in the web-ui codebase.

## Design Philosophy

The UI targets a **dark-first, compact, information-dense** aesthetic inspired by Linear and Slack. Every pixel should earn its place. Avoid decorative elements, excessive shadows, and nested card hierarchies. Prefer flat surfaces with subtle borders.

**Core principles:**
- Compact over spacious — tighter padding, smaller type, less vertical waste.
- Flat over layered — single-level cards with dividers, not nested card stacks.
- Monochromatic over colorful — semantic colors only for status/urgency, never decoration.
- Readable over flashy — contrast ratios must pass WCAG AA on dark backgrounds.
- Linkable over hidden — operator-visible view state that changes which records
  or panels are shown SHOULD default to route/query state when practical, so
  refresh and deep links restore the same view. Keep transient drafts and pure
  presentation toggles out of the URL.

## Color System

### Dark Gray Palette (Tailwind Override)

The default Tailwind `gray` scale is overridden in `tailwind.config.cjs` to dark values. The numbering is **inverted** from what you might expect: lower numbers are darker, higher numbers are lighter.

| Token      | Hex       | Usage                                     |
|------------|-----------|-------------------------------------------|
| `gray-50`  | `#0e1015` | Body/page background, inset wells         |
| `gray-100` | `#181a21` | Card/panel surfaces (replaces `bg-white`)  |
| `gray-200` | `#262a33` | Borders, badge backgrounds, button fills   |
| `gray-300` | `#353a45` | Strong borders, active button fills        |
| `gray-400` | `#565b66` | Subtle/disabled text                       |
| `gray-500` | `#7d828e` | Muted text (secondary labels)              |
| `gray-600` | `#9ca1ab` | Secondary text                             |
| `gray-700` | `#b4b9c2` | Body text                                  |
| `gray-800` | `#d0d4db` | Strong text, button label text             |
| `gray-900` | `#e2e5eb` | Headings, primary text                     |
| `gray-950` | `#f0f2f5` | Brightest text (rare)                      |

**Key consequence:** `bg-white` is never used. Use `bg-gray-100` for panel surfaces. `text-gray-900` produces near-white text suitable for headings.

### CSS Custom Properties

Global design tokens live in `src/app.css` under `:root`. These power the shell, sidebar, and non-Tailwind styles.

| Variable              | Value       | Purpose                          |
|-----------------------|-------------|----------------------------------|
| `--ui-bg`             | `#0e1015`   | Page background                  |
| `--ui-panel`          | `#181a21`   | Panel/card surface               |
| `--ui-panel-muted`    | `#13151b`   | Muted/inset panel surface        |
| `--ui-border`         | `#262a33`   | Standard border                  |
| `--ui-border-subtle`  | `#1e2129`   | Very subtle border               |
| `--ui-border-strong`  | `#353a45`   | Emphasized border                |
| `--ui-text`           | `#e2e5eb`   | Primary text                     |
| `--ui-text-muted`     | `#7d828e`   | Muted text                       |
| `--ui-text-subtle`    | `#565b66`   | Subtle/disabled text             |
| `--ui-accent`         | `#818cf8`   | Accent color (indigo)            |
| `--ui-accent-strong`  | `#6366f1`   | Strong accent (brand mark, CTAs) |

### Semantic Colors

Semantic colors use Tailwind defaults (not overridden). For dark backgrounds, use **opacity-based backgrounds** and **lightened text**:

| Purpose       | Background        | Text            | Border (if needed)   |
|---------------|-------------------|-----------------|----------------------|
| Error/danger  | `bg-red-500/10`   | `text-red-400`  | `border-red-500/20`  |
| Warning       | `bg-amber-500/10` | `text-amber-400`| `border-amber-500/20`|
| Success       | `bg-emerald-500/10`| `text-emerald-400`| —                  |
| Info/accent   | `bg-indigo-500/10`| `text-indigo-400`| —                   |
| Blue badge    | `bg-blue-500/10`  | `text-blue-400` | —                    |
| Fuchsia badge | `bg-fuchsia-500/10`| `text-fuchsia-400`| —                  |
| Teal badge    | `bg-teal-500/10`  | `text-teal-400` | —                    |
| Purple badge  | `bg-purple-500/10`| `text-purple-400`| —                   |

**Never use** `-50` shade backgrounds (e.g. `bg-red-50`) or `-600`/`-700` shade text for semantic colors. Those are calibrated for light themes and produce poor contrast on dark surfaces.

### Urgency Colors

Urgency escalation follows a fixed hue ladder. Apply these consistently across inbox triage surfaces:

| Level       | Text            | Background (subtle tint) |
|-------------|-----------------|--------------------------|
| Immediate   | `text-red-400`  | `bg-red-500/5`           |
| High        | `text-amber-400`| `bg-amber-500/5`         |
| Normal      | `text-[var(--ui-text)]` | — (no tint)      |

**Rule:** Color both the label *and* the count/number when the count is non-zero. Zero counts stay muted (`text-[var(--ui-text)]`). This prevents visual noise when a queue is empty while still surfacing urgency when it isn't.

### Inbox Category Colors

Inbox categories use a fixed color assignment. Use these whenever a category label or count is rendered as a signal (summary cards, badges, section headers):

| Category              | Text              | Badge background     |
|-----------------------|-------------------|----------------------|
| `intervention_needed` | `text-red-400`    | `bg-red-500/10`      |
| `decision_needed`     | `text-indigo-400` | `bg-indigo-500/10`   |
| `work_item_risk`      | `text-amber-400`  | `bg-amber-500/10`    |
| `stale_topic`         | `text-orange-400` | `bg-orange-500/10`   |
| `document_attention`  | `text-sky-400`    | `bg-sky-500/10`      |

**Rule:** Color the count only when it is non-zero. Zero counts and category labels for empty queues stay muted. Never apply category color decoratively — only when it carries a live signal.

### Artifact Kind Colors

Artifact kind badges use a fixed color assignment from `src/lib/artifactKinds.js`:

| Kind       | Classes                                   |
|------------|-------------------------------------------|
| `receipt`  | `text-emerald-400 bg-emerald-500/10`      |
| `review`   | `text-amber-400 bg-amber-500/10`          |
| `doc`      | `text-fuchsia-400 bg-fuchsia-500/10`      |
| `log`      | `text-teal-400 bg-teal-500/10`            |
| `evidence` | `text-[var(--ui-text-muted)] bg-[var(--ui-border)]` |

### Status Colors

Topic, board, and document status badges follow this mapping:

| Status    | Text                | Background          |
|-----------|---------------------|---------------------|
| `active`  | `text-emerald-400`  | `bg-emerald-500/10` |
| `paused`  | `text-amber-400`    | `bg-amber-500/10`   |
| `closed`  | `text-slate-300`    | `bg-slate-500/10`   |
| `draft`   | `text-amber-400`    | `bg-amber-500/10`   |
| Archived  | `text-amber-400`    | `bg-amber-500/15`   |
| Stale     | `text-red-400`      | `bg-red-500/10`     |

### Priority Colors

Topic priority dots and text labels follow this mapping:

| Priority | Dot class         | Text class        |
|----------|-------------------|-------------------|
| `p0`     | `bg-red-500`      | `text-red-400`    |
| `p1`     | `bg-amber-400`    | `text-amber-400`  |
| `p2`     | `bg-blue-400`     | `text-blue-400`   |
| `p3`     | `bg-gray-300`     | `text-gray-400`   |

### Provenance Colors

Provenance indicators (rendered by `ProvenanceBadge`) use dots only — no badge background:

| State            | Dot class          |
|------------------|--------------------|
| Evidence-backed  | `bg-emerald-400`   |
| Inferred         | `bg-amber-400`     |
| Unknown          | `bg-slate-400`     |

### Color Usage Rules

1. **Semantic only.** Color communicates status, urgency, or category. Never use color for decoration or to make a surface "more interesting."
2. **Zero = muted.** Counts and metrics at zero always render in muted or default text. Color appears only when there is a live signal.
3. **User-defined tags stay neutral.** Labels and tags entered by operators (board labels, topic tags, document tags) always use `bg-[var(--ui-border)] text-[var(--ui-text-muted)]` regardless of their string content — never infer semantic color from tag names.
4. **Background tints are optional emphasis.** Use `color/5` or `color/10` background tints on urgency/status cards only when the count is non-zero and the tint adds meaningful emphasis. Do not tint containers that hold zero-count data.

## Typography

- **Font:** Inter (loaded via Google Fonts in `app.html`).
- **Base size:** 13px (`font-size: 13px` on body).
- **Line height:** 1.5 (on body).

| Role             | Class                                        |
|------------------|----------------------------------------------|
| Page heading     | `text-lg font-semibold text-gray-900`        |
| Section heading  | `text-[13px] font-semibold text-gray-900`    |
| Body text        | `text-[13px] text-gray-700` or `text-gray-800` |
| Label (uppercase)| `text-[11px] font-medium text-gray-400 uppercase tracking-wide` |
| Muted/secondary  | `text-[13px] text-gray-500`                  |
| Timestamp/meta   | `text-[11px] text-gray-400`                  |

Preferred font sizes: `text-lg`, `text-[13px]`, `text-[12px]`, `text-[11px]`. Avoid Tailwind's `text-sm` / `text-xs` / `text-base` — use explicit pixel sizes for consistency.

## Layout Patterns

### Surface Hierarchy

```
Page background (--ui-bg / gray-50)
  └─ Card surface (bg-gray-100, border border-gray-200, rounded-md)
       ├─ Inner section (border-t border-gray-200 for dividers)
       └─ Inset well (bg-gray-50 for inputs, callout boxes)
```

### Lists

Use a single bordered container with thin dividers, not individual cards per item:

```svelte
<div class="space-y-px rounded-md border border-gray-200 bg-gray-100 overflow-hidden">
  {#each items as item, i}
    <div class="px-3 py-2.5 hover:bg-gray-200 {i > 0 ? 'border-t border-gray-200' : ''}">
      ...
    </div>
  {/each}
</div>
```

### Forms

- Input/select background: `bg-gray-50` (darker than card = inset feel).
- Borders: `border border-gray-200`.
- Focus: handled globally in `app.css` (indigo ring).
- Labels: `text-[12px] font-medium text-gray-600`.

```svelte
<label class="text-[12px] font-medium text-gray-600">
  Field name
  <input class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-1.5 text-[13px]" />
</label>
```

## Component Patterns

### Buttons

| Style      | Classes                                                                      |
|------------|-----------------------------------------------------------------------------|
| Primary    | `rounded-md bg-gray-200 px-3 py-1.5 text-[12px] font-medium text-gray-900 hover:bg-gray-300` |
| Accent     | `rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500` |
| Secondary  | `rounded-md border border-gray-200 bg-gray-100 px-3 py-1.5 text-[12px] font-medium text-gray-600 hover:bg-gray-200` |
| Ghost      | `rounded-md px-3 py-1.5 text-[12px] font-medium text-gray-500 hover:bg-gray-200` |

Use **accent** for save/submit/create actions. Use **primary** for prominent navigation (e.g. "Review inbox"). Use **secondary** for cancel/reset/filter toggles.

**Never** use `bg-gray-900 text-white` for buttons — gray-900 is near-white in our palette.

### Badges and Tags

```svelte
<span class="rounded bg-gray-200 px-1.5 py-0.5 text-[11px] font-medium text-gray-600">
  tag-name
</span>
```

For semantic badges, use the opacity-based backgrounds:

```svelte
<span class="rounded px-1.5 py-0.5 text-[11px] font-semibold text-blue-400 bg-blue-500/10">
  Receipt
</span>
```

### Cards and Sections

```svelte
<div class="rounded-md border border-gray-200 bg-gray-100">
  <div class="border-b border-gray-200 px-4 py-2.5">
    <h2 class="text-[13px] font-medium text-gray-900">Section title</h2>
  </div>
  <div class="px-4 py-3">
    <!-- content -->
  </div>
</div>
```

### Notices and Alerts

```svelte
<!-- Error -->
<div class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400">...</div>

<!-- Success -->
<div class="rounded-md bg-emerald-500/10 px-3 py-2 text-[12px] text-emerald-400">...</div>

<!-- Warning -->
<div class="rounded-md bg-amber-500/10 px-3 py-2 text-[12px] text-amber-400">...</div>

<!-- Info -->
<div class="rounded-md bg-indigo-500/10 px-3 py-2 text-[12px] text-indigo-400">...</div>
```

### Hover States

Hover should **brighten** the element, not darken it. On a `bg-gray-100` surface, use `hover:bg-gray-200`.

### Links

Internal navigation links that sit inline: `text-indigo-400 hover:text-indigo-300`.

## Spacing Conventions

- Page padding: handled by `.shell-main-scroll` in `app.css`.
- Between major page sections: `space-y-6` or `space-y-5`.
- Between cards/panels: `space-y-3` or `space-y-4`.
- Inside cards: `px-4 py-3` (content), `px-4 py-2.5` (headers/footers).
- Form field gaps: `gap-2` or `gap-3`.
- Border radius: `rounded-md` for everything. Avoid `rounded-xl` or `rounded-lg`.

## Data Relationships & Navigation

**Thread vs topic:** Use **topic** as the default operator-facing noun for the primary work item (navigation, headers, list rows). **Thread** is correct for the timeline primitive, `thread:` / `thread_id` diagnostics, read-only `/threads` inspection, or when the UI explicitly means a backing stream that is not being presented as a topic.

When building pages that display entities with parent/child or many-to-many relationships, follow these principles to avoid confusing operators:

### Parent/Owner Links

Every detail page must clearly show its parent entity. Use a labeled inline link in the header area:

```svelte
<span class="text-[var(--ui-text-subtle)]">Topic</span>
<a class="ml-1 text-indigo-400 hover:text-indigo-300" href={topicHref}>
  {topicTitle}
</a>
```

Examples: Board → primary topic context, Document → owning topic (or backing-thread link surfaced as topic navigation where applicable), Artifact → topic context. Prefer **Backing thread** (or a neutral **Linked thread** label) only when the target is explicitly thread-indexed inspection, not the topic workspace.

### Navigational Symmetry

If entity A links to entity B, operators should be able to navigate from B back to A with equal prominence. When adding a link from a detail page to a related entity, check whether the reverse direction exists. If A owns B, A's detail page should list its B children in a dedicated panel (not a buried badge).

### Attribution in Aggregated Lists

When a page rolls up items from multiple child entities, each item must identify its source. Never show a flat list where operators cannot tell which parent each item belongs to.

- On list pages: show the owner (e.g., topic badge on each document row).
- On detail pages: filter items by relationship and label sections (e.g., "Owned by this topic" vs "Appears as card on").

### Avoid Duplicate Context

The same relationship should not appear in multiple places on the same page with different labels. If a parent topic (or explicit backing-thread) link is in the header, suppress it from a generic "Linked references" list. Use explicit structural rendering over generic ref dumps.

### Relationship Labels

Use consistent labels for relationship types:

| Relationship | Label | Where |
|---|---|---|
| Board → topic | `Topic` | Board header context line |
| Document → topic | `Topic` | Document header (when linking to the organizational work item) |
| Artifact → topic | `Topic` | Artifact header (same) |
| Topic detail → owned boards | Section: "Owned by this topic" | Topic/boards panel |
| Topic detail → board cards | Section: "Appears as card on" | Topic boards panel |
| List item → topic | `Topic: {title or id}` | List row metadata |
| Diagnostic / `thread:` target | `Backing thread` or `Thread` | Only when the route or ref is explicitly thread-scoped |

### Scope Labels for Counts

When displaying counts that exclude certain items, label them explicitly. Example: card counts on a board exclude the primary thread — label as "N cards by column" rather than ambiguous "N cards".

## Anti-Patterns

- **No `bg-white`** — always `bg-gray-100` for surfaces.
- **No `text-white` on gray buttons** — gray-900 is the "bright" text; `text-white` is only for accent-colored buttons (`bg-indigo-*`).
- **No `-50` semantic backgrounds** — use `*-500/10` opacity pattern instead.
- **No `-600` or `-700` semantic text** — use `-400` for readability on dark.
- **No deep card nesting** — flatten with dividers.
- **No `rounded-xl`** — use `rounded-md` consistently.
- **No decorative shadows** — shadows are minimal (`--ui-shadow-*` tokens only).
- **No hardcoded light-theme hex values** — use the gray scale or CSS custom properties.

## Adding New Pages

1. Follow the surface hierarchy: page bg → `bg-gray-100` card → `border-gray-200` dividers.
2. Use the typography scale above for headings, labels, body text.
3. Use the button patterns above — accent for primary actions, secondary for everything else.
4. Keep semantic colors to the opacity-based pattern.
5. Test that text is readable against dark surfaces (gray-900 text on gray-100 bg).
6. Maintain compact spacing — prefer `py-2.5` over `py-4`, prefer `text-[13px]` over `text-sm`.
