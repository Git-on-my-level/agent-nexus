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
 * Everything the Inbox classifies, fetched in parallel. Each source settles
 * on its own so one failing list does not blank the others.
 *
 * @param {{ withHistory?: boolean, client?: object, onProgress?: function }} [options]
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
  onProgress,
} = {}) {
  const skipped = Promise.resolve(null);
  const progress = Array.from({ length: 6 }, () => ({ status: "pending" }));
  const visibleSources = (sources) => {
    const hidden = new Set(sources[2]?.value?.archived_refs || []);
    return sources.map((result, index) => {
      if (index > 1 || result.status !== "fulfilled" || !result.value?.items)
        return { ...result };
      return {
        ...result,
        value: {
          ...result.value,
          items: result.value.items.filter(
            (item) => !hidden.has(item.work_ref),
          ),
        },
      };
    });
  };
  const publish = () => onProgress?.(visibleSources(progress));
  const pages = async (index, fetchPage, key) => {
    try {
      const value = await listAllPages(fetchPage, key, 8, (page) => {
        if (index === 3) {
          progress[index] = { status: "fulfilled", value: page };
          publish();
        }
      });
      progress[index] = { status: "fulfilled", value };
      publish();
      return value;
    } catch (reason) {
      progress[index] = { status: "rejected", reason };
      publish();
      throw reason;
    }
  };
  // On the page, give actionable asks the first database turn. Secondary
  // history/work queries must not keep an already-loaded ask behind a spinner.
  const open = pages(
    3,
    (cursor) => client.listInboxItems({ status: "open", limit: 50, cursor }),
    "items",
  );
  if (onProgress) await open.catch(() => {});
  const results = await Promise.allSettled([
    pages(
      0,
      (cursor) => client.listPmDecisions({ limit: 50, cursor }),
      "items",
    ),
    pages(1, (cursor) => client.listPmActions({ limit: 50, cursor }), "items"),
    pages(2, (cursor) => client.listWork({ limit: 50, cursor }), "work"),
    open,
    pages(
      4,
      (cursor) =>
        client.listInboxItems({ status: "completed", limit: 50, cursor }),
      "items",
    ),
    withHistory ? client.getHomeUnread() : skipped,
  ]);
  return visibleSources(results);
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
