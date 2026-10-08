import { coreClient } from "$lib/coreClient";

/**
 * The attention surface must not hide an obligation on page two. Follow
 * cursors up to a bound; past it, say so instead of claiming completeness.
 */
export async function listAllPages(fetchPage, key, maxPages = 8, onPage) {
  const collected = [];
  const archived = new Set();
  const requestedCursors = new Set();
  let cursor;
  let more = false;
  for (let page = 0; page < maxPages; page += 1) {
    // A stalled/cyclic cursor cannot reveal another page. Keep what was
    // fetched and report a partial result instead of repeating the requests.
    if (requestedCursors.has(cursor)) break;
    requestedCursors.add(cursor);
    const result = await fetchPage(cursor);
    collected.push(...(Array.isArray(result?.[key]) ? result[key] : []));
    for (const ref of result?.archived_refs || []) archived.add(ref);
    cursor = result?.next_cursor || "";
    more = Boolean(cursor) || result?.has_more === true;
    onPage?.({
      [key]: [...collected],
      has_more: more,
      archived_refs: [...archived],
    });
    if (!cursor) break;
  }
  return {
    [key]: collected,
    has_more: more && Boolean(cursor),
    archived_refs: [...archived],
  };
}

/**
 * Fetch every bounded feed in parallel and publish one settled snapshot.
 * Work and completed asks affect ranking and deduplication, so painting open
 * asks before those feeds finish would insert older rows above the selection.
 * A failed feed still allows the others to render with a partial-result notice.
 */
export async function loadInboxSources({
  withHistory = true,
  client = coreClient,
  onProgress,
} = {}) {
  const pages = (fetchPage, key) => listAllPages(fetchPage, key, 8);
  const results = await Promise.allSettled([
    pages((cursor) => client.listPmDecisions({ limit: 50, cursor }), "items"),
    pages((cursor) => client.listPmActions({ limit: 50, cursor }), "items"),
    pages(
      (cursor) => client.listWork({ limit: 50, cursor, summary: 1 }),
      "work",
    ),
    pages(
      (cursor) => client.listInboxItems({ status: "open", limit: 50, cursor }),
      "items",
    ),
    pages(
      (cursor) =>
        client.listInboxItems({ status: "completed", limit: 50, cursor }),
      "items",
    ),
    withHistory ? client.getHomeUnread() : Promise.resolve(null),
  ]);
  const hidden = new Set(results[2]?.value?.archived_refs || []);
  const visible = results.map((result, index) => {
    if (index > 1 || result.status !== "fulfilled" || !result.value?.items)
      return result;
    return {
      ...result,
      value: {
        ...result.value,
        items: result.value.items.filter((item) => !hidden.has(item.work_ref)),
      },
    };
  });
  onProgress?.(visible);
  return visible;
}

/** Completed rows have their own ids; match their original inbox_item_id too. */
export function mergeInboxItems(openItems = [], completedItems = []) {
  const answered = new Set(
    completedItems.map((item) => item?.inbox_item_id).filter(Boolean),
  );
  return [
    ...new Map(
      [
        ...openItems.filter((item) => !answered.has(item?.id)),
        ...completedItems,
      ]
        .filter((item) => item?.id)
        .map((item) => [item.id, item]),
    ).values(),
  ];
}
