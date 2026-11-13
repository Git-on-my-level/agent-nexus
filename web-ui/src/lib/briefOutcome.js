/**
 * The outcome side of `brief.v1`: the number an initiative is judged by
 * (`measure`), what a decision puts at risk (`stakes`), what agents told the
 * reader without wanting an answer (`fyi`) and whether anyone is doing the
 * housekeeping (`upkeep`).
 *
 * Normalizers ignore unknown fields. Formatters are pure and take `now`, so
 * goldens are deterministic. Nothing here invents state: every string is a
 * reading core computed, formatted.
 */

import { formatAgo } from "$lib/time/format.js";

const text = (value) => String(value ?? "").trim();
const number = (value) =>
  typeof value === "number" && Number.isFinite(value) ? value : null;

/** `initiatives[].measure`, or null when the initiative declares none. */
export function normalizeMeasure(raw) {
  if (!raw || typeof raw !== "object") return null;
  const label = text(raw.label);
  if (!label) return null;
  return {
    label,
    direction: raw.direction === "down" ? "down" : "up",
    target: number(raw.target),
    unit: text(raw.unit),
    baseline: number(raw.baseline),
    latest: number(raw.latest),
    latestAt: text(raw.latest_at),
    previous: number(raw.previous),
    previousAt: text(raw.previous_at),
    onTarget: typeof raw.on_target === "boolean" ? raw.on_target : null,
    staleFor: Math.max(0, Number(raw.age_seconds) || 0),
    points: (Array.isArray(raw.points) ? raw.points : [])
      .map((p) => ({ at: text(p?.at), value: number(p?.value) }))
      .filter((p) => p.value !== null),
  };
}

/** `decisions[].stakes`, or null when the asker claimed none. */
export function normalizeStakes(raw) {
  if (!raw || typeof raw !== "object") return null;
  const stakes = {
    irreversible: raw.irreversible === true,
    cost: text(raw.cost),
  };
  return stakes.irreversible || stakes.cost ? stakes : null;
}

/** `fyi`, newest first as core sends it. */
export function normalizeFyi(raw) {
  return (Array.isArray(raw) ? raw : [])
    .map((row) => {
      const ref = text(row?.ref);
      if (!ref) return null;
      const author = row.author;
      return {
        ref,
        title: text(row.title) || ref,
        text: text(row.text),
        author:
          author && typeof author === "object"
            ? text(author.display) || text(author.id)
            : text(author),
        at: text(row.at),
        subject: text(row.subject?.ref)
          ? { ref: text(row.subject.ref), title: text(row.subject.title) }
          : null,
        repeats: Math.max(0, Math.trunc(Number(row.repeats) || 0)),
      };
    })
    .filter(Boolean);
}

/** `upkeep`, or null from a core that omits it (or a scoped reader). */
export function normalizeUpkeep(raw) {
  if (!raw || typeof raw !== "object") return null;
  return {
    open: Math.max(0, Math.trunc(Number(raw.open_suggestions) || 0)),
    truncated: Boolean(raw.truncated),
    oldestWaiting: Math.max(0, Number(raw.oldest_waiting_seconds) || 0),
    untended: Boolean(raw.untended),
  };
}

/** At most two decimals, no trailing zeros: 41.50 → "41.5", 7.00 → "7". */
export function formatNumber(value) {
  if (value === null || value === undefined) return "";
  return String(Number(Number(value).toFixed(2)));
}

/**
 * A value with its unit. "%" and one-character units attach ("12.4%",
 * "41.5h" only when the unit is a single character); longer units take a
 * space ("3 hosts").
 */
export function formatMeasureValue(value, unit = "") {
  const n = formatNumber(value);
  if (!n) return "";
  if (!unit) return n;
  return unit.length === 1 ? `${n}${unit}` : `${n} ${unit}`;
}

/** Whole units of the largest fitting span: 90s → "1m", 4 days → "4d". */
export function formatShortSpan(seconds) {
  const s = Math.max(0, Number(seconds) || 0);
  if (s >= 86_400) return `${Math.floor(s / 86_400)}d`;
  if (s >= 3_600) return `${Math.floor(s / 3_600)}h`;
  if (s >= 60) return `${Math.floor(s / 60)}m`;
  return `${Math.floor(s)}s`;
}

