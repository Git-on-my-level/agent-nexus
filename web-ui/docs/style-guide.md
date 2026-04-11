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

## Accessibility — Contrast Requirements

All text intended to be read must meet **WCAG 2 AA** contrast thresholds against the surface it sits on:

| Threshold | Minimum ratio | Applies to |
|-----------|---------------|------------|
| Normal text (< 18px / < 14px bold) | **4.5 : 1** | Body, labels, timestamps, metadata, badges |
| Large text (≥ 18px / ≥ 14px bold) | **3 : 1** | Page headings, section headings |

Measured contrast ratios for each text token on the two primary surfaces:

| Token | on `gray-100` (#181a21) | on `gray-50` (#0e1015) | Verdict |
|-------|-------------------------|------------------------|---------|
| `gray-400` (#6b7080) | 3.5 : 1 | 3.9 : 1 | Large text only; disabled/decorative |
| `gray-500` (#8e939c) | 5.6 : 1 | 6.2 : 1 | **AA pass** — minimum for readable text |
| `gray-600` (#9ca1ab) | 6.7 : 1 | 7.3 : 1 | AA pass |
| `gray-700`+ | > 8 : 1 | > 9 : 1 | AA pass |

**Rules:**

1. **All readable text uses `gray-500` minimum.** Labels, timestamps, metadata, badge text, secondary copy, empty-state messages, placeholder-text-that-should-be-read — all must use `text-gray-500` / `text-[var(--ui-text-muted)]` or brighter.
2. **`gray-400` is decorative/disabled only.** Use it for disabled controls, non-essential separators (e.g. `·` dot dividers), priority-dot fills, and text the operator is *not* expected to read. Never pair `text-gray-400` with `text-[11px]` — the combination is unreadable.
3. **Test on `gray-100` (the worse case).** If text passes contrast on `gray-100`, it will also pass on the darker `gray-50`.

## Color System

### Dark Gray Palette (Tailwind Override)

The default Tailwind `gray` scale is overridden in `tailwind.config.cjs` to dark values. The numbering is **inverted** from what you might expect: lower numbers are darker, higher numbers are lighter.

| Token      | Hex       | Usage                                     |
|------------|-----------|-------------------------------------------|
| `gray-50`  | `#0e1015` | Body/page background, inset wells         |
| `gray-100` | `#181a21` | Card/panel surfaces (replaces `bg-white`)  |
| `gray-200` | `#262a33` | Borders, badge backgrounds, button fills   |
| `gray-300` | `#353a45` | Strong borders, active button fills        |
| `gray-400` | `#6b7080` | Disabled text, decorative separators (not readable text) |
| `gray-500` | `#8e939c` | Muted text — minimum for readable content  |
| `gray-600` | `#9ca1ab` | Secondary text                             |
| `gray-700` | `#b4b9c2` | Body text                                  |
| `gray-800` | `#d0d4db` | Strong text, button label text             |
| `gray-900` | `#e2e5eb` | Headings, primary text                     |
| `gray-950` | `#f0f2f5` | Brightest text (rare)                      |

**Key consequence:** `bg-white` is never used. Use `bg-gray-100` for panel surfaces. `text-gray-900` produces near-white text suitable for headings.

**Contrast consequence:** `text-gray-400` does not meet AA for normal-size text on any surface. Any text an operator should read — including "subtle" labels, timestamps, metadata lines, and uppercase section headers — must use `text-gray-500` or brighter.

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
| `--ui-text-muted`     | `#8e939c`   | Muted text (min for readable content) |
| `--ui-text-subtle`    | `#6b7080`   | Disabled/decorative only         |
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

| Category          | Text              | Badge background     |
|-------------------|-------------------|----------------------|
| `action_needed`   | `text-indigo-400` | `bg-indigo-500/10`   |
| `risk_exception`  | `text-amber-400`  | `bg-amber-500/10`    |
| `attention`       | `text-sky-400`    | `bg-sky-500/10`      |

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
| `p3`     | `bg-gray-400`     | `text-gray-500`   |

### Provenance Colors

Provenance indicators (rendered by `ProvenanceBadge`) use dots only — no badge background:

| State            | Dot class          |
|------------------|--------------------|
| Evidence-backed  | `bg-emerald-400`   |
| Inferred         | `bg-amber-400`     |
| Unknown          | `bg-slate-400`     |

### Color Usage Rules

1. **Semantic only.** Color communicates status, urgency, or category. Never use color for decoration or to make a surface "more interesting."
2. **Zero = muted.** Counts and metrics at zero always render in muted or default text (`text-gray-500` / `text-[var(--ui-text-muted)]`). Color appears only when there is a live signal. This applies to column count badges on board views, urgency tally cards, and category counts — all must be neutral gray when zero.
3. **User-defined tags stay neutral.** Labels and tags entered by operators (board labels, topic tags, document tags) always use `bg-[var(--ui-border)] text-[var(--ui-text-muted)]` regardless of their string content — never infer semantic color from tag names.
4. **Background tints are optional emphasis.** Use `color/5` or `color/10` background tints on urgency/status cards only when the count is non-zero and the tint adds meaningful emphasis. Do not tint containers that hold zero-count data.
5. **Status badges ≠ user tags.** Status indicators (`Active`, `Paused`, `Closed`) use semantic color per the Status Colors table. User-defined tags use neutral gray. When they appear side-by-side (e.g. board list rows), place status first, then tags, with a small gap (`gap-1.5`) so the semantic distinction is clear.

## Typography

- **Font:** Inter (loaded via Google Fonts in `app.html`).
- **Base size:** 13px (`font-size: 13px` on body).
- **Line height:** 1.5 (on body).

| Role             | Class                                        |
|------------------|----------------------------------------------|
| Page heading     | `text-lg font-semibold text-gray-900`        |
| Section heading  | `text-[13px] font-semibold text-gray-900`    |
| Body text        | `text-[13px] text-gray-700` or `text-gray-800` |
| Label (uppercase)| `text-[11px] font-medium text-gray-500 uppercase tracking-wide` |
| Muted/secondary  | `text-[13px] text-gray-500`                  |
| Timestamp/meta   | `text-[11px] text-gray-500`                  |
| Disabled/decorative | `text-[13px] text-gray-400` (not at 11px) |

Preferred font sizes: `text-lg`, `text-[13px]`, `text-[12px]`, `text-[11px]`. Avoid Tailwind's `text-sm` / `text-xs` / `text-base` — use explicit pixel sizes for consistency.

**Contrast floor:** At `text-[11px]`, the minimum readable token is `gray-500` (5.4 : 1 on panels). Never combine small size with `gray-400` — the result fails WCAG AA and is genuinely hard to read on dark surfaces. The previous pattern of `text-[11px] text-gray-400` for labels and timestamps must be migrated to `text-gray-500`.

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
- Placeholder text: `placeholder:text-[var(--ui-text-subtle)]` (gray-400 is acceptable for placeholders since they are replaced by user input).

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

### IDs, Hashes, and Ref Metadata

Long identifiers (UUIDs, thread IDs, content hashes) are common in OAR data. Display rules:

1. **Truncate in list contexts.** Show the first 8 characters followed by `…` for UUIDs and hashes. Use `title` attribute or copy-on-click for the full value.
2. **Monospace for IDs.** Use `font-mono text-[11px]` for raw identifiers to distinguish them from prose.
3. **Separate ref links.** When displaying multiple refs on one line, separate them with `·` in `text-[var(--ui-text-subtle)]` or use distinct labeled groups (`Thread`, `Topic`, `Card`). Never concatenate bare ref strings.
4. **Readable metadata on list rows.** In artifact/thread/document list views, metadata lines should use structured labels (`Thread:`, `Topic:`, `Card:`) before each ref link. Avoid dumping raw refs as a run-on string.

### Destructive Actions

Destructive operations (delete, permanently delete, archive) follow escalating prominence:

| Action type | Style | Safeguard |
|-------------|-------|-----------|
| Single-item delete | Ghost or secondary button, `text-red-400` | Inline confirmation or undo toast |
| Single-item permanent delete | `bg-red-500/10 text-red-400 border border-red-500/20` | Inline confirmation |
| Batch destructive (e.g. "Permanently delete all") | Same as single permanent delete, but **disabled by default** | Must require explicit confirmation dialog before execution |

**Rule:** Batch destructive actions that affect multiple resources should never execute on a single click. The button itself is acceptable; the action must gate behind a confirmation modal that names the count and action.

### Interactive Element Nesting

Never nest focusable/clickable elements. A `<button>` inside an `<a>`, or an `<a>` inside a `<button>`, breaks screen reader announcements and causes unpredictable focus behavior. If a list row is clickable as a whole, use a single interactive wrapper and place child controls outside the click target, or use event delegation with `stopPropagation` on nested controls.

## Mobile Patterns

The UI uses a **mobile-first responsive shell**. Breakpoints:

| Breakpoint | Value | Notes |
|---|---|---|
| Mobile | `< 640px` | Bottom nav only, no sidebar |
| Tablet | `640px–1023px` | Bottom nav only, no sidebar |
| Desktop | `≥ 1024px` | Sidebar visible, bottom nav hidden |

### Bottom Navigation Bar (mobile/tablet)

On screens narrower than 1024px a fixed bottom tab bar (`.shell-bottom-nav`) replaces the sidebar for primary navigation. It shows the five primary destinations (Home, Inbox, Topics, Boards, Docs) plus a **More** tab (icon + short label on each).

- `z-index: 20` — below overlays that use higher z-index (for example modals and the command palette), above normal page content.
- Respects `env(safe-area-inset-bottom)` for notched devices.
- `.shell-main-scroll` bottom padding is `5rem` on mobile/tablet to clear the bar; it resets to `2.5rem` at the 1024px breakpoint.

**Do not add a second bottom bar** or position fixed elements at `bottom: 0` without accounting for this bar. Reserve `z-index: 20+` for shell chrome only.

### Mobile Header

The sticky top bar (`.shell-mobile-header`) is minimal: **OAR** wordmark and an identity control (initials + **Switch** or **Sign out**). Secondary destinations (Artifacts, Trash, Access), multi-workspace switching when applicable, and full identity copy live on the **`/more`** page, linked from the bottom tab bar.

### Page Header Toolbars on Mobile

List pages have a `flex flex-wrap items-start justify-between gap-4` header row. On mobile:

- **Hide keyboard-only hints** — the ⌘K shortcut indicator must use `hidden sm:inline-flex`.
- **Hide descriptive subtitles** — the one-liner description below the page heading should use `hidden sm:block` so it doesn't consume 2–3 lines on a 390px screen.
- The action buttons (`Filters`, `New topic`, `Create board`, etc.) wrap naturally and remain visible.

```svelte
<!-- Page heading + description (hide description on mobile) -->
<h1 class="text-lg font-semibold text-gray-900">Topics</h1>
<p class="mt-1 hidden text-[12px] text-gray-500 sm:block">
  Primary organizational surface...
</p>

<!-- ⌘K shortcut — keyboard-only, hide on mobile -->
<span class="hidden items-center gap-1 rounded border border-gray-200 ... sm:inline-flex">
  <kbd>⌘K</kbd>
</span>
```

## Spacing Conventions

- Page padding: handled by `.shell-main-scroll` in `app.css`.
- Between major page sections: `space-y-6` or `space-y-5`.
- Between cards/panels: `space-y-3` or `space-y-4`.
- Inside cards: `px-4 py-3` (content), `px-4 py-2.5` (headers/footers).
- Form field gaps: `gap-2` or `gap-3`.
- Border radius: `rounded-md` for everything. Avoid `rounded-xl` or `rounded-lg`.
- **Bottom clearance on mobile:** reserve `pb-5` (or `5rem`) at the end of scrollable page content — the bottom nav occupies this space.

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

- **No keyboard-only UI on mobile** — `⌘K` shortcut indicators, keyboard hints, and similar controls must use `hidden sm:inline-flex` so they don't waste space on touch devices.
- **No page description on mobile without `hidden sm:block`** — the subtitle copy below a list-page heading (`text-[12px] text-gray-500`) should be hidden on mobile; it crowds the action toolbar.
- **No fixed bottom elements without bottom-nav clearance** — if you add `position: fixed; bottom: 0` at z-index < 20, it will be hidden under the bottom nav. Always account for `5rem` bottom clearance on mobile.
- **No `bg-white`** — always `bg-gray-100` for surfaces.
- **No `text-white` on gray buttons** — gray-900 is the "bright" text; `text-white` is only for accent-colored buttons (`bg-indigo-*`).
- **No `-50` semantic backgrounds** — use `*-500/10` opacity pattern instead.
- **No `-600` or `-700` semantic text** — use `-400` for readability on dark.
- **No deep card nesting** — flatten with dividers.
- **No `rounded-xl`** — use `rounded-md` consistently.
- **No decorative shadows** — shadows are minimal (`--ui-shadow-*` tokens only).
- **No hardcoded light-theme hex values** — use the gray scale or CSS custom properties.
- **No `text-gray-400` for readable text** — gray-400 fails WCAG AA (3.5 : 1 on panels, below 4.5 threshold). Use `gray-500` minimum for anything operators should read. Reserve `gray-400` for disabled controls and decorative separators only.
- **No `text-[11px] text-gray-400`** — this combination (small + low contrast) is the single most common accessibility violation in the codebase. Always use `text-gray-500` or brighter at 11px.
- **No colored zero-count badges** — board column counts, urgency tallies, and category counts at zero must use muted gray, not their semantic color.
- **No nested interactive elements** — `<a>` inside `<button>` or vice versa breaks accessibility. Use event delegation instead.

## Adding New Pages

1. Follow the surface hierarchy: page bg → `bg-gray-100` card → `border-gray-200` dividers.
2. Use the typography scale above for headings, labels, body text.
3. Use the button patterns above — accent for primary actions, secondary for everything else.
4. Keep semantic colors to the opacity-based pattern.
5. **Verify contrast:** all readable text must be `gray-500` or brighter on `gray-100` surfaces. Use the contrast table in the Accessibility section as reference.
6. Maintain compact spacing — prefer `py-2.5` over `py-4`, prefer `text-[13px]` over `text-sm`.
7. Truncate IDs and hashes in list views — show 8 characters max with `title` for the full value.
8. Use `text-[var(--ui-text-muted)]` (not `text-[var(--ui-text-subtle)]`) for secondary metadata that operators need to read.
