import { createInboxSourceClient } from "$lib/coreClient";
import { isPmNotOnboardedRefusal } from "$lib/pm/onboardingState.js";

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
    has_more: more,
    archived_refs: [...archived],
  };
}

/**
 * Publish after each feed's first page, or 800 ms, whichever comes first.
 * Continue bounded pagination with incremental snapshots, but stop waiting
 * after five seconds. A partial history never admits work-derived rows.
 * `complete` means pagination finished without a cap/cycle/partial marker.
 *
 * The PM feeds are always read, whatever the workspace's PM state says. A PM
 * agent runs on the reader's own computer, so one can be absent while
 * proposals it filed earlier still wait for a yes — and core still lists them
 * and still accepts an answer. Hiding an obligation the Inbox is the only
 * place to answer is worse than two bounded reads, so only the PM
 * *affordances* are gated (see `pm/onboardingState.js`). A refusal is
 * tolerated below.
 */
export async function loadInboxSources({
  withHistory = true,
  client,
  onProgress,
  firstPaintMs = 800,
  deadlineMs = 5_000,
  signal,
} = {}) {
  const controller = new AbortController();
  client ||= createInboxSourceClient(controller.signal);
  const results = Array.from({ length: 6 }, () => ({ status: "pending" }));
  const firstPages = new Set();
  let published = false;
  let stopped = false;
  const snapshot = () => {
    const hidden = new Set(results[2]?.value?.archived_refs || []);
    return results.map((result, index) => {
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
  const publish = () => {
    if (stopped || signal?.aborted) return;
    published = true;
    onProgress?.(snapshot());
  };
  const update = (index, result) => {
    if (stopped) return;
    results[index] = result;
    firstPages.add(index);
    if (published || firstPages.size === results.length) publish();
  };
  // Wrapping each outstanding page also terminates pagination when a client
  // transport does not support cancellation. Late responses are ignored.
  const bounded = (fetchPage) => {
    if (controller.signal.aborted)
      return Promise.reject(controller.signal.reason);
    return new Promise((resolve, reject) => {
      const settle = (callback, value) => {
        controller.signal.removeEventListener("abort", abort);
        callback(value);
      };
      const abort = () => settle(reject, controller.signal.reason);
      controller.signal.addEventListener("abort", abort, { once: true });
      Promise.resolve()
        .then(() => {
          if (controller.signal.aborted) throw controller.signal.reason;
          return fetchPage();
        })
        .then(
          (value) => settle(resolve, value),
          (reason) => settle(reject, reason),
        );
    });
  };
  const pages = (index, fetchPage, key) =>
    listAllPages(
      (cursor) => bounded(() => fetchPage(cursor)),
      key,
      8,
      (value) =>
        update(index, {
          status: "fulfilled",
          value,
          complete: !value.has_more,
        }),
    );
  const cancel = () => controller.abort(new Error("Inbox loading canceled"));
  signal?.addEventListener("abort", cancel, { once: true });
  if (signal?.aborted) cancel();
  const firstPaintTimer = setTimeout(() => {
    if (!published) publish();
  }, firstPaintMs);
  const deadlineTimer = setTimeout(
    () => controller.abort(new Error("Inbox loading timed out")),
    deadlineMs,
  );
  const emptyFeed = () => ({ items: [], has_more: false, archived_refs: [] });
  /*
   * A workspace with no PM onboarded refuses these routes outright. That is
   * an answer — there is nothing there — not a fault the reader can act on,
   * and reporting it as an error would make the Inbox look broken for a
   * feature the workspace simply does not have. Any other failure, including
   * a PM bridge that is merely down, keeps its error.
   */
  const withoutPm = (load) =>
    load().catch((reason) => {
      if (isPmNotOnboardedRefusal(reason)) return emptyFeed();
      throw reason;
    });
  const feeds = [
    () =>
      withoutPm(() =>
        pages(
          0,
          (cursor) => client.listPmDecisions({ limit: 50, cursor }),
          "items",
        ),
      ),
    () =>
      withoutPm(() =>
        pages(
          1,
          (cursor) => client.listPmActions({ limit: 50, cursor }),
          "items",
        ),
      ),
    () =>
      pages(
        2,
        (cursor) => client.listWork({ limit: 50, cursor, summary: 1 }),
        "work",
      ),
    () =>
      pages(
        3,
        (cursor) =>
          client.listInboxItems({ status: "open", limit: 50, cursor }),
        "items",
      ),
    () =>
      pages(
        4,
        (cursor) =>
          client.listInboxItems({ status: "completed", limit: 50, cursor }),
        "items",
      ),
    () => bounded(() => (withHistory ? client.getHomeUnread() : null)),
  ];
  try {
    await Promise.all(
      feeds.map(async (fetchFeed, index) => {
        try {
          const value = await fetchFeed();
          update(index, {
            status: "fulfilled",
            value,
            complete: !value?.has_more,
          });
        } catch (reason) {
          const previous = results[index];
          update(
            index,
            previous.status === "fulfilled"
              ? { ...previous, complete: false, reason }
              : { status: "rejected", reason },
          );
        }
      }),
    );
    return snapshot();
  } finally {
    stopped = true;
    clearTimeout(firstPaintTimer);
    clearTimeout(deadlineTimer);
    signal?.removeEventListener("abort", cancel);
  }
}

/** Work classification needs complete ask history, including later pages. */
export function hasCompleteInboxHistory(results) {
  return [3, 4].every(
    (index) =>
      results[index]?.status === "fulfilled" &&
      results[index].complete === true,
  );
}

/** A partial read adds knowledge; only a complete read may remove records. */
export function mergeInboxSnapshot(previous, incoming, complete, key = "id") {
  if (complete) return incoming;
  return [
    ...new Map(
      [...previous, ...incoming].map((item) => [item[key], item]),
    ).values(),
  ];
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
