import { rosterSummary } from "$lib/agentPresence.js";
import { filterTopLevelDocuments } from "$lib/documentVisibility.js";
import {
  buildInboxRows,
  filterMailbox,
  formatWait,
  inboxRowBadge,
  rowWaitMs,
} from "$lib/inboxMailbox.js";
import { loadInboxSources } from "$lib/inboxSources.js";
import {
  dedupeWorkBySource,
  label,
  PHASES,
  sourceLabel,
  taskDetailPath,
  workFreshness,
  workKey,
} from "$lib/pm/presentation.js";
import { resourceRouteSegment } from "$lib/resourceIdentity.js";
import {
  parseVisualReport,
  visualReportContentText,
} from "$lib/visualReports.js";

/** `GET /work` page size (contract maximum) and how many rows Overview will read. */
export const WORK_PAGE_LIMIT = 200;
export const WORK_ROW_CAP = 2000;
/** Full `human=1` rescans follow live events at most this often. */
export const HUMAN_LIVE_REFRESH_MS = 10_000;

/**
 * Delay before the next full human-filter rescan. `0` means run now.
 * @param {number} now
 * @param {number} lastAt
 * @param {number} [interval]
 */
export function humanLiveRefreshDelay(
  now,
  lastAt,
  interval = HUMAN_LIVE_REFRESH_MS,
) {
  const elapsed = Number(now) - Number(lastAt || 0);
  if (!Number.isFinite(elapsed) || elapsed >= interval) return 0;
  return interval - elapsed;
}
/** Most recently updated documents considered for a visual report. */
export const DOC_SCAN_CAP = 20;
export const PREVIEW_LIMIT = 5;

const SOURCE_ORDER = ["nexus", "github", "multica", "git", "ssh_git"];
const FRESHNESS_BUCKETS = [
  { key: "stale", label: "Not checked lately" },
  { key: "unknown", label: "Never checked" },
  { key: "error", label: "Read failing" },
];

/** Title prefix that prefers a visual report on Overview. */
const DASHBOARD_TITLE = /^(?:fleet dashboard|dashboard)\b/i;

export function isPreferredDashboardTitle(title) {
  return DASHBOARD_TITLE.test(String(title ?? "").trim());
}

export function sectionMessage(error, fallback) {
  const raw = error instanceof Error ? error.message : String(error ?? "");
  const message = raw.replace(/\s+/g, " ").trim();
  if (!message) return fallback;
  return message.length > 180 ? `${message.slice(0, 177)}…` : message;
}

export function unavailableSection(error, fallback = "This did not load.") {
  return {
    status: "unavailable",
    message: sectionMessage(error, fallback),
  };
}

/**
 * Actor ids that are people: a principal of kind human, an actor tagged
 * human, or an explicit `human:` ref. An unknown id is not assumed to be
 * a person.
 *
 * @param {object[]} [actors]
 * @param {object[]} [principals]
 */
export function humanActorIdSet(actors = [], principals = []) {
  const ids = new Set();
  for (const actor of Array.isArray(actors) ? actors : []) {
    const tags = Array.isArray(actor?.tags)
      ? actor.tags.map((tag) => String(tag).toLowerCase())
      : [];
    const id = String(actor?.id ?? actor?.actor_id ?? "").trim();
    if (id && tags.includes("human")) ids.add(id);
  }
  for (const principal of Array.isArray(principals) ? principals : []) {
    if (String(principal?.principal_kind ?? "").toLowerCase() !== "human")
      continue;
    const id = String(principal?.actor_id ?? "").trim();
    if (id) ids.add(id);
  }
  return ids;
}

function directoryPageIncomplete(value) {
  if (!value || typeof value !== "object") return false;
  return Boolean(
    String(value.next_cursor ?? "").trim() || value.has_more === true,
  );
}

/**
 * People used to decide "next actor is a person". A failed or paged-out
 * read is still usable, but the count must say it may be short.
 *
 * @param {PromiseSettledResult<{ actors?: object[] }>|undefined} actorsResult
 * @param {PromiseSettledResult<{ principals?: object[] }>|undefined} principalsResult
 */