/**
 * What a measure reads as on a Watch row.
 *
 * `reading` is "41.5 h → 48 h", or null when nothing has published the
 * number, in which case `target` is "target 7". `change` is the movement
 * since the earlier reading, toned by whether it went the wanted way.
 * `stale` is the age of the latest reading ("read 4d ago"); nothing judges it old.
 *
 * @returns {{ label: string, reading: string|null, target: string,
 *   change: { text: string, tone: "ok"|"danger"|"muted" }|null,
 *   stale: string, onTarget: boolean|null }}
 */
export function measureView(measure, now = Date.now()) {
  const target = formatMeasureValue(measure.target, measure.unit);
  const view = {
    label: measure.label,
    reading: null,
    target: target ? `target ${target}` : "",
    change: null,
    stale: "",
    onTarget: measure.onTarget,
  };
  if (measure.latest === null) return view;
  const latest = formatMeasureValue(measure.latest, measure.unit);
  view.reading = target ? `${latest} → ${target}` : latest;
  if (measure.previous !== null) {
    const delta = measure.latest - measure.previous;
    const span =
      (Date.parse(measure.latestAt) - Date.parse(measure.previousAt)) / 1000;
    const over = Number.isFinite(span) && span > 0 ? formatShortSpan(span) : "";
    const size = formatNumber(Math.abs(delta));
    if (size === "0") {
      view.change = {
        text: over ? `flat in ${over}` : "flat",
        tone: "muted",
      };
    } else {
      const went = delta > 0 ? "up" : "down";
      view.change = {
        text: `${went} ${size}${over ? ` in ${over}` : ""}`,
        tone: went === measure.direction ? "ok" : "danger",
      };
    }
  }
  if (measure.staleFor > 0)
    view.stale = `read ${formatAgo(now - measure.staleFor * 1000, now)}`;
  return view;
}

/** The measure line as one string, for a title attribute or a golden. */
export function measureText(measure, now = Date.now()) {
  const view = measureView(measure, now);
  return [
    view.reading
      ? `${view.label} ${view.reading}`
      : `${view.label}: No reading`,
    view.reading ? "" : view.target,
    view.change?.text,
    view.stale,
  ]
    .filter(Boolean)
    .join(" · ");
}

/**
 * On a row with a measure the number leads, so a late promise becomes a
 * small note that reporting is late rather than the headline.
 */
export function demotePromiseParts(parts) {
  return parts.map((part) =>
    part.field === "initiatives.past_promise_for"
      ? {
          text: `reporting late ${part.text.replace(/ past$/, "")}`,
          field: part.field,
          tone: "muted",
        }
      : part,
  );
}

/**
 * The untended-workspace notice: open suggestions that core reports as
 * untended (the oldest has waited longer than the product's one fallback
 * duration). The threshold is core's; this file holds none. Null otherwise.
 *
 * @returns {{ count: string, days: number }|null}
 */
export function upkeepNotice(upkeep) {
  if (!upkeep || upkeep.open <= 0) return null;
  if (!upkeep.untended) return null;
  return {
    count: `${upkeep.open}${upkeep.truncated ? "+" : ""}`,
    days: Math.floor(upkeep.oldestWaiting / 86_400),
  };
}

/** "arrived 5 times" for a headline that collapsed four older copies. */
export function fyiRepeatText(repeats) {
  return repeats > 0 ? `arrived ${repeats + 1} times` : "";
}

/** Sparkline polyline points for values in a `width`×`height` box. */
export function sparklinePoints(values, width = 56, height = 16) {
  if (!Array.isArray(values) || values.length < 2) return "";
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = max - min || 1;
  const step = width / (values.length - 1);
  return values
    .map((v, i) => {
      const x = Number((i * step).toFixed(1));
      const y = Number(
        (height - 1 - ((v - min) / range) * (height - 2)).toFixed(1),
      );
      return `${x},${y}`;
    })
    .join(" ");
}
