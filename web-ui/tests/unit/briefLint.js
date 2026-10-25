import { BRIEF_COMPUTED_FIELDS } from "../../src/lib/brief.js";

/**
 * I1: every element showing phase, health, progress or a promise carries
 * `data-source="computed:<field>"` naming a brief field, or sits in an author
 * chip. Labels, empty states and aria text are exempt: the lint reads only
 * visible text nodes, and skips fixed labels marked `data-ui-label`.
 */
const COMPUTED_TEXT = [
  /\b\d+\/\d+\b/, // progress
  /\bpromised\b/i,
  /\b\d+[mhd] (past|overdue|ago)\b/,
  /\bmoved \d+×/,
  /\bnext[: ]/i,
  /\bevidence\b/i,
  /\bblocks \d+/i,
  /\bby (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d+/,
  /\b(stale|stalled|dark|at risk|on track|no plan)\b/i,
];

export function lintComputed(root) {
  const problems = [];
  for (const el of root.querySelectorAll("[data-source]")) {
    const value = el.getAttribute("data-source") ?? "";
    const field = value.startsWith("computed:") ? value.slice(9) : "";
    if (!BRIEF_COMPUTED_FIELDS.has(field))
      problems.push(
        `unknown data-source "${value}" on <${el.tagName.toLowerCase()}>`,
      );
  }
  const walker = root.ownerDocument.createTreeWalker(root, 4 /* SHOW_TEXT */);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = node.textContent.trim();
    if (!text) continue;
    const parent = node.parentElement;
    if (
      parent?.closest(
        "[data-source], [data-author-chip], [data-ui-label], .sr-only, button, textarea, [data-decision-body], [data-decision-title], [data-watch-title]",
      )
    )
      continue;
    if (COMPUTED_TEXT.some((pattern) => pattern.test(text)))
      problems.push(`computed-looking text without data-source: "${text}"`);
  }
  return problems;
}