export function settleHumanDirectory(actorsResult, principalsResult) {
  const actorsRejected = actorsResult?.status === "rejected";
  const principalsRejected = principalsResult?.status === "rejected";
  if (actorsRejected && principalsRejected) {
    throw actorsResult.reason instanceof Error
      ? actorsResult.reason
      : new Error("People could not be loaded.");
  }
  const incomplete =
    actorsRejected ||
    principalsRejected ||
    directoryPageIncomplete(actorsResult?.value) ||
    directoryPageIncomplete(principalsResult?.value);
  return {
    ids: humanActorIdSet(
      actorsRejected ? [] : actorsResult?.value?.actors || [],
      principalsRejected ? [] : principalsResult?.value?.principals || [],
    ),
    incomplete,
  };
}

export function isHumanNextActor(work, humanIds) {
  const raw = String(work?.next_actor ?? "").trim();
  if (!raw || !humanIds) return false;
  if (raw.toLowerCase() === "human" || /^human:/i.test(raw)) return true;
  const bare = raw.replace(/^actor:/i, "");
  return humanIds.has(raw) || humanIds.has(bare);
}

/** A capped read is "12+", matching the Tasks list. */
export function formatPartialCount(count, truncated) {
  return `${Number(count) || 0}${truncated ? "+" : ""}`;
}

export function tasksQuery(filters = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    const text = String(value ?? "").trim();
    if (text) params.set(key, text);
  }
  const query = params.toString();
  return query ? `/tasks?${query}` : "/tasks";
}

function sourceKey(work) {
  return String(work?.source?.authority ?? "").trim();
}

/** Blank phase is not the `unknown` phase. Core does not match `phase=unknown`. */
function phaseKey(work) {
  const phase = String(work?.phase ?? "").trim();
  return phase || "none";
}

function matrixCellHref(source, phase) {
  if (!source) return "";
  if (phase === "none") return tasksQuery({ source });
  return tasksQuery({ source, phase });
}

/**
 * Counts of deduped tasks by phase and source authority.
 * Rows follow the known source order, then any other authority.
 *
 * @param {object[]} records
 */
export function workMatrix(records = []) {
  const list = Array.isArray(records) ? records : [];
  const phases = [...PHASES];
  const seen = new Set(phases);
  /** @type {Map<string, Map<string, number>>} */
  const counts = new Map();
  for (const work of list) {
    const source = sourceKey(work);
    const phase = phaseKey(work);
    if (!seen.has(phase)) {
      phases.push(phase);
      seen.add(phase);
    }
    if (!counts.has(source)) counts.set(source, new Map());
    const row = counts.get(source);
    row.set(phase, (row.get(phase) || 0) + 1);
  }
  const sources = [...counts.keys()].sort((a, b) => {
    const ai = SOURCE_ORDER.indexOf(a);
    const bi = SOURCE_ORDER.indexOf(b);
    const ar = ai === -1 ? SOURCE_ORDER.length : ai;
    const br = bi === -1 ? SOURCE_ORDER.length : bi;
    if (ar !== br) return ar - br;
    return a.localeCompare(b);
  });
  return {
    phases: phases.map((key) => ({
      key,
      label: key === "none" ? "No phase" : label(key),
    })),
    rows: sources.map((key) => {
      const row = counts.get(key) || new Map();
      const cells = phases.map((phase) => ({
        phase,
        count: row.get(phase) || 0,
        href: matrixCellHref(key, phase),
      }));
      return {
        key,
        label: key ? sourceLabel({ authority: key }) : "No source",
        total: cells.reduce((sum, cell) => sum + cell.count, 0),
        cells,
      };
    }),
  };
}

