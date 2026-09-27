import { coreClient } from "$lib/coreClient";

/**
 * The attention surface must not hide an obligation on page two. Follow
 * cursors up to a bound; past it, say so instead of claiming completeness.
 */
export async function listAllPages(fetchPage, key, maxPages = 8) {
  const collected = [];
  let cursor;
  let more = false;
  for (let page = 0; page < maxPages; page += 1) {
    const result = await fetchPage(cursor);
    collected.push(...(Array.isArray(result?.[key]) ? result[key] : []));
    cursor = result?.next_cursor || "";
    more = Boolean(cursor) || result?.has_more === true;
    if (!cursor) break;
  }
  return { [key]: collected, has_more: more && Boolean(cursor) };
}

/**
 * Everything the Inbox classifies, fetched in parallel. Each source settles
 * on its own so one failing list does not blank the others.
 *
 * @param {{ withHistory?: boolean }} [options] `withHistory: false` skips the
 *   Handled and Watching-only sources (completed items, unread updates); the
 *   sidebar count needs only what can land in Needs you.
 * @returns {Promise<PromiseSettledResult<any>[]>} decisions, actions, work,
 *   open items, completed items, unread updates (the last two resolve to
 *   empty lists when skipped)
 */
export function loadInboxSources({ withHistory = true } = {}) {
  const skipped = Promise.resolve(null);
  return Promise.allSettled([
    listAllPages(
      (cursor) => coreClient.listPmDecisions({ limit: 50, cursor }),
      "items",
    ),
    listAllPages(
      (cursor) => coreClient.listPmActions({ limit: 50, cursor }),
      "items",
    ),
    listAllPages(
      (cursor) => coreClient.listWork({ limit: 50, cursor }),
      "work",
    ),
    coreClient.listInboxItems({ status: "open", limit: 50 }),
    withHistory
      ? coreClient.listInboxItems({ status: "completed", limit: 50 })
      : skipped,
    withHistory ? coreClient.getHomeUnread() : skipped,
  ]);
}

/** Open and completed inbox items merged by id (completed wins). */
export function mergeInboxItems(openItems = [], completedItems = []) {
  return [
    ...new Map(
      [...openItems, ...completedItems]
        .filter((item) => item?.id)
        .map((item) => [item.id, item]),
    ).values(),
  ];
}
