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
  Work Order
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