/** stale / unknown / error, using the same key the Tasks freshness filter uses. */
export function freshnessBuckets(records = [], now = Date.now()) {
  const counts = { stale: 0, unknown: 0, error: 0 };
  for (const work of Array.isArray(records) ? records : []) {
    const key = workFreshness(work, now).key;
    if (key in counts) counts[key] += 1;
  }
  return FRESHNESS_BUCKETS.map((bucket) => ({
    ...bucket,
    count: counts[bucket.key],
    href: tasksQuery({ freshness: bucket.key }),
  }));
}

/**
 * Newest first. A title starting with "Dashboard" or "Fleet Dashboard"
 * is preferred over a newer report that does not.
 *
 * @param {Array<{ id?: string, title?: string, updated_at?: string, report?: object }>} entries
 */
export function selectVisualReports(entries = []) {
  const valid = (Array.isArray(entries) ? entries : []).filter(
    (entry) => entry?.report,
  );
  return [...valid].sort((a, b) => {
    const prefer =
      Number(isPreferredDashboardTitle(b?.title)) -
      Number(isPreferredDashboardTitle(a?.title));
    if (prefer) return prefer;
    const aMs = Date.parse(a?.updated_at ?? "");
    const bMs = Date.parse(b?.updated_at ?? "");
    const aTime = Number.isFinite(aMs) ? aMs : Number.NEGATIVE_INFINITY;
    const bTime = Number.isFinite(bMs) ? bMs : Number.NEGATIVE_INFINITY;
    if (aTime !== bTime) return bTime - aTime;
    return String(a?.id ?? "").localeCompare(String(b?.id ?? ""));
  });
}

/**
 * Follow `next_cursor` until the cap. A cursor left over means the count
 * is incomplete and must be shown as such.
 *
 * @param {(query: { limit: number, cursor?: string }) => Promise<{ work?: object[], next_cursor?: string }>} listPage
 */
export async function listWorkPages(
  listPage,
  { limit = WORK_PAGE_LIMIT, cap = WORK_ROW_CAP } = {},
) {
  const work = [];
  let cursor = "";
  while (work.length < cap) {
    const result = await listPage({
      limit: Math.min(limit, cap - work.length),
      cursor: cursor || undefined,
    });
    const rows = result?.work;
    if (!Array.isArray(rows)) {
      throw new Error("Work list was not returned.");
    }
    work.push(...rows);
    cursor = String(result?.next_cursor ?? "").trim();
    if (!cursor) break;
  }
  const truncated = Boolean(cursor) || work.length > cap;
  return { work: work.slice(0, cap), truncated };
}

function previewWork(work) {
  return {
    key: workKey(work),
    title: String(work?.title || "Untitled task"),
    href: taskDetailPath(work),
  };
}

function needsYouPreview(row, now) {
  const waitMs = rowWaitMs(row, now);
  const badge = inboxRowBadge(row, now);
  const params = new URLSearchParams({
    mailbox: "needs-you",
    item: row.id,
  });
  return {
    id: row.id,
    title: String(row.title || "Untitled"),
    source: String(row.source || ""),
    requester: String(row.requesterLabel || ""),
    wait: Number.isFinite(waitMs) ? formatWait(waitMs) : "",
    badge: badge ? { label: badge.label, tone: badge.tone } : null,
    href: `/inbox?${params.toString()}`,
  };
}

/**
 * Same Needs you rows the Inbox and the sidebar count use.
 * A rejected source is a failure, not an empty inbox.
 */
export function needsYouFromSources(
  {
    decisions = [],
    actions = [],
    work = [],
    inboxItems = [],
    truncated = false,
  },
  now = Date.now(),
) {
  const rows = filterMailbox(
    buildInboxRows({
      decisions,
      actions,
      work,
      inboxItems,
      now,
    }),
    "needs-you",
  );
  return {
    status: "ok",
    count: rows.length,
    truncated: Boolean(truncated),
    rows: rows.slice(0, PREVIEW_LIMIT).map((row) => needsYouPreview(row, now)),
    href: "/inbox?mailbox=needs-you",
  };
}

