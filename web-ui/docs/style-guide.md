# Agent Nexus UI Style Guide

Reference for visual conventions, color usage, and component patterns.
Follow this guide when adding or modifying UI in the web-ui codebase.

## Design Philosophy

The UI targets a **dark-first, compact, information-dense** aesthetic inspired
by Linear and Slack. Every pixel should earn its place. Avoid decorative
elements, excessive shadows, and nested card hierarchies. Prefer flat surfaces
with subtle borders.

**Core principles:**

- Compact over spacious: tighter padding, smaller type, less vertical waste.
- Flat over layered: single-level panels with dividers, not nested card stacks.
- Monochromatic over colorful: semantic colors only for lifecycle, urgency,
  errors, warnings, and live category signals.
- Readable over flashy: readable text must pass WCAG AA on dark backgrounds.
- Linkable over hidden: operator-visible view state that changes which records
  or panels are shown should default to route/query state when practical.

## Runtime Design Contract

The runtime source of truth is [`src/app.css`](../src/app.css), exposed to
Tailwind by [`tailwind.config.cjs`](../tailwind.config.cjs). The active contract
uses short CSS variables (`--bg`, `--panel`, `--line`, `--fg`, `--accent`, etc.)
and semantic Tailwind keys (`bg-panel`, `border-line`, `text-fg-muted`, etc.).

Do **not** introduce the older `--ui-*` or `gray-*` style-guide vocabulary in new
code. Those names described an abandoned documentation draft and are not exposed
by the current Tailwind theme.

| Purpose | CSS variable | Tailwind key | Value |
| --- | --- | --- | --- |
| Page background | `--bg` | `bg-bg` | `#0b0d12` |
| Muted page/inset surface | `--bg-soft` | `bg-bg-soft` | `#11141b` |
| Panel/card surface | `--panel` | `bg-panel` | `#161922` |
| Hovered panel surface | `--panel-hover` | `bg-panel-hover` | `#1b1f2a` |
| Subtle divider | `--line-subtle` | `border-line-subtle` | `#1e2230` |
| Standard divider | `--line` | `border-line` | `#2a2f3d` |
| Strong divider | `--line-strong` | `border-line-strong` | `#3a4050` |
| Primary text | `--fg` | `text-fg` | `#e8ebf1` |
| Muted readable text | `--fg-muted` | `text-fg-muted` | `#a1a7b4` |
| Subtle/decorative text | `--fg-subtle` | `text-fg-subtle` | `#7b8494` |
| Accent/focus | `--accent` | `text-accent` / `border-accent` | `#22d3ee` |
| Accent hover | `--accent-hover` | `text-accent-hover` | `#67e8f9` |
| Accent solid surface | `--accent-solid` | `bg-accent-solid` | `#155e75` |
| Accent link text | `--accent-text` | `text-accent-text` | `#5eead4` |
| Accent soft surface | `--accent-soft` | `bg-accent-soft` | `rgba(34, 211, 238, 0.12)` |

### Authoring Rule

Prefer semantic Tailwind keys (`bg-panel`, `border-line`, `text-fg-muted`) for
new markup. Use arbitrary `var()` utilities only where Tailwind cannot express
the property, or while touching legacy code that has not yet been migrated. The
unification plan tracks a migration away from mixed idioms.

### Focus rings and `:focus-visible`

- **Base layer:** `app.css` `@layer base` sets the default focus-visible outline
  for interactive elements and native inputs.
- **Text fields:** Use the `.ui-input` primitive (or semantic Tailwind with
  `focus:border-accent`) so the focused border matches `--accent`. Avoid stacking
  redundant `focus:ring-*` utilities on top of `.ui-input:focus`.
- **Custom controls:** For non-native clickable chrome, follow `RefChip` /
  workspace patterns: `focus-visible:outline-none` plus a short
  `focus-visible:ring-2 focus-visible:ring-accent-solid/40` (or equivalent) so
  keyboard users get a visible target.

The current product accent is cyan/teal (`--accent`, `--accent-text`). A future
brand change to indigo must be handled as a visible product change in `app.css`,
`tailwind.config.cjs`, this guide, and `.cursorrules` together.

### Unification Decisions

Use these decisions for the UI/UX unification rollout:

1. Keep the current cyan/teal token contract. Do not combine this migration with
   a brand hue change.
2. Prefer semantic Tailwind keys for all new and touched markup.
3. Do not keep compatibility wrappers for old internal UI components during this
   migration. Replace call sites directly, preserve operator-visible behavior,
   and delete obsolete components/props in the same change.
