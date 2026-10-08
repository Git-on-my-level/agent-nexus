/**
 * The one urgent band at the top of the Overview.
 *
 * It answers a single question — "is anything waiting on me, anywhere" — and
 * it answers it across every workspace the reader can reach, because a CEO
 * with four workspaces should not have to visit four dashboards to find out.
 *
 * Two kinds of row, in this order:
 *
 * 1. **Open asks for the reader.** Something is waiting for an answer.
 * 2. **Critical initiatives.** Blocked, at risk or stale: nothing is waiting
 *    for an answer, but work has stopped moving.
 *
 * Asks come first because an ask is a thing the reader can finish in a minute;
 * a stalled initiative is a thing they have to decide about.
 *
 * ## Reading other workspaces
 *
 * The browser talks to core through a same-origin proxy that routes on two
 * headers, so a per-workspace client is a client with different headers — no
 * second session, no second login. That is what "fan out with existing
 * per-workspace sessions" means here. On a self-hosted single-workspace
 * deployment the catalog has one entry and the fan-out is a no-op.
 *
 * Every read settles on its own: a workspace the reader has lost access to
 * contributes a quiet "unavailable" note, never a blank band. The band says
 * how many workspaces it could not read rather than pretending it saw them.
 */

import { needsAttention } from "./workSummary.js";

const asText = (value) => String(value ?? "").trim();

/** One workspace's open asks, read at most this deep. */
export const OPEN_ASKS_LIMIT = 20;

/**
 * Rows from the Overview snapshot's `needs_you`, which core already scoped to
 * the reader. This is the current workspace's contribution and it costs
 * nothing: the Overview read it with everything else.
 *
 * @param {{ status?: string, rows?: object[], count?: number, href?: string, truncated?: boolean, message?: string }} needsYou
 * @param {{ slug?: string, organizationSlug?: string, label?: string }} workspace
 */
export function asksFromSnapshot(needsYou, workspace = {}) {
  if (!needsYou || needsYou.status !== "ok") {
    return {
      status: "unavailable",
      workspace,
      message: asText(needsYou?.message) || "Open asks could not be read.",
      rows: [],
      count: 0,
      truncated: false,
    };
  }
  const rows = (Array.isArray(needsYou.rows) ? needsYou.rows : []).flatMap(
    (row) => {
      const title = asText(row?.title);
      if (!title) return [];
      return [
        {
          id: asText(row?.id) || title,
          title,
          source: asText(row?.source),
          href: asText(row?.href) || asText(needsYou.href),
          workspace,
        },
      ];
    },
  );
  return {
    status: "ok",
    workspace,
    rows,
    count: Number(needsYou.count) || rows.length,
    truncated: needsYou.truncated === true,
    href: asText(needsYou.href),
  };
}

/**
 * One other workspace's open asks, from the cheapest read available.
 *
 * `openAsks` is the per-workspace endpoint when core exposes one; otherwise
 * one page of open Inbox items is the fallback — a single request, no
 * pagination, which is what keeps a fan-out across six workspaces to six
 * requests.
 *
 * @param {object} client a core client bound to that workspace
 * @param {{ slug?: string, organizationSlug?: string, label?: string }} workspace
 * @param {{ limit?: number }} [options]
 */
export async function readWorkspaceOpenAsks(
  client,
  workspace,
  { limit = OPEN_ASKS_LIMIT } = {},
) {
  try {
    if (typeof client?.getOpenAsks === "function") {
      const result = await client.getOpenAsks({ limit });
      return normalizeAskRows(
        Array.isArray(result?.items) ? result.items : result?.rows,
        workspace,
        { count: result?.count, truncated: result?.has_more },
      );
    }
    const result = await client.listInboxItems({ status: "open", limit });
    const items = Array.isArray(result?.items) ? result.items : [];
    return normalizeAskRows(items, workspace, {
      count: items.length,
      truncated: Boolean(result?.next_cursor || result?.has_more),
    });
  } catch (error) {
    return {
      status: "unavailable",
      workspace,
      message:
        error instanceof Error && error.message
          ? error.message
          : "This workspace could not be read.",
      rows: [],
      count: 0,
      truncated: false,
    };
  }
}

function normalizeAskRows(items, workspace, { count, truncated } = {}) {
  const rows = (Array.isArray(items) ? items : []).flatMap((item) => {
    const title =
      asText(item?.title) || asText(item?.summary) || asText(item?.body);
    if (!title) return [];
    const id = asText(item?.id) || title;
    return [
      {
        id,
        title,
        source: asText(item?.source) || asText(item?.category),
        href: asText(item?.href) || `/inbox/${encodeURIComponent(id)}`,
        workspace,
      },
    ];
  });
  return {
    status: "ok",
    workspace,
    rows,
    count: Number.isFinite(Number(count)) ? Number(count) : rows.length,
    truncated: Boolean(truncated),
    href: "/inbox",
  };
}

