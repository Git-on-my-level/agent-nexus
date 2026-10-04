/**
 * "Since you last looked" — what changed on the Overview since this viewer's
 * previous visit.
 *
 * Core owns the baseline. `GET /overview` returns `since_you_last_looked`
 * against the previous visit and then records this one, so the strip costs no
 * extra request and no client-side bookkeeping — there is no localStorage here,
 * and no per-browser guess at who the viewer is.
 *
 * A first visit has `since: null` and no items, which is a strip that does not
 * render rather than an empty one that says nothing.
 */

const asText = (value) => String(value ?? "").trim();

/**
 * How each change reads. Phrasing stays generic by design: the digest carries
 * refs and titles, never the conversation text behind an answered ask.
 */
const KINDS = Object.freeze({
  step_completed: { label: "step done", tone: "ok" },
  initiative_stalled: { label: "stalled", tone: "warn" },
  initiative_blocked: { label: "blocked", tone: "danger" },
  ask_answered: { label: "answered", tone: "ok" },
  decision_recorded: { label: "decided", tone: "ok" },
});

/** Order the strip reads in: what needs attention first, then what progressed. */
const ORDER = [
  "initiative_blocked",
  "initiative_stalled",
  "ask_answered",
  "step_completed",
];

/**
 * The strip, or `null` when there is nothing to show.
 *
 * @param {{since?: string|null, generated_at?: string, items?: object[], truncated?: boolean}|null} digest
 * @param {{limit?: number}} [options]
 */
export function sinceYouLastLookedStrip(digest, { limit = 6 } = {}) {
  if (!digest || typeof digest !== "object") return null;
  // No baseline yet: this is a first visit, so there is no "since" to report.
  if (!asText(digest.since)) return null;

  const items = (Array.isArray(digest.items) ? digest.items : [])
    .map((item) => {
      const kind = asText(item?.kind);
      const known = KINDS[kind];
      if (!known) return null;
      return {
        kind,
        label: known.label,
        tone: known.tone,
        ref: asText(item?.ref),
        title: asText(item?.title) || asText(item?.ref),
        stepId: asText(item?.step_id),
        at: asText(item?.ts),
      };
    })
    .filter(Boolean);

  if (!items.length) return null;

  const rank = (item) => {
    const index = ORDER.indexOf(item.kind);
    return index === -1 ? ORDER.length : index;
  };
  const ordered = [...items].sort((a, b) => rank(a) - rank(b));
  const shown = ordered.slice(0, limit);

  const counts = {};
  for (const item of items) counts[item.kind] = (counts[item.kind] ?? 0) + 1;

  return {
    since: asText(digest.since),
    items: shown,
    // Both the server's cap and this strip's own cap are "there is more".
    overflow: Math.max(0, items.length - shown.length),
    truncated: digest.truncated === true,
    counts,
    summary: summarize(counts),
  };
}

/** A one-line reading of the whole digest, for the strip's heading. */
function summarize(counts) {
  const parts = [];
  const plural = (count, one, many) => `${count} ${count === 1 ? one : many}`;
  if (counts.step_completed) {
    parts.push(plural(counts.step_completed, "step done", "steps done"));
  }
  if (counts.initiative_blocked) {
    parts.push(`${counts.initiative_blocked} blocked`);
  }
  if (counts.initiative_stalled) {
    parts.push(`${counts.initiative_stalled} stalled`);
  }
  if (counts.ask_answered) {
    parts.push(plural(counts.ask_answered, "ask answered", "asks answered"));
  }
  if (counts.decision_recorded) {
    parts.push(plural(counts.decision_recorded, "decision", "decisions"));
  }
  return parts.join(" · ");
}