async function loadNeedsYou(client, now) {
  const results = await loadInboxSources({ withHistory: false, client });
  const failed = results
    .slice(0, 4)
    .find((result) => result.status === "rejected");
  if (failed) throw failed.reason;
  const value = (index, key) => results[index].value?.[key] || [];
  return needsYouFromSources(
    {
      decisions: value(0, "items"),
      actions: value(1, "items"),
      work: value(2, "work"),
      inboxItems: value(3, "items"),
      truncated: results.some(
        (result) =>
          result.status === "fulfilled" &&
          (result.value?.has_more === true ||
            Boolean(result.value?.next_cursor)),
      ),
    },
    now,
  );
}

async function loadHumanIds(client) {
  const [actorsResult, principalsResult] = await Promise.allSettled([
    client.listActors?.({ limit: 200 }) ?? Promise.resolve({ actors: [] }),
    client.listPrincipals?.({ limit: 200 }) ??
      Promise.resolve({ principals: [] }),
  ]);
  return settleHumanDirectory(actorsResult, principalsResult);
}

async function loadWork(client, now) {
  const [{ work, truncated }, humanIds] = await Promise.all([
    listWorkPages((query) => client.listWork(query)),
    loadHumanIds(client).then(
      (directory) => ({ status: "ok", ...directory }),
      (error) => ({
        status: "unavailable",
        message: sectionMessage(error, "People could not be loaded."),
      }),
    ),
  ]);
  const records = dedupeWorkBySource(work).records;
  const matrix = workMatrix(records);
  const blocked = records.filter((item) => item?.phase === "blocked");
  const human =
    humanIds.status === "ok"
      ? {
          status: "ok",
          incomplete: Boolean(humanIds.incomplete),
          count: records.filter((item) => isHumanNextActor(item, humanIds.ids))
            .length,
          href: tasksQuery({ human: "1" }),
          items: records
            .filter((item) => isHumanNextActor(item, humanIds.ids))
            .slice(0, PREVIEW_LIMIT)
            .map(previewWork),
        }
      : {
          status: "unavailable",
          message: humanIds.message,
          href: tasksQuery({ human: "1" }),
          count: null,
          items: [],
        };
  return {
    status: "ok",
    total: records.length,
    truncated,
    matrix,
    blocked: {
      count: blocked.length,
      href: tasksQuery({ phase: "blocked" }),
      items: blocked.slice(0, PREVIEW_LIMIT).map(previewWork),
    },
    human,
    freshness: freshnessBuckets(records, now),
  };
}

async function loadAgents(client) {
  const result = await client.listAgents();
  const agents = result?.agents;
  if (!Array.isArray(agents)) {
    throw new Error("Agent roster was not returned.");
  }
  const summary = rosterSummary(agents);
  return {
    status: "ok",
    working: summary.working,
    waiting: summary.waiting_on_human,
    stale: summary.stale,
    href: "/agents",
  };
}

async function mapPool(items, limit, fn) {
  const out = new Array(items.length);
  let next = 0;
  const workers = Array.from(
    { length: Math.min(limit, items.length) },
    async () => {
      while (next < items.length) {
        const index = next;
        next += 1;
        out[index] = await fn(items[index], index);
      }
    },
  );
  await Promise.all(workers);
  return out;
}

/**
 * Preferred dashboard titles first, then newest. The inline view reads in
 * this order and stops at the first valid report.
 *
 * @param {object[]} documents
 */
export function orderDocumentsForReportScan(documents = []) {
  return [...(Array.isArray(documents) ? documents : [])].sort((a, b) => {
    const prefer =
      Number(isPreferredDashboardTitle(b?.title)) -
      Number(isPreferredDashboardTitle(a?.title));
    if (prefer) return prefer;
    const aMs = Date.parse(a?.updated_at ?? "");
    const bMs = Date.parse(b?.updated_at ?? "");
    const aTime = Number.isFinite(aMs) ? aMs : Number.NEGATIVE_INFINITY;
    const bTime = Number.isFinite(bMs) ? bMs : Number.NEGATIVE_INFINITY;
    if (aTime !== bTime) return bTime - aTime;
    return String(a?.id ?? "").localeCompare(String(b?.id ?? ""));
  });
}

