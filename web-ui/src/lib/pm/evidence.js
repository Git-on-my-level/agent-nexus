import { safeSourceHref, sourceLabel } from "./presentation.js";

/**
 * Task evidence, grouped by the source it was read from.
 *
 * A reader reports the source item and every comment, review and check on it
 * each time it runs, so four reads of one GitHub issue used to print twenty
 * links that all read "github.com/…/issues/208". An operator wants one line
 * per source ("GitHub #208 · 4 observations · last 1m ago"), the distinct
 * links once, and the read-by-read history only on request.
 *
 * Presentation only: observations stay append-only in core.
 */

/** "Git-on-my-level/agent-nexus#208" → "#208"; ids without a number stay whole. */
export function shortNativeId(nativeId) {
  const raw = String(nativeId ?? "").trim();
  if (!raw) return "";
  const hash = raw.lastIndexOf("#");
  if (hash >= 0 && hash < raw.length - 1) return raw.slice(hash);
  // A bare issue number reads as one.
  if (/^\d+$/.test(raw)) return `#${raw}`;
  return raw;
}

/** The source line's name: "GitHub #208", "Multica MUL-12", "Git". */
export function sourceName(work) {
  const source = work?.source ?? {};
  return [sourceLabel(source), shortNativeId(source.native_id)]
    .filter(Boolean)
    .join(" ");
}

function numberFromPath(url) {
  try {
    const match = new URL(url).pathname.match(/\/(?:issues|pull)\/(\d+)/);
    return match ? `#${match[1]}` : "";
  } catch {
    return "";
  }
}

function hostPath(url) {
  try {
    const parsed = new URL(url);
    return `${parsed.host}${parsed.pathname === "/" ? "" : parsed.pathname}`;
  } catch {
    return "";
  }
}

const KIND_LABELS = {
  issue: "Issue",
  pull_request: "Pull request",
  comment: "Comment",
  review: "Review",
  check_run: "Check",
  task_run: "Run",
  git_revision: "Revision",
};

/**
 * A link name an operator can tell apart from its neighbours. Comments on one
 * issue share a path and differ only by fragment, so they are numbered.
 */
function linkLabel(entry, url, ordinal) {
  const kind = String(entry?.kind ?? "").trim();
  const summary = String(entry?.summary ?? "").trim();
  const kindLabel = KIND_LABELS[kind] || "";
  if (kind === "issue" || kind === "pull_request") {
    const number = numberFromPath(url);
    return [kindLabel, number].filter(Boolean).join(" ") || hostPath(url);
  }
  if (kind === "comment") return `${kindLabel} ${ordinal}`;
  if (kindLabel && summary) return `${kindLabel} · ${summary}`;
  if (kindLabel) return `${kindLabel} ${ordinal}`;
  return summary || hostPath(url) || String(entry?.ref ?? "") || "Source link";
}

function statusOf(observation) {
  if (observation?.status === "error") return "error";
  if (observation?.verification === "verified") return "verified";
  if (observation?.status === "uncertain") return "uncertain";
  return "reported";
}

/** Badge wording for one read. `verified` is only trusted from `verification`. */
export function observationStatusLabel(observation) {
  return {
    error: "Read failed",
    verified: "Verified evidence",
    uncertain: "Uncertain report",
    reported: "Reported claim",
  }[statusOf(observation)];
}

export function observationStatusTone(observation) {
  return {
    error: "danger",
    verified: "ok",
    uncertain: "warn",
    reported: "neutral",
  }[statusOf(observation)];
}

export function observationErrorText(observation) {
  const error = observation?.error;
  if (!error) return "";
  return typeof error === "string"
    ? error
    : String(error.message || error.code || "");
}

/**
 * Distinct evidence links across reads, newest read first, each URL once.
 * Entries without a safe http(s) URL are kept as plain text, also once.
 */
export function distinctEvidenceLinks(observations) {
  const seen = new Set();
  const links = [];
  const counters = {};
  for (const observation of Array.isArray(observations) ? observations : []) {
    if (statusOf(observation) === "error") continue;
    for (const entry of Array.isArray(observation?.evidence)
      ? observation.evidence
      : []) {
      const href = safeSourceHref(entry?.url);
      const key =
        href || String(entry?.ref ?? entry?.summary ?? "").trim() || "";
      if (!key || seen.has(key)) continue;
      seen.add(key);
      const kind = String(entry?.kind ?? "").trim() || "_";
      counters[kind] = (counters[kind] || 0) + 1;
      links.push({
        key,
        href,
        kind,
        label: linkLabel(entry, href, counters[kind]),
        summary: String(entry?.summary ?? "").trim(),
      });
    }
  }
  return links;
}

/**
 * Read history with consecutive identical reads folded into one row: a
 * reader learns more from "failed 30 times since 2h ago" or "the same
 * report 18 times since 9:40" than from thirty identical lines. Reads are
 * identical when they failed with the same message, or reported the same
 * source revision with the same links and the same standing.
 */
function historyKey(observation) {
  const status = statusOf(observation);
  if (status === "error") return `error|${observationErrorText(observation)}`;
  const links = (
    Array.isArray(observation?.evidence) ? observation.evidence : []
  )
    .map((entry) => String(entry?.url ?? entry?.ref ?? entry?.summary ?? ""))
    .join(" ");
  return `${status}|${observation?.source_revision ?? ""}|${links}`;
}

export function observationHistory(observations) {
  const rows = [];
  for (const observation of Array.isArray(observations) ? observations : []) {
    const key = historyKey(observation);
    const last = rows[rows.length - 1];
    if (last && last.key === key) {
      last.count += 1;
      last.oldest = observation.observed_at || last.oldest;
      continue;
    }
    const failed = statusOf(observation) === "error";
    rows.push({
      key,
      group: failed,
      count: 1,
      message: failed ? observationErrorText(observation) : "",
      newest: observation.observed_at,
      oldest: observation.observed_at,
      observation,
    });
  }
  return rows;
}

/**
 * One summary per source for a task's observations.
 *
 * Every observation of a task describes that task's source item, so a task
 * has at most one source group; the key is still derived so a task whose
 * reports name another authority (a linked pull request's own reader, say)
 * degrades to separate lines instead of merging them.
 *
 * @returns {Array<{ key: string, name: string, count: number, reads: number,
 *   latest: object | null, latestRead: object | null, lastAt: string,
 *   links: Array<{ key: string, href: string, kind: string, label: string, summary: string }>,
 *   observations: object[] }>}
 */
export function evidenceSources(observations, work) {
  const list = Array.isArray(observations) ? observations : [];
  if (!list.length) return [];
  const fallbackName = sourceName(work) || "Source";
  const groups = new Map();
  for (const observation of list) {
    const authority = String(
      observation?.source?.authority ?? work?.source?.authority ?? "",
    )
      .trim()
      .toLowerCase();
    const key = authority || "_";
    if (!groups.has(key)) {
      groups.set(key, {
        key,
        name:
          authority && authority !== work?.source?.authority
            ? sourceLabel({ authority })
            : fallbackName,
        observations: [],
      });
    }
    groups.get(key).observations.push(observation);
  }
  return [...groups.values()].map((group) => {
    const reads = group.observations.filter(
      (observation) => statusOf(observation) !== "error",
    );
    const latest = group.observations[0] ?? null;
    const latestRead = reads[0] ?? null;
    return {
      ...group,
      count: group.observations.length,
      reads: reads.length,
      latest,
      latestRead,
      lastAt: latestRead?.observed_at || latest?.observed_at || "",
      links: distinctEvidenceLinks(group.observations),
    };
  });
}