4. Use CSS primitives (`@layer components`) for pure visual recipes: panels,
   inputs, chips, secondary/ghost controls, inline alerts, and banners.
5. Use Svelte components for structure or behavior: page shells, page headers,
   form shells, unified ref/picker controls, and confirmation helpers.
6. Keep layout shells styling-only. They may own spacing, width, and slots; they
   must not fetch data or own resource semantics.
7. Keep common card and discussion controls visible. Column changes, move
   actions, composer actions, attachments, and discussion disclosure should
   normalize into compact footer/action rows rather than disappearing into menus.

## Accessibility

Readable text must meet **WCAG 2 AA** contrast thresholds against its surface.

| Text kind | Minimum ratio |
| --- | --- |
| Normal text (< 18px / < 14px bold) | 4.5 : 1 |
| Large text (>= 18px / >= 14px bold) | 3 : 1 |

Measured contrast against the two common surfaces:

| Token | on `--panel` (`#161922`) | on `--bg-soft` (`#11141b`) | Use |
| --- | ---: | ---: | --- |
| `--fg-subtle` (`#7b8494`) | 4.4 : 1 | 4.8 : 1 | Borderline; disabled/decorative or larger text |
| `--fg-muted` (`#a1a7b4`) | 7.0 : 1 | 7.6 : 1 | Minimum preferred readable secondary text |
| `--fg` (`#e8ebf1`) | 13.2 : 1 | 14.4 : 1 | Primary text/headings |

**Rules:**

1. Use `text-fg-muted` or brighter for labels, timestamps, metadata, badge text,
   and empty-state copy that operators need to read.
2. Reserve `text-fg-subtle` for disabled controls, decorative separators, or
   non-essential secondary hints.
3. Avoid `text-fg-subtle` at `text-micro` size unless the text is decorative.

## Typography

- **Font:** Inter via `@fontsource/inter`.
- **Base size:** 13px on `body`.
- **Line height:** 1.5 on `body`.

Tailwind font-size keys are defined in `tailwind.config.cjs`:

| Role | Preferred class |
| --- | --- |
| Display | `text-display` |
| Page title | `text-title` or `text-subtitle` depending on density |
| Section heading | `text-meta font-semibold` |
| Body | `text-meta` |
| Compact label / timestamp | `text-micro` |
| Monospace IDs | `font-mono text-micro` |

Prefer the project font-size keys (`text-micro`, `text-meta`, `text-body`,
`text-subtitle`, `text-title`, `text-display`) over Tailwind defaults such as
`text-sm`, `text-xs`, or `text-base`.

## Color Usage

### Semantic Colors

Use semantic colors only when the color carries state. The Tailwind config
currently exposes a deliberately small palette in addition to runtime tokens:
`danger`, `warn`, `ok`, `info`, `orange.400`, and `blue.400`. Existing code also
contains a limited set of opacity utilities for semantic Tailwind defaults; keep
new usage consistent with surrounding files until a shared primitive exists.

| Purpose | Preferred classes |
| --- | --- |
| Error/danger | `bg-danger-soft text-danger-text border-danger/30` |
| Warning | `bg-warn-soft text-warn-text border-warn/30` |
| Success | `bg-ok-soft text-ok-text` |
| Accent/info | `bg-accent-soft text-accent-text` |

### Signal Rules

1. Color communicates status, urgency, category, or action risk. Do not use color
   only to make a surface more interesting.
2. Counts and metrics at zero render muted (`text-fg-muted` or `text-fg-subtle`).
   Apply urgency/category color only when there is a live non-zero signal.
3. Free-form labels stay neutral: `bg-line text-fg-muted`.
4. Lifecycle, provenance, urgency, artifact kind, and inbox category colors are
   domain signals. Keep their mappings local to the existing helper/component
   that owns the domain semantics.

## Layout Patterns

### Surface Hierarchy

```text
Page background (bg-bg)
  -> Panel surface (bg-panel, border border-line, rounded-md)
       -> Dividers (border-line)
       -> Inset controls/wells (bg-bg-soft)
```

Use one panel level with dividers. Avoid cards inside cards unless the inner
surface is a real repeated item, modal, or tool.

### Lists

Use a single bordered container with thin dividers, not individual cards per
item:

```svelte
<div class="space-y-px overflow-hidden rounded-md border border-line bg-panel">
  {#each items as item, i}
    <div class="px-3 py-2.5 hover:bg-panel-hover {i > 0 ? 'border-t border-line' : ''}">
      ...
    </div>
  {/each}
</div>
```

### Forms

- Input/select background: `bg-bg-soft`.
- Borders: `border border-line`.
- Focus: global `:focus-visible` in `app.css` owns the default focus ring.
- Labels: `text-micro font-medium text-fg-muted`.
- Placeholder text: `placeholder:text-fg-subtle`.

```svelte
<label class="text-micro font-medium text-fg-muted">
  Field name
  <input
    class="mt-1 w-full rounded-md border border-line bg-bg-soft px-3 py-1.5 text-meta text-fg placeholder:text-fg-subtle"
  />
</label>
```

## Component Patterns

### Buttons

| Style | Classes |
| --- | --- |
| Primary | `rounded-md bg-panel px-3 py-1.5 text-micro font-medium text-fg hover:bg-line` |
| Accent | `rounded-md bg-accent-solid px-3 py-1.5 text-micro font-medium text-white hover:bg-accent` |
| Secondary | `rounded-md border border-line bg-bg-soft px-3 py-1.5 text-micro font-medium text-fg-muted hover:bg-line-subtle` |
| Ghost | `rounded-md px-3 py-1.5 text-micro font-medium text-fg-muted hover:bg-line-subtle` |

Use accent for save/submit/create actions. Use secondary or ghost for
cancel/reset/filter toggles. Destructive actions use danger text and must be
gated appropriately.

### Badges and Tags

```svelte
<span class="rounded bg-line px-1.5 py-0.5 text-micro font-medium text-fg-muted">
  tag-name
</span>
```

### Cards and Sections

```svelte
<section class="rounded-md border border-line bg-panel">
  <header class="border-b border-line px-4 py-2.5">
    <h2 class="text-meta font-medium text-fg">Section title</h2>
  </header>
  <div class="px-4 py-3">
    <!-- content -->
  </div>
</section>
```

### Notices and Alerts

Until a shared `InlineAlert`/`Banner` primitive exists, prefer these shapes:

```svelte
<div class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text">
  ...
</div>

<div class="rounded-md bg-warn-soft px-3 py-2 text-micro text-warn-text">
  ...
</div>

<div class="rounded-md bg-ok-soft px-3 py-2 text-micro text-ok-text">
  ...
</div>
```

### Hover States

Hover should brighten or raise contrast. On `bg-panel`, use `hover:bg-panel-hover`
or `hover:bg-line-subtle`; on `bg-bg-soft`, use `hover:bg-line-subtle`.

### Links

Internal navigation links that sit inline should use
`text-accent-text hover:text-accent-hover`.

## IDs, Hashes, and Ref Metadata

Long identifiers are common in Agent Nexus data.

1. Truncate in list contexts. Show the first 10 characters followed by `…` for
   UUIDs and hashes. Use `title` or copy-on-click for the full value.
2. Use `font-mono text-micro` for raw identifiers.
3. Separate multiple refs with `·` in `text-fg-subtle` or use distinct labeled
   groups (`Thread`, `Topic`, `Card`).
4. In artifact/thread/document list views, metadata lines should use structured
   labels before each ref link. Avoid dumping raw refs as a run-on string.

## Destructive Actions

Destructive operations follow escalating prominence:

| Action type | Style | Safeguard |
| --- | --- | --- |
| Single-item delete/archive | Ghost or secondary button with `text-danger-text` when destructive | Inline confirmation or undo toast |
| Single-item permanent delete | `bg-danger-soft text-danger-text border border-danger/30` | Inline confirmation |
| Batch destructive | Same as permanent delete, but disabled until valid | Confirmation modal naming count and action |

Batch destructive actions that affect multiple resources must never execute on a
single click.

## Interactive Element Nesting

Never nest focusable/clickable elements. A `<button>` inside an `<a>`, or an
`<a>` inside a `<button>`, breaks screen reader announcements and creates
unpredictable focus behavior. If a row is clickable as a whole, use one
interactive wrapper and place child controls outside the click target, or use
event delegation with `stopPropagation` on nested controls.

## Mobile Patterns

The UI uses a **mobile-first responsive shell**.

| Breakpoint | Value | Notes |
| --- | --- | --- |
| Mobile | `< 640px` | Bottom nav only, no sidebar |
| Tablet | `640px-1023px` | Bottom nav only, no sidebar |
| Desktop | `>= 1024px` | Sidebar visible, bottom nav hidden |

