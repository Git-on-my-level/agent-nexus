import { humanActorIdSet } from "$lib/humanActors.js";
export { humanActorIdSet, isHumanNextActor } from "$lib/humanActors.js";
import { rosterSummary } from "$lib/agentPresence.js";
import {
  buildInboxRows,
  filterMailbox,
  formatWait,
  inboxRowBadge,
  rowWaitMs,
} from "$lib/inboxMailbox.js";
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

/** The core snapshot is shared with `anx overview --json`. */
export async function loadOverview(client, { now = Date.now() } = {}) {
  const snapshot = await client.getOverview();
  const records = dedupeWorkBySource(snapshot.work.items).records;
  const blocked = records.filter((item) => item.phase === "blocked");
  return {
    needsYou: {
      ...snapshot.needs_you,
      rows: snapshot.needs_you.rows.slice(0, PREVIEW_LIMIT),
    },
    initiatives: snapshot.initiatives,
    sinceYouLastLooked: snapshot.since_you_last_looked,
    /*
     * The morning brief arrives computed. An older core sends no `brief` at
     * all, and that stays undefined rather than becoming an empty object, so
     * the band can tell "no brief" apart from "a brief with nothing in it".
     */
    brief: snapshot.brief,
    reports: {
      ...snapshot.dashboard,
      reports: (snapshot.dashboard.reports || []).flatMap((entry) => {
        const parsed = parseVisualReport(JSON.stringify(entry.report));
        return parsed.report ? [{ ...entry, report: parsed.report }] : [];
      }),
    },
    agents:
      snapshot.agents.status === "ok"
        ? {
            status: "ok",
            ...(() => {
              const summary = rosterSummary(snapshot.agents.items);
              return {
                working: summary.working,
                waiting: summary.waiting_on_human,
                /*
                 * Two different facts, both counted: `stale` is a silence with
                 * work riding on it, `offline` is an agent that is simply not
                 * running. The tile shows the first when there is one and the
                 * second otherwise, so a quiet workspace does not read as a
                 * page of warnings. See `agentPresence.js`.
                 */
                stale: summary.stale,
                offline: summary.offline,
                href: "/agents",
                truncated: snapshot.agents.truncated === true,
              };
            })(),
          }
        : snapshot.agents,
    work: {
      status: snapshot.work.status,
      total: records.length,
      truncated: snapshot.work.truncated === true,
      matrix: workMatrix(records),
      blocked: {
        count: blocked.length,
        href: tasksQuery({ phase: "blocked" }),
        items: blocked.slice(0, PREVIEW_LIMIT).map(previewWork),
      },
      human: {
        status: "ok",
        count: snapshot.work.human_count,
        items: [],
        href: tasksQuery({ human: "1" }),
      },
      freshness: freshnessBuckets(records, now),
    },
  };
}

// Preserve loaded choices while advancing through the API's candidate windows.
export function mergeDashboardReports(current, next) {
  const reports = new Map();
  for (const entry of [...(current?.reports || []), ...(next?.reports || [])]) {
    const parsed = parseVisualReport(JSON.stringify(entry.report));
    if (parsed.report)
      reports.set(entry.id, { ...entry, report: parsed.report });
  }
  return {
    ...current,
    ...next,
    pinned_ref: current?.pinned_ref ?? next?.pinned_ref ?? null,
    reports: [...reports.values()],
  };
}
