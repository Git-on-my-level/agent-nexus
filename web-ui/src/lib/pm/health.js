import { sourceLabel, workFreshness } from "./presentation.js";

/** Coverage of loaded work only; never infer health for unseen connections. */
export function integrationGroups(records, now = Date.now()) {
  const groups = new Map();
  for (const work of records) {
    if (work.source?.authority === "nexus") continue;
    const source = work.source || {};
    const key = JSON.stringify([
      source.authority || "unknown",
      source.connection_id || "unknown",
    ]);
    if (!groups.has(key))
      groups.set(key, {
        key,
        source,
        label: sourceLabel(source),
        items: [],
        counts: { fresh: 0, stale: 0, unknown: 0, error: 0 },
      });
    const group = groups.get(key);
    group.items.push(work);
    group.counts[workFreshness(work, now).key]++;
  }
  return [...groups.values()];
}
