import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { globSync } from "node:fs";
import { describe, expect, it } from "vitest";

/**
 * One renderer for a card's state, enforced.
 *
 * Before `WorkSummary`, six surfaces each decided for themselves what a
 * card's status was: the Tasks table read `work.phase`, the board card read it
 * again and badged `Blocked` on its own, the Overview read `plan_health`, a
 * report panel read `plan_state.health`, and the task header printed the
 * stored phase. The same task could read In progress in the table, Blocked on
 * the board and At risk on the dashboard, all at once and all from one
 * response.
 *
 * So the rule is structural rather than a convention: the computed summary is
 * read in exactly one module, and status is rendered by exactly one component.
 * Every exception below is a phase naming an *action* or a *column* — "Move to
 * In review" is a button, not a claim about what state a card is in.
 */

const root = fileURLToPath(new URL("../../src", import.meta.url));
const relative = (file) => file.slice(root.length - 3).replaceAll("\\", "/");

/**
 * The file's code, without its comments.
 *
 * A comment naming a field is documentation, which is what these rules are
 * for in the first place; matching it would make explaining the rule a
 * violation of it.
 *
 * A block comment only counts when it opens a line. Stripping every opener
 * anywhere was a hole: a string holding one (a glob, a regex) started a
 * comment that ran to the next string holding a closer, and anything in
 * between — a whole status renderer — went unchecked. Every comment in this
 * repo opens its own line, and an anchored match can only strip less.
 */
