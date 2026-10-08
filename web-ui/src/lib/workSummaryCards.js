/**
 * Which cards a card grid shows, and in what order.
 *
 * Selection and ordering only: every value a card *renders* comes from
 * `workSummary.js` and is drawn by `WorkSummary.svelte`. This decides which
 * block a card belongs to and which card is worse than which, because a card,
 * a brief row and a page header must not disagree about that — the Overview's
 * whole job is to put what is wrong at the top.
 *
 * The one thing it does rather than shows raw is the description: `summary`
 * on a card is authored markdown, so an excerpt that sliced the source read
 * `**Goal:** ship the…`.
 */

import { markdownExcerpt } from "./markdown.js";
import { hasHint, workProse, workSummaryModel } from "./workSummary.js";

const asText = (value) => String(value ?? "").trim();

/**
 * Which block of a card grid a card belongs to.
 *
 * Finished and planless cards are real but are not what a dashboard is for,
 * so they collapse at the bottom instead of pushing live work down.
 */
export const CARD_GROUPS = Object.freeze({
  ATTENTION: "attention",
  DONE: "done",
  NO_PLAN: "no_plan",
});

/**
 * @param {object|null} summary a `workSummaryModel` result
 *
 * Planless cards are found by the `no_plan` *hint*, not by a status. Core
 * computes a planless card's state from its phase now — "In progress", not
 * "No plan" — so reading the status would have folded nothing away and
 * dropped every planless initiative into the attention grid, which is the
 * noise this fold exists to keep out.
 */
export function cardGroup(summary) {
  const key = asText(summary?.status?.state);
  if (key === "done" || key === "cancelled") return CARD_GROUPS.DONE;
  if (hasHint(summary, "no_plan") || !key) return CARD_GROUPS.NO_PLAN;
  return CARD_GROUPS.ATTENTION;
}

/**
 * One card.
 *
 * @param {object} item a card, work or overview initiative row
 * @param {{ now?: number, href?: (ref: string) => string, excerptLimit?: number }} [options]
 */
export function workSummaryCard(item, options = {}) {
  const { href = () => "", excerptLimit = 120, now = Date.now() } = options;
  const ref = asText(item?.ref);
  const summary = workSummaryModel(item, { now });
  /*
   * Blocked step titles, which the Overview projection carries beside the
   * summary. They are the sharpest thing a card can say, so they get the one
   * pill a card is allowed for something waiting on a person.
   */
  const needs = (Array.isArray(item?.needs) ? item.needs : [])
    .map(asText)
    .filter(Boolean);
  return {
    ref,
    title: asText(item?.title) || ref,
    href: href(ref),
    /*
     * The prose body, as a plain line. `workProse` reads whichever spelling
     * the response used — `summary=1` moves the prose to `summary_text` and
     * leaves the computed object at `summary`.
     */
    excerpt: markdownExcerpt(workProse(item), { limit: excerptLimit }),
    needs,
    summary,
    rank: summary.status?.rank ?? Number.MAX_SAFE_INTEGER,
    group: cardGroup(summary),
  };
}

/**
 * Cards for a grid, worst first.
 *
 * Within a state the incoming order is kept — core sorts by priority or
 * recency, and re-sorting it here would throw that away. The sort is stable,
 * so equal ranks come out in the order they arrived.
 *
 * @param {object[]} items
 * @param {{ now?: number, href?: (ref: string) => string }} [options]
 */
export function workSummaryCards(items = [], options = {}) {
  return (Array.isArray(items) ? items : [])
    .filter((item) => asText(item?.ref))
    .map((item) => workSummaryCard(item, options))
    .sort((a, b) => a.rank - b.rank);
}

/**
 * The cards, split into the blocks a grid renders.
 *
 * @param {object[]} items
 * @param {{ now?: number, href?: (ref: string) => string }} [options]
 */
export function groupedWorkSummaryCards(items = [], options = {}) {
  const cards = workSummaryCards(items, options);
  return {
    attention: cards.filter((card) => card.group === CARD_GROUPS.ATTENTION),
    done: cards.filter((card) => card.group === CARD_GROUPS.DONE),
    noPlan: cards.filter((card) => card.group === CARD_GROUPS.NO_PLAN),
  };
}

/**
 * The single Inbox line the Overview is allowed.
 *
 * The brief is explicit that a dashboard must not restate the Inbox: at most
 * one link saying how much is waiting, plus a pill on the card it belongs to.
 * This replaces a per-item list.
 *
 * It says "items", not "decisions". Core's `needs_you` count mixes decisions
 * with human-assigned and blocked tasks and other Inbox entries, so calling
 * them all decisions would claim something is waiting for an answer when it
 * is a task waiting to be picked up.
 *
 * @param {{ status?: string, count?: number, truncated?: boolean, href?: string }} needsYou
 */
export function inboxWaitingLine(needsYou) {
  if (!needsYou || needsYou.status !== "ok") return null;
  const count = Number(needsYou.count) || 0;
  if (count <= 0) return null;
  const suffix = needsYou.truncated ? "+" : "";
  return {
    count,
    href: asText(needsYou.href),
    label: `${count}${suffix} ${count === 1 ? "item needs" : "items need"} you`,
  };
}
