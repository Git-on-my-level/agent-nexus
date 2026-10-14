/**
 * Pure model for the ⌘K palette: fuzzy matching, ranking and the command
 * lists. The component owns fetching, focus and running a command; every
 * command here only describes itself.
 *
 * A command: `{ id, group, label, keywords?, shortcut?: string[], icon?,
 * run?: () => unknown, page?: string, hint? }`. A command with `page` opens a
 * sub-list (Move to…, Assign to…) instead of running.
 */

/**
 * Subsequence match with bonuses for word starts and runs, like the
 * palettes operators already know. `null` when the query does not match.
 * Spaces in the query are ignored, so "assign leo" matches
 * "Assign to Leo Park".
 */
export function fuzzyScore(query, text) {
  const q = String(query ?? "")
    .toLowerCase()
    .replace(/\s+/g, "");
  const t = String(text ?? "").toLowerCase();
  if (!q) return 0;
  if (!t) return null;
  // Two walks: one that jumps to word starts when it can ("il" → "Initiative
  // List"), one plain greedy walk that always finds a match if one exists.
  const scores = [walk(q, t, true), walk(q, t, false)].filter(
    (value) => value !== null,
  );
  return scores.length ? Math.max(...scores) : null;
}

const WORD_BREAK = /[\s\-_/·.:,()]/;
function isWordStart(t, i) {
  return i === 0 || WORD_BREAK.test(t[i - 1]);
}

function walk(q, t, preferWordStart) {
  let score = 0;
  let ti = 0;
  let run = 0;
  let first = -1;
  for (let qi = 0; qi < q.length; qi += 1) {
    const ch = q[qi];
    let found = t[ti] === ch ? ti : -1;
    if (found < 0) {
      let any = -1;
      for (let i = ti; i < t.length; i += 1) {
        if (t[i] !== ch) continue;
        if (any < 0) any = i;
        if (!preferWordStart || isWordStart(t, i)) {
          found = i;
          break;
        }
      }
      if (found < 0) found = any;
    }
    if (found < 0) return null;
    if (first < 0) first = found;
    if (found === ti && qi > 0) {
      run += 1;
      score += 3 + run;
    } else {
      run = 0;
      score += 1 - Math.min(2, (found - ti) * 0.05);
    }
    if (isWordStart(t, found)) score += 5;
    ti = found + 1;
  }
  if (first === 0) score += 6;
  return score - t.length * 0.02;
}

/** Best score of a command against the query, over its label and keywords. */
export function commandScore(command, query) {
  const label = fuzzyScore(query, command?.label);
  const keywords = (Array.isArray(command?.keywords) ? command.keywords : [])
    .map((word) => fuzzyScore(query, word))
    .filter((value) => value !== null)
    .map((value) => value * 0.8);
  const all = [label, ...keywords].filter((value) => value !== null);
  return all.length ? Math.max(...all) : null;
}

/**
 * Filter and order commands for a query. Group order is the order groups
 * first appear in `commands`; within a group, best match first (stable).
 * An empty query keeps everything in its given order.
 */
export function rankCommands(commands, query) {
  const list = Array.isArray(commands) ? commands : [];
  const trimmed = String(query ?? "").trim();
  if (!trimmed) return list;
  const groupOrder = [];
  const scored = [];
  list.forEach((command, index) => {
    const score = commandScore(command, trimmed);
    if (score === null) return;
    if (!groupOrder.includes(command.group)) groupOrder.push(command.group);
    scored.push({ command, score, index });
  });
  return scored
    .sort(
      (a, b) =>
        groupOrder.indexOf(a.command.group) -
          groupOrder.indexOf(b.command.group) ||
        b.score - a.score ||
        a.index - b.index,
    )
    .map((entry) => entry.command);
}

/** "Go to" destinations. Shortcuts listed here are the ones the palette binds. */
export const GO_TO_SHORTCUTS = {
  d: "/decide",
  w: "/watch",
  i: "/initiatives",
  a: "/agents",
};

/**
 * @param {{
 *   settingsGroups?: object[],
 *   go: (href: string) => void,
 *   mod?: string,
 *   pmVisible?: boolean,
 * }} options `pmVisible` false means no PM agent is connected: Ask is left
 * out, and PM setup (a Settings item) is the way in.
 */
export function goToCommands({
  settingsGroups = [],
  go,
  mod = "⌘",
  pmVisible = true,
}) {
  const commands = [
    {
      id: "go:decide",
      group: "Go to",
      label: "Decide",
      keywords: ["inbox", "needs you", "attention", "waiting", "approve"],
      shortcut: ["G", "D"],
      icon: "inbox",
      run: () => go("/decide"),
    },
    {
      id: "go:watch",
      group: "Go to",
      label: "Watch",
      keywords: ["outcomes", "home", "overview", "dashboard"],
      shortcut: ["G", "W"],
      icon: "overview",
      run: () => go("/watch"),
    },
  ];
  if (pmVisible) {
    commands.push({
      id: "go:pm",
      group: "Go to",
      label: "Ask",
      keywords: ["pm", "chat", "ask pm"],
      shortcut: [mod, "J"],
      icon: "askPm",
      run: () => go("/pm"),
    });
  }
  for (const group of settingsGroups) {
    for (const item of group.items ?? []) {
      const entry = {
        id: `go:${item.href}`,
        group: "Go to",
        label: item.label,
        hint: group.label,
        keywords: [group.label, item.hint].filter(Boolean),
        icon: item.icon,
        run: () => go(item.href),
      };
      if (item.href === "/initiatives") {
        entry.shortcut = ["G", "I"];
        entry.keywords.push("work", "table");
      } else if (item.href === "/agents") {
        entry.shortcut = ["G", "A"];
        entry.keywords.push("hosts", "working", "waiting", "stale");
      } else if (item.href === "/docs") {
        entry.keywords.push("documents");
      }
      commands.push(entry);
    }
  }
  return commands;
}