const read = (file) => {
  const raw = readFileSync(file, "utf8")
    .replaceAll(/^[ \t]*\/\*[\s\S]*?\*\//gm, " ")
    .replaceAll(/^[ \t]*<!--[\s\S]*?-->/gm, " ");
  /*
   * `//` is a comment in script, and ordinary text in markup. Stripping such
   * lines everywhere let a renderer hide behind one in a template, so in a
   * component only the script is swept.
   */
  const scriptEnd = file.endsWith(".svelte")
    ? raw.indexOf("</script>")
    : raw.length;
  if (scriptEnd < 0) return raw;
  return (
    raw.slice(0, scriptEnd).replaceAll(/^\s*\/\/.*$/gm, " ") +
    raw.slice(scriptEnd)
  );
};

const sources = globSync("**/*.{svelte,js}", { cwd: root, withFileTypes: true })
  .filter((entry) => entry.isFile())
  .map((entry) => `${entry.parentPath ?? entry.path}/${entry.name}`)
  .filter((file) => !file.includes("/generated/"))
  .sort();

describe("the computed summary is read in one module", () => {
  /**
   * The fields core computes a card's presentation from. A page that reads one
   * of these directly is deciding for itself what the card's state is.
   */
  const COMPUTED_FIELDS = [
    "work_summary",
    "plan_health",
    "plan_step_digest",
    "status_mismatch",
  ];

  const ALLOWED = new Set([
    // The one reader: it normalizes every spelling into one model.
    "src/lib/workSummary.js",
    // Its back-compatibility reader for a core that computes no summary.
    "src/lib/planHealth.js",
  ]);

  it.each(COMPUTED_FIELDS)("only one module reads %s", (field) => {
    const pattern = new RegExp(`\\b${field}\\b`);
    const offenders = sources
      .filter((file) => pattern.test(read(file)))
      .map(relative)
      .filter((file) => !ALLOWED.has(file));
    expect(offenders).toEqual([]);
  });
});

describe("the vocabulary lives in one place", () => {
  /**
   * A second vocabulary always looks the same: a table mapping card states to
   * display strings.
   *
   * This is the rule the DOM-shaped ones below cannot reach. A presenter does
   * not have to live beside a component — `src/lib` is where this codebase
   * keeps them — and it does not have to be called anything in particular. It
   * does have to decide what `blocked` is *called*, and that decision is a
   * literal in a file, wherever the file is.
   */
  /*
   * The computed health states plus the stored phases, minus the three words
   * that belong to other vocabularies too: `review` is also an ask kind,
   * `cancelled` and `done` are also run states and plan-step statuses, and
   * matching them made unrelated tables into false positives.
   */
  const STATES =
    "in_progress|blocked|at_risk|no_plan|on_track|backlog|stale|ready";
  /*
   * Quoted or bare keys. Not a `new Map([[...]])` of pairs, and not a key
   * computed at runtime — this catches the shape a second vocabulary
   * actually takes, not every shape one could be written in. The rules in
   * the next block are the DOM-side net; between them a presenter has to be
   * deliberately disguised to get through, which is a different problem from
   * one written without noticing.
   */
  const pattern = new RegExp(`["']?\\b(${STATES})\\b["']?\\s*:\\s*["'\`]`, "g");

  const ALLOWED = new Map([
    ["src/lib/workSummary.js", "is the vocabulary: labels, tones and order"],
    [
      "src/lib/planHealth.js",
      "normalizes the legacy spellings into that vocabulary",
    ],
    ["src/lib/pm/presentation.js", "owns the stored phase's names"],
    [
      "src/lib/refResolve.js",
      "names what an external record calls itself — merged, draft, open",
    ],
    [
      "src/lib/inboxDigest.js",
      'names the column a card was moved *to*, as the object of a verb: "moved 2 tasks to review"',
    ],
  ]);

  it("no other module decides what a card state is called", () => {
    const offenders = [];
    for (const file of sources) {
      const name = relative(file);
      if (ALLOWED.has(name)) continue;
      const keys = new Set(
        [...read(file).matchAll(pattern)].map((match) => match[1]),
      );
      // One key is a row, a filter or a fixture; two or more is a table.
      if (keys.size > 1) offenders.push(`${name}: ${[...keys].join(", ")}`);
    }
    expect(offenders).toEqual([]);
  });

  it("would catch a second vocabulary wherever it is written", () => {
    const table =
      'const WORDS = { in_progress: "In progress", blocked: "Blocked" };';
    expect(new Set([...table.matchAll(pattern)].map((m) => m[1])).size).toBe(2);
  });
});

describe("card status is rendered in one component", () => {
  /**
   * What rendering a card's status looks like in markup. Each pattern is
   * something that put a state or a phase on screen before `WorkSummary`.
   */
  const RENDERS_STATUS = [
    { what: "a phase label", re: /\b(phaseLabel|PHASE_LABELS)\b/i },
    {
      what: "a phase through label()",
      re: /\blabel\([^)]*\b(phase|column_key)\b/,
    },
    /*
     * Any health or status attribute, not just `data-health`: a second
     * renderer reborn under `data-card-status` is the same thing wearing a
     * different hook.
     */
    {
      what: "a health or status data attribute",
      re: /data-[\w-]*(health|status)[\w-]*=/,
    },
    /*
     * A phase read off a row, however it is spelled: `work.phase`,
     * `work?.column_key`, `work["phase"]`. Bracket access was a hole — the
     * dotted form alone let a renamed presenter straight through.
     */
    {
      what: "a raw phase",
      re: /\{[^}]*(?:[.?]\s*(?:phase|column_key)\b|\[\s*["'](?:phase|column_key)["']\s*\])/,
    },
    /*
     * The per-page formatters this change deleted, by name. The rules above
     * are the general net; this one makes a straight revert fail rather than
     * quietly reintroducing a second vocabulary under a local helper. A plan
     * step's own status is a different thing and keeps its own name. Matched by
     * stem rather than by exact name: `workStatusText` is the same presenter
     * under a prefix, and a word-boundary anchor let it through.
     */
    {
      what: "a local card-status formatter",
      re: /[\w$]*(?:statusText|phaseText|healthLabel)\s*\(/i,
    },
    /*
     * The two generic helper names the table and the board used, exactly:
     * prefix-tolerant here would catch `eventTypeDotClass` and
     * `receiptStageDotClass`, which colour an event type and a delivery
     * stage — different vocabularies that are nobody's card status.
     */
    {
      what: "a card status dot or tone helper",
      re: /\b(badgeTone|dotClass)\s*\(/i,
    },
  ];

  /**
   * Phases that name an action or a column, not a card's state. Each entry
   * says why, because an entry without a reason is how a second renderer gets
   * back in.
   */
  const ALLOWED = new Map([
    ["src/lib/components/WorkSummary.svelte", "is the one renderer"],
    [
      "src/lib/components/CommandPalette.svelte",
      "names the target of a move command: a button, not a claim about state",
    ],
    [
      "src/routes/o/[organization]/w/[workspace]/tasks/+page.svelte",
      "lists phases as filter options and labels the move confirmations",
    ],
    [
      "src/lib/components/pm/WorkViews.svelte",
      "labels the board's phase columns, which group cards rather than state them",
    ],
    [
      "src/lib/components/pm/DecisionPanel.svelte",
      "names the phase a proposal would write, and the read-back of that write",
    ],
    [
      "src/routes/o/[organization]/w/[workspace]/overview/+page.svelte",
      "labels the columns of a counts-by-phase matrix, not any one card",
    ],
    [
      "src/lib/components/WorkSummaryCard.svelte",
      "mirrors the summary's own state onto the card shell, for the grid's sort border",
    ],
    [
      "src/lib/components/PlanView.svelte",
      "marks a plan step's status (done/active/blocked/not_started), a different vocabulary",
    ],
    [
      "src/lib/components/AnxRefChip.svelte",
      "marks the source status a resolved ref published, not the card's computed state",
    ],
  ]);

  /**
   * Where rendering lives: components and routes, in either language.
   *
   * `.js` as well as `.svelte`, because a presenter moved into a helper
   * beside its component is still a presenter — and that was the other way
   * this guard could be walked around.
   */
  const rendering = (name) =>
    name.startsWith("src/lib/components/") || name.startsWith("src/routes/");

  const offenders = sources
    .filter((file) => rendering(relative(file)))
    .flatMap((file) => {
      const body = read(file);
      const name = relative(file);
      if (ALLOWED.has(name)) return [];
      return RENDERS_STATUS.filter(({ re }) => re.test(body)).map(
        ({ what }) => `${name} renders ${what}`,
      );
    });

  it("no component outside WorkSummary renders a card's status or phase", () => {
    expect(offenders).toEqual([]);
  });

  it("would catch each presenter this change deleted", () => {
    /*
     * A guard whose patterns match nothing is a guard that passes forever.
     * These are the exact lines the migrated surfaces used to carry, so a
     * regression that reintroduces one of them fails here.
     */
    const regressions = [
      // The Tasks table's own status column.
      "<SignalBadge {tone}>{statusText(work)}</SignalBadge>",
      // The board card deciding Blocked for itself.
      '{#if work.phase === "blocked"}',
      // The task header printing the stored phase.
      ">{label(work.phase)}</SignalBadge",
      // The Overview tile's health pill, and the same thing renamed.
      "<span data-health={tile.health.state}></span>",
      "<span data-card-status={tile.health.state}></span>",
      // Bracket access, which the dotted rule alone let through.
      '<span>{work["phase"]}</span>',
      "<span>{work?.column_key}</span>",
      // A presenter renamed and moved into a helper beside its component.
      "const text = workStatusText(work);",
      // A report panel's own phase line.
      '{String(item.phase ?? "").replaceAll("_", " ")}',
      // The ⌘K result subtitle.
      "subtitle: [phaseLabel(work.phase)].join()",
    ];
    for (const line of regressions) {
      expect(
        RENDERS_STATUS.some(({ re }) => re.test(line)),
        `no rule catches: ${line}`,
      ).toBe(true);
    }
  });

  it("every allowed exception is still a real file", () => {
    // An allowlist entry for a file that no longer exists is an exemption
    // nobody is checking.
    const present = new Set(sources.map(relative));
    for (const name of ALLOWED.keys()) {
      expect(present.has(name), `${name} is allowlisted but missing`).toBe(
        true,
      );
    }
  });
});