/**
 * Card refs an ask row already stands for.
 *
 * Core's `needs_you` mixes decisions, open asks and blocked work, so a blocked
 * initiative can arrive as an ask row *and* as a critical initiative — the
 * same card, twice, three rows apart in a band whose whole claim is that it
 * says what needs doing once. The ask row wins: it is the thing that can be
 * finished, and it names the action rather than the state.
 *
 * Rows carry an id and an href rather than a ref, so both are read: an id like
 * `task:card:rollback-wording` and an href like `/tasks/rollback-wording` both
 * point at `card:rollback-wording`.
 *
 * @param {Array<{id?: string, href?: string}>} rows
 * @returns {Set<string>} card refs, and bare handles for href-only matches
 */
export function refsCoveredByAsks(rows = []) {
  const covered = new Set();
  for (const row of Array.isArray(rows) ? rows : []) {
    const id = asText(row?.id);
    const typed = /card:([A-Za-z0-9][A-Za-z0-9._-]*)/.exec(id);
    if (typed) {
      covered.add(`card:${typed[1]}`);
      covered.add(typed[1]);
    }
    const href = asText(row?.href);
    const path = /\/tasks\/([^/?#]+)/.exec(href);
    if (path) {
      const handle = decodeURIComponent(path[1]);
      covered.add(handle);
      covered.add(handle.startsWith("card:") ? handle : `card:${handle}`);
    }
  }
  return covered;
}

/**
 * Critical initiatives, worst first, from cards already built for the grid.
 *
 * The band reuses the cards rather than re-deriving status: one vocabulary,
 * one sort, and a card and its band row cannot say different things.
 *
 * @param {object[]} cards `workSummaryCards` output
 * @param {Set<string>} [covered] refs an ask row already stands for
 */
export function criticalInitiatives(cards = [], covered = new Set()) {
  return (Array.isArray(cards) ? cards : [])
    .filter((card) => needsAttention(card?.summary))
    .filter((card) => !covered.has(asText(card?.ref)))
    .sort((a, b) => a.rank - b.rank);
}

/**
 * The band.
 *
 * @param {{
 *   asks?: Array<{status: string, rows?: object[], count?: number, truncated?: boolean, workspace?: object, message?: string}>,
 *   cards?: object[],
 *   notCovered?: Array<{slug?: string, label?: string}>,
 *   askLimit?: number,
 *   initiativeLimit?: number,
 * }} input
 */
export function urgentBandModel({
  asks = [],
  cards = [],
  notCovered = [],
  askLimit = 6,
  initiativeLimit = 6,
} = {}) {
  const reads = Array.isArray(asks) ? asks : [];
  const ok = reads.filter((read) => read?.status === "ok");
  const unavailable = reads.filter((read) => read?.status !== "ok");

  const askRows = ok.flatMap((read) => read.rows ?? []);
  const askCount = ok.reduce(
    (total, read) => total + (Number(read.count) || 0),
    0,
  );
  const askTruncated =
    ok.some((read) => read.truncated) || askRows.length > askLimit;

  // An initiative already named by an ask row is not listed again below it.
  const critical = criticalInitiatives(cards, refsCoveredByAsks(askRows));

  return {
    asks: {
      rows: askRows.slice(0, askLimit),
      count: askCount,
      truncated: askTruncated,
      /** Workspaces beyond the current one that contributed a row. */
      workspaces: new Set(
        askRows.map((row) => asText(row.workspace?.slug)).filter(Boolean),
      ).size,
    },
    initiatives: {
      rows: critical.slice(0, initiativeLimit),
      count: critical.length,
      truncated: critical.length > initiativeLimit,
    },
    unavailable: unavailable.map((read) => ({
      workspace: read?.workspace ?? {},
      message: asText(read?.message) || "This workspace could not be read.",
    })),
    /*
     * Workspaces this browser has no session for. Not a failure — hosted
     * writes a session per workspace, and the viewer has simply not opened
     * these — so the band states its coverage rather than reporting an
     * outage. Both lists exist because they want different words: one is
     * "not included", the other is "tried and could not".
     */
    notCovered: (Array.isArray(notCovered) ? notCovered : []).map(
      (workspace) => ({
        slug: asText(workspace?.slug),
        label: asText(workspace?.label) || asText(workspace?.slug),
      }),
    ),
    get empty() {
      return askCount === 0 && critical.length === 0;
    },
  };
}