async function readReportDocument(client, doc) {
  const id = String(doc?.id ?? "").trim();
  if (!id) return { error: new Error("Document has no id.") };
  try {
    const got = await client.getDocument(id);
    const content = got?.revision?.content;
    const parsed = parseReportDocumentContent(content);
    const document = got?.document ?? doc;
    return {
      id,
      title: String(document?.title ?? doc?.title ?? ""),
      updated_at: document?.updated_at || doc?.updated_at || "",
      segment:
        resourceRouteSegment(document, "document") ||
        resourceRouteSegment(doc, "document") ||
        id,
      report: parsed.report,
      revision_ref: String(got?.revision?.ref ?? ""),
    };
  } catch (error) {
    return { error };
  }
}

export function parseReportDocumentContent(content) {
  return parseVisualReport(visualReportContentText(content));
}

/**
 * Read until one visual report is found. Documents not yet read stay in
 * `pending` for the report selector.
 *
 * @param {object[]} documents
 * @param {(doc: object) => Promise<object>} read
 */
export async function collectVisualReports(documents, read) {
  const ordered = orderDocumentsForReportScan(documents);
  let scanned = 0;
  let failures = 0;
  /** @type {object|null} */
  let found = null;
  const pending = [];
  for (const doc of ordered) {
    if (found) {
      pending.push(doc);
      continue;
    }
    scanned += 1;
    const entry = await read(doc);
    if (entry?.error) {
      failures += 1;
      continue;
    }
    if (entry?.report) found = entry;
  }
  return {
    reports: found ? [found] : [],
    pending,
    scanned,
    failures,
  };
}

function reportReadFailure(failures) {
  return failures === 1
    ? "A document could not be read."
    : `${failures} documents could not be read.`;
}

async function loadReports(client) {
  const listed = await client.listDocuments({
    state: ["active"],
    limit: DOC_SCAN_CAP,
  });
  if (!Array.isArray(listed?.documents)) {
    throw new Error("Document list was not returned.");
  }
  const documents = filterTopLevelDocuments(listed.documents).slice(
    0,
    DOC_SCAN_CAP,
  );
  const scan = await collectVisualReports(documents, (doc) =>
    readReportDocument(client, doc),
  );
  if (scan.failures && scan.reports.length === 0) {
    throw new Error(reportReadFailure(scan.failures));
  }
  return {
    status: "ok",
    reports: scan.reports,
    pending: scan.pending,
    truncated: Boolean(listed?.next_cursor),
    scanned: scan.scanned,
    warning: scan.failures
      ? "Some documents could not be read, so this list may be incomplete."
      : "",
  };
}

/** Read the documents skipped on the first paint, once the selector opens. */
export async function loadPendingReports(client, pending = [], existing = []) {
  const reads = await mapPool(Array.isArray(pending) ? pending : [], 6, (doc) =>
    readReportDocument(client, doc),
  );
  let failures = 0;
  const found = [];
  for (const entry of reads) {
    if (entry?.error) failures += 1;
    else if (entry?.report) found.push(entry);
  }
  return {
    reports: selectVisualReports([
      ...(Array.isArray(existing) ? existing : []),
      ...found,
    ]),
    failures,
  };
}

async function guard(run, fallback) {
  try {
    return await run();
  } catch (error) {
    return unavailableSection(error, fallback);
  }
}

/** Four independent reads. One failure does not zero out the others. */
export async function loadOverview(client, { now = Date.now() } = {}) {
  const [needsYou, work, agents, reports] = await Promise.all([
    guard(() => loadNeedsYou(client, now), "Needs you could not be loaded."),
    guard(() => loadWork(client, now), "Tasks could not be loaded."),
    guard(() => loadAgents(client), "Agents could not be loaded."),
    guard(() => loadReports(client), "Reports could not be loaded."),
  ]);
  return { needsYou, work, agents, reports };
}
