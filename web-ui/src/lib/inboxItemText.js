/**
 * An inbox item's heading and its body, separated.
 *
 * Asks are written by agents, and a long-standing habit is to put the whole
 * question — context, options and a recommendation — into the title and leave
 * the body empty. Rendered as a title that is the whole ask reads as several
 * hundred characters of bold with no structure in it.
 *
 * So the title is only ever a heading here: the first sentence, or about
 * {@link INBOX_HEADING_MAX_CHARS} characters. Whatever is left moves into the
 * body, above the body the item already had, where it renders as markdown like
 * any other prose.
 *
 * This is presentation only. Core still owns the stored title and body; a
 * shorter title arriving from the writing side simply makes the split a no-op.
 */

/** How long a heading may get before the rest moves into the body. */
export const INBOX_HEADING_MAX_CHARS = 120;

/**
 * Shortest heading a sentence boundary may produce. Below this the first
 * sentence is an acknowledgement ("Quick one.") rather than the ask, and a
 * word-boundary cut at the full budget says more.
 */
const MIN_SENTENCE_HEADING_CHARS = 32;

/**
 * The budget is "about 120 characters", not exactly: a title barely over it
 * stays whole. Splitting one that ends `…rollout window closes` moved the
 * single word "closes" into the body, where it read as an orphan paragraph.
 * The heading still clamps to two lines on screen, so nothing runs long.
 */
function orphanRestLimit(max) {
  return Math.min(30, Math.round(max * 0.25));
}

/** Collapse runs of whitespace so a heading is one line of prose. */
function singleLine(value) {
  return String(value ?? "")
    .replace(/\s+/g, " ")
    .trim();
}

function normalizeSource(value) {
  return String(value ?? "")
    .replace(/\r\n?/g, "\n")
    .trim();
}

/** Trailing punctuation left behind by a mid-sentence cut. */
function trimCutEdge(value) {
  return String(value ?? "")
    .replace(/[\s,;:([{<"'“‘\-–—]+$/u, "")
    .trim();
}

/**
 * The last sentence end at or before `max`, or -1 when there is none. The
 * boundary must be followed by whitespace so a version number or a decimal
 * does not end the heading.
 */
function lastSentenceEnd(text, max) {
  const pattern = /[.!?]+(?=\s|$)/g;
  let found = -1;
  let match;
  while ((match = pattern.exec(text)) !== null) {
    const end = match.index + match[0].length;
    if (end > max) break;
    found = end;
  }
  return found;
}

/**
 * Split a title into the heading to show and the text that belongs in the
 * body.
 *
 * The author's own boundaries win before any measuring does: a title with a
 * line break in it already says where the heading ends. A leading markdown
 * heading marker on that first line is dropped, since the line is about to be
 * rendered as a heading.
 *
 * @param {string} title
 * @param {{ maxChars?: number }} [options]
 * @returns {{ heading: string, rest: string, truncated: boolean }}
 *   `truncated` means the heading stops mid-sentence and wears an ellipsis.
 */
export function clampInboxHeading(title, { maxChars } = {}) {
  const max =
    Number.isFinite(maxChars) && maxChars > 0
      ? Math.floor(maxChars)
      : INBOX_HEADING_MAX_CHARS;
  const source = normalizeSource(title);
  if (!source) return { heading: "", rest: "", truncated: false };

  const breakIndex = source.indexOf("\n");
  const firstLine = (
    breakIndex >= 0 ? source.slice(0, breakIndex) : source
  ).trim();
  const tail = breakIndex >= 0 ? source.slice(breakIndex + 1).trim() : "";
  const head = firstLine.replace(/^#{1,6}\s+/, "").trim();

  const join = (first, second) =>
    [first, second].filter(Boolean).join("\n\n").trim();

  if (head.length <= max + orphanRestLimit(max)) {
    return { heading: head, rest: tail, truncated: false };
  }

  const sentenceEnd = lastSentenceEnd(head, max);
  if (sentenceEnd >= MIN_SENTENCE_HEADING_CHARS) {
    return {
      heading: head.slice(0, sentenceEnd).trim(),
      rest: join(head.slice(sentenceEnd).trim(), tail),
      truncated: false,
    };
  }

  // No usable sentence end: cut on a word boundary inside the budget, and
  // only mid-word when the title has no spaces to cut on (a bare id or URL).
  const window = head.slice(0, max);
  const lastSpace = window.lastIndexOf(" ");
  const cut = lastSpace >= Math.floor(max * 0.6) ? lastSpace : max;
  return {
    heading: `${trimCutEdge(head.slice(0, cut))}…`,
    rest: join(head.slice(cut).trim(), tail),
    truncated: true,
  };
}

/**
 * The heading and body to render for an inbox item.
 *
 * @param {{ title?: string, body?: string }} item
 * @param {{ maxChars?: number }} [options]
 * @returns {{
 *   heading: string,
 *   body: string,
 *   fullTitle: string,
 *   movedFromTitle: boolean,
 *   truncated: boolean,
 * }}
 */
export function inboxItemText(item, options = {}) {
  const { heading, rest, truncated } = clampInboxHeading(item?.title, options);
  const body = normalizeSource(item?.body);
  return {
    heading,
    body: [rest, body].filter(Boolean).join("\n\n").trim(),
    fullTitle: singleLine(item?.title),
    movedFromTitle: Boolean(rest),
    truncated,
  };
}

/** Compare two pieces of agent prose for "this is the same text". */
function comparableText(value) {
  // Trailing ellipsis and sentence punctuation are noise for this comparison:
  // the writing side clips a long card title to "…" and keeps the period.
  return singleLine(value)
    .toLowerCase()
    .replace(/[.…\s]+$/u, "");
}

/**
 * True when the subject card's title is the ask text over again.
 *
 * An ask filed with `anx ask` auto-creates the card it blocks, and the card
 * gets the ask text as its title. The context box then printed the whole ask a
 * second time, directly under the heading. A truncated card title counts too:
 * the writing side clips long titles, so the card often holds a prefix of the
 * ask rather than all of it.
 *
 * @param {string} subjectTitle
 * @param {string} askText
 * @param {{ minPrefix?: number }} [options] how much of a shared prefix is
 *   enough to call it a repeat; short titles that merely start the same way
 *   ("Fix login") are coincidence, not duplication.
 */
export function subjectTitleRepeatsAsk(
  subjectTitle,
  askText,
  { minPrefix = 40 } = {},
) {
  const subject = comparableText(subjectTitle);
  const ask = comparableText(askText);
  if (!subject || !ask) return false;
  if (subject === ask) return true;
  const [shorter, longer] =
    subject.length <= ask.length ? [subject, ask] : [ask, subject];
  return shorter.length >= minPrefix && longer.startsWith(shorter);
}
