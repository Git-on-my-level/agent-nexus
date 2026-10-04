import { coreClient } from "$lib/coreClient";

/**
 * The attention surface must not hide an obligation on page two. Follow
 * cursors up to a bound; past it, say so instead of claiming completeness.
 */
export async function listAllPages(fetchPage, key, maxPages = 8) {
  const collected = [];
  const archived = new Set();
  let cursor;
  let more = false;
  for (let page = 0; page < maxPages; page += 1) {
    const result = await fetchPage(cursor);
    collected.push(...(Array.isArray(result?.[key]) ? result[key] : []));
    for (const ref of result?.archived_refs || []) archived.add(ref);
    cursor = result?.next_cursor || "";
    more = Boolean(cursor) || result?.has_more === true;
    if (!cursor) break;
  }
  return {
    [key]: collected,
    has_more: more && Boolean(cursor),
    archived_refs: [...archived],
  };
}

/**
 * Everything the Inbox classifies, fetched in parallel. Each source settles
 * on its own so one failing list does not blank the others.
 *
 * @param {{ withHistory?: boolean, client?: object }} [options]
 *   `withHistory: false` skips unread updates. Completed asks remain loaded so
 *   a blocked card does not return to Needs you after its ask is answered.
 *   `client` defaults to the browser core client;
 *   a server load passes its own.
 * @returns {Promise<PromiseSettledResult<any>[]>} decisions, actions, work,
 *   open items, completed items, unread updates (unread updates resolve to
 *   null when skipped)
 */
export async function loadInboxSources({
  withHistory = true,
  client = coreClient,
} = {}) {
  const skipped = Promise.resolve(null);
  const results = await Promise.allSettled([
    listAllPages(
      (cursor) => client.listPmDecisions({ limit: 50, cursor }),
      "items",
    ),
    listAllPages(
      (cursor) => client.listPmActions({ limit: 50, cursor }),
      "items",
    ),
    listAllPages((cursor) => client.listWork({ limit: 50, cursor }), "work"),
    listAllPages(
      (cursor) => client.listInboxItems({ status: "open", limit: 50, cursor }),
      "items",
    ),
    listAllPages(
      (cursor) =>
        client.listInboxItems({ status: "completed", limit: 50, cursor }),
      "items",
    ),
    withHistory ? client.getHomeUnread() : skipped,
  ]);
  const hidden = new Set(results[2]?.value?.archived_refs || []);
  for (const index of [0, 1]) {
    if (results[index].status === "fulfilled" && results[index].value?.items) {
      results[index].value.items = results[index].value.items.filter(
        (item) => !hidden.has(item.work_ref),
      );
    }
  }
  return results;
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