### Bottom Navigation Bar

On screens narrower than 1024px, `.shell-bottom-nav` replaces the sidebar for
primary navigation. It sits above normal page content and below overlays such as
modals and the command palette.

- Respect `env(safe-area-inset-bottom)`.
- `.shell-main-scroll` needs bottom padding on mobile/tablet to clear the bar.
- Do not add a second bottom bar or fixed bottom element without accounting for
  the shell nav.

### Page Header Toolbars

Mobile is for triage. Show work first, chrome second, explanatory copy last.

- Hide keyboard-only hints with `hidden sm:inline-flex`.
- Hide descriptive subtitles with `hidden sm:block`.
- Keep page headers to one compact row when possible.
- Keep safety-critical copy in confirmations and destructive modals.

```svelte
<h1 class="text-subtitle font-semibold text-fg">Topics</h1>
<p class="mt-1 hidden text-micro text-fg-muted sm:block">
  Primary organizational surface...
</p>

<span class="hidden items-center gap-1 rounded border border-line sm:inline-flex">
  <kbd>⌘K</kbd>
</span>
```

## Spacing

- Page padding: handled by `.shell-main-scroll` in `app.css`.
- Between major page sections: `space-y-6` or `space-y-5`.
- Between panels: `space-y-3` or `space-y-4`.
- Inside panels: `px-4 py-3` for content, `px-4 py-2.5` for headers/footers.
- Form field gaps: `gap-2` or `gap-3`.
- Border radius: `rounded-md` by default. Avoid `rounded-xl`.
- Bottom clearance on mobile: reserve enough padding for the bottom nav.

## Data Relationships & Navigation

**Thread vs topic:** Use **topic** as the default operator-facing noun for the
primary work item. **Thread** is correct for the timeline primitive,
`thread:` / `thread_id` diagnostics, read-only `/threads` inspection, or when
the UI explicitly means a backing stream.

### Parent/Owner Links

Every detail page must clearly show its parent entity. Use a labeled inline link
in the header area:

```svelte
<span class="text-fg-subtle">Topic</span>
<a class="ml-1 text-accent-text hover:text-accent-hover" href={topicHref}>
  {topicTitle}
</a>
```

Examples: Board -> primary topic context, Document -> owning topic, Artifact ->
topic context. Prefer **Backing thread** only when the target is explicitly
thread-indexed inspection.

### Navigational Symmetry

If entity A links to entity B, operators should be able to navigate from B back
to A with equal prominence. If A owns B, A's detail page should list its B
children in a dedicated panel.

### Attribution In Aggregated Lists

When a page rolls up items from multiple child entities, each item must identify
its source. Never show a flat list where operators cannot tell which parent each
item belongs to.

### Relationship Labels

| Relationship | Label | Where |
| --- | --- | --- |
| Board -> topic | `Topic` | Board header context line |
| Document -> topic | `Topic` | Document header |
| Artifact -> topic | `Topic` | Artifact header |
| Topic detail -> owned boards | Section: `Owned by this topic` | Topic boards panel |
| Topic detail -> board cards | Section: `Appears as card on` | Topic boards panel |
| List item -> topic | `Topic: {title or id}` | List row metadata |
| Diagnostic / `thread:` target | `Backing thread` or `Thread` | Explicitly thread-scoped surfaces |

## Anti-Patterns

- No keyboard-only UI on mobile.
- No page description on mobile without `hidden sm:block`.
- No fixed bottom elements without bottom-nav clearance.
- No `bg-white` or light-theme surface assumptions.
- No arbitrary `gray-*` classes in new code; they are not part of the active
  Tailwind theme.
- No `--ui-*` variables in new code or docs.
- No color on zero-count badges.
- No deep card nesting.
- No `rounded-xl` unless a local component explicitly requires it.
- No decorative shadows beyond the shell/menu/modal shadow tokens.
- No nested interactive elements.

## Adding New Pages

1. Follow the surface hierarchy: `bg-bg` page -> `bg-panel` panel -> `border-line`
   dividers.
2. Use the project typography scale.
3. Use semantic Tailwind token keys for colors.
4. Keep semantic colors tied to real status, urgency, category, or action risk.
5. Verify contrast: readable text should be `text-fg-muted` or brighter.
6. Maintain compact spacing.
7. Truncate IDs and hashes in list views.
8. Use `text-fg-muted` for secondary metadata operators need to read.
