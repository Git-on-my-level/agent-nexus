/**
 * What an ask's answer did, and whether anyone received it.
 *
 * Core owns every fact here. `GET /asks/{ask_id}` (contract `AskOutcome`)
 * carries the ask's status, whether it has gone stale, the task decision the
 * answer recorded, and one row per delivery subscription. The ask event's own
 * payload carries the authoring evidence — typed refs for native resources and
 * labelled external links. This module turns both into the one vocabulary the
 * Inbox renders, so the pane and the standalone item page cannot disagree
 * about the same answer.
 *
 * Nothing here decides a state. `state`, `phase` and `reason` are core's
 * words; a value this client has never seen still renders, with a neutral tone
 * and core's own text, rather than blanking the panel.
 */

import taxonomy from "./generated/taxonomy.json";
import { formatAge } from "./ageBadge.js";
import { inboxSubjectNoun, splitTypedRef } from "./inboxUtils.js";

const asText = (value) => String(value ?? "").trim();

/**
 * Every response outcome the contract accepts, from the generated taxonomy
 * rather than a second copy of the enum. `needs_context` and `resolved` landed
 * with ask delivery; a client allowlist written by hand had already dropped
 * them once.
 */
export const INBOX_RESPONSE_OUTCOMES = Object.freeze(
  (taxonomy?.enums?.human_attention_response_outcome?.values ?? []).map(asText),
);

export const NEEDS_CONTEXT_OUTCOME = "needs_context";

export function isInboxResponseOutcome(outcome) {
  return INBOX_RESPONSE_OUTCOMES.includes(asText(outcome));
}

/**
 * The ask this inbox item is, as the `event:<id>` ref `asks.get` takes.
 *
 * Completed rows carry the same request ref, so a Handled row reads its own
 * delivery state without a second lookup for the open row it used to be.
 */
export function askRefForInboxItem(item) {
  const explicit =
    asText(item?.request_event_ref) || asText(item?.source_event_ref);
  if (explicit.startsWith("event:")) return explicit;
  const sourceId = asText(item?.source_event_id);
  return sourceId ? `event:${sourceId}` : "";
}

/**
 * Whether this item accepts "I need more context".
 *
 * Core publishes `allowed_response_outcomes` on the items where it restricts
 * the reader's options — a structured access-grant request takes approve or
 * reject and rejects everything else without mutating anything. Offering the
 * response there would leave the request pending behind a reader who believed
 * they had answered it.
 */
export function supportsNeedsContext(item) {
  const allowed = item?.allowed_response_outcomes;
  if (!Array.isArray(allowed) || allowed.length === 0) return true;
  return allowed.map(asText).includes(NEEDS_CONTEXT_OUTCOME);
}

/** An open ask core has marked stale, so the Inbox can fold it with the rest. */
export function askIsStale(item) {
  return item?.is_stale === true;
}

/*
 * Lookup tables with no prototype: these are keyed by strings core sends, and
 * an ordinary object would answer `constructor` with a function, which then
 * renders as `function Object() { [native code] }` instead of core's own word.
 */
const SUBSCRIPTION_KIND_LABELS = Object.freeze(
  Object.assign(Object.create(null), {
    await: "Live await",
    bridge: "Host bridge",
    webhook: "Webhook",
  }),
);

const DELIVERY_STATE_PRESENTATION = Object.freeze(
  Object.assign(Object.create(null), {
    delivered: { label: "Delivered", tone: "ok" },
    pending: { label: "Pending", tone: "neutral" },
    failed: { label: "Failed", tone: "danger" },
    none: { label: "Not delivered", tone: "neutral" },
  }),
);

/*
 * Core records why a delivery stopped as a fixed token, which is the right
 * thing to store and the wrong thing to show an operator: `dead_letter:
 * retry_limit` and `endpoint_blocked` are engineer words. These are the tokens
 * `askWebhooks` writes; a token this list has never seen falls through to
 * core's own text rather than being hidden.
 */
const DELIVERY_REASONS = Object.freeze(
  Object.assign(Object.create(null), {
    retry_limit: "Gave up after the attempt limit",
    recipient_inactive: "The subscribing agent is no longer active",
    response_not_accessible:
      "The answer is no longer readable by the subscriber",
    payload_too_large: "The answer is too large to send",
    endpoint_blocked: "The endpoint address is not allowed",
    secret_unavailable: "The signing secret could not be read",
    invalid_endpoint: "The endpoint URL is not usable",
    transport_failed: "The endpoint could not be reached",
  }),
);

/** Operator words for a subscription kind; an unknown kind keeps core's word. */
export function subscriptionKindLabel(kind) {
  const key = asText(kind);
  return SUBSCRIPTION_KIND_LABELS[key] ?? (key || "Subscription");
}

/**
 * Core's delivery reason as a sentence.
 *
 * A dead-lettered delivery arrives as `dead_letter: <token>`; the prefix says
 * it will not be retried, which the state already says, so the token is what
 * carries the information.
 */
export function deliveryReasonText(reason) {
  const raw = asText(reason);
  if (!raw) return "";
  const token = raw.startsWith("dead_letter:")
    ? raw.slice("dead_letter:".length).trim()
    : raw;
  return DELIVERY_REASONS[token] ?? raw;
}

/**
 * One delivery row, as the Handled panel draws it.
 *
 * The label is the non-secret one the subscriber chose; a webhook's URL and
 * its signing secret never reach a read. A failed delivery carries core's
 * reason, which is the only thing that says what to fix — there is no retry,
 * because the contract gives the operator no way to replay one: only the
 * subscriber itself may record a receipt.
 */
export function deliveryRowModel(delivery, { now = Date.now() } = {}) {
  const state = asText(delivery?.state) || "none";
  const presentation = DELIVERY_STATE_PRESENTATION[state] ?? {
    label: state,
    tone: "neutral",
  };
  const lastAt = asText(delivery?.last_at);
  const attempts = Number(delivery?.attempts);
  return {
    id: asText(delivery?.id),
    kindLabel: subscriptionKindLabel(delivery?.kind),
    label: asText(delivery?.label),
    state,
    stateLabel: presentation.label,
    tone: presentation.tone,
    failed: state === "failed",
    attempts: Number.isFinite(attempts) && attempts > 0 ? attempts : 0,
    ageLabel: lastAt ? formatAge(lastAt, now) : "",
    reason: deliveryReasonText(delivery?.reason),
  };
}

const CLOSED_PHASES = new Set(["done", "cancelled"]);

/**
 * What the answer did to the task, in one line.
 *
 * Core records the decision on the card in the same transaction as the answer,
 * so this is a read of that record rather than an inference: `phase` is where
 * the card ended up, `next_actor` is who owns it now, and `reason`
 * `source_owned` means an external source owns the phase and core left it
 * alone.
 *
 * A context request clears the blocker too, so the phase moves — but calling
 * that "Unblocked" next to a header that says the ask was not answered reads
 * as though it had been. `status` is what tells the two apart.
 */
export function taskOutcomeModel(
  taskOutcome,
  { nextActorLabel = "", status = "" } = {},
) {
  if (!taskOutcome || typeof taskOutcome !== "object") return null;
  const ref = asText(taskOutcome.card_ref);
  const phase = asText(taskOutcome.phase);
  const nextActor = asText(taskOutcome.next_actor);
  const reason = asText(taskOutcome.reason);
  if (!ref && !phase && !nextActor) return null;
  const who = asText(nextActorLabel) || nextActor;
  const closed = CLOSED_PHASES.has(phase);
  const next = who ? ` · next: ${who}` : "";
  let label = "";
  if (closed) {
    label = "Closed";
  } else if (asText(status) === NEEDS_CONTEXT_OUTCOME) {
    label = `Returned for context${next}`;
  } else if (reason === "source_owned") {
    label = `Phase unchanged (source owns it)${next}`;
  } else {
    label = `Unblocked${next}`;
  }
  return { ref, phase, nextActor, nextActorLabel: who, reason, closed, label };
}

/**
 * The whole delivery picture for one ask.
 *
 * `notDelivered` is the state the reader most needs named rather than coloured:
 * nobody subscribed, so nothing was sent, and the task moved anyway. That is
 * how an unattended agent is meant to work, not a failure, and it says so in
 * words instead of wearing a warning.
 *
 * Where there is no task outcome at all the sentence says nothing about a task.
 * Core returns none for an access-grant decision, for a legacy non-card ask,
 * and for every answer recorded before #336 — and "the task was unblocked"
 * about a task that does not exist is a confident wrong answer.
 */
export function askDeliveryModel(
  outcome,
  { now = Date.now(), nextActorLabel = "" } = {},
) {
  if (!outcome || typeof outcome !== "object") return null;
  const status = asText(outcome.status) || "open";
  const task = taskOutcomeModel(outcome.task_outcome, {
    nextActorLabel,
    status,
  });
  const subscriptions = (
    Array.isArray(outcome.delivery) ? outcome.delivery : []
  )
    .filter((entry) => entry && typeof entry === "object")
    .map((entry) => deliveryRowModel(entry, { now }));
  const answered = status !== "open";
  const owner = task?.nextActorLabel || "";
  let notDelivered = null;
  if (answered && subscriptions.length === 0) {
    if (!task) {
      notDelivered = "Not delivered: no subscriber was registered.";
    } else if (task.closed) {
      notDelivered = "Not delivered: no subscriber; the task was closed.";
    } else if (owner) {
      notDelivered = `Not delivered: no subscriber; task unblocked for ${owner}.`;
    } else {
      notDelivered = "Not delivered: no subscriber; the task was unblocked.";
    }
  }
  return {
    status,
    isStale: outcome.is_stale === true,
    needsContext: status === NEEDS_CONTEXT_OUTCOME,
    task,
    subscriptions,
    notDelivered,
  };
}

/** Refs that are plumbing rather than evidence a reader would open. */
const NON_EVIDENCE_PREFIXES = new Set(["thread", "inbox", "event", "artifact"]);

const EXTERNAL_LINK_KINDS = [
  [/\/pull\/\d+/, "Pull request"],
  [/\/merge_requests\/\d+/, "Merge request"],
  [/\/commit\/[0-9a-f]{7,40}/i, "Commit"],
  [/\/blob\/[0-9a-f]{40}\//i, "Code"],
  [/\/issues\/\d+/, "Issue"],
];

function externalLinkKind(url) {
  for (const [pattern, label] of EXTERNAL_LINK_KINDS) {
    if (pattern.test(url)) return label;
  }
  return "";
}

/** An http(s) URL with no embedded credentials, or "" when it is neither. */
export function safeEvidenceUrl(value) {
  const raw = asText(value);
  if (!raw) return "";
  let url;
  try {
    url = new URL(raw);
  } catch {
    return "";
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") return "";
  if (url.username || url.password) return "";
  return url.toString();
}

/**
 * The evidence under an ask's question.
 *
 * Two kinds, and they are not interchangeable. Native evidence is typed refs
 * (`document:`, `card:`, `topic:`) which resolve inside the workspace and open
 * without leaving the Inbox. External evidence is `payload.evidence`: a label
 * and an http(s) URL, written by the asking agent. Refs come from the ask
 * event when it is readable and from the inbox row otherwise — the row's
 * `related_refs` already merges the event's refs, so the panel still has the
 * native evidence when the event read is refused.
 *
 * The subject is excluded: the context strip above already names the task this
 * ask blocks, and repeating it as evidence read as a second, different thing.
 * `subjectRef` is the ref that strip actually resolved to, which is not always
 * the item's own `subject_ref` — an ask filed on a thread that names one card
 * is shown as being about that card.
 */
export function askEvidenceModel({
  item = null,
  event = null,
  subjectRef = "",
} = {}) {
  const payload =
    event?.payload && typeof event.payload === "object" ? event.payload : {};
  const subjects = new Set(
    [
      asText(item?.subject_ref),
      asText(payload.subject_ref),
      asText(subjectRef),
    ].filter(Boolean),
  );
  const rawRefs = [
    ...(Array.isArray(payload.related_refs) ? payload.related_refs : []),
    ...(Array.isArray(item?.related_refs) ? item.related_refs : []),
  ];
  const seenRefs = new Set();
  const refs = [];
  for (const value of rawRefs) {
    const ref = asText(value);
    if (!ref || subjects.has(ref) || seenRefs.has(ref)) continue;
    const { prefix, id } = splitTypedRef(ref);
    if (!prefix || !id || NON_EVIDENCE_PREFIXES.has(prefix)) continue;
    seenRefs.add(ref);
    refs.push({ ref, prefix, id, noun: inboxSubjectNoun(prefix) });
  }
  const seenLinks = new Set();
  const links = [];
  for (const entry of Array.isArray(payload.evidence) ? payload.evidence : []) {
    const url = safeEvidenceUrl(entry?.url);
    if (!url || seenLinks.has(url)) continue;
    seenLinks.add(url);
    let host = "";
    try {
      host = new URL(url).host;
    } catch {
      host = "";
    }
    links.push({
      label: asText(entry?.label) || host || url,
      url,
      host,
      kind: externalLinkKind(url),
    });
  }
  return {
    refs,
    links,
    supersedes: asText(payload.supersedes),
    overrideReason: asText(payload.authoring_override_reason),
    empty: refs.length === 0 && links.length === 0,
  };
}

/*
 * Spans a rewrite must not touch: fenced blocks, an existing markdown link or
 * image, and an autolink. A name inside one of those is already a reference,
 * or is not prose at all.
 */
const PROTECTED_SPANS =
  /(```[\s\S]*?```|~~~[\s\S]*?~~~|!?\[[^\]]*\]\([^)]*\)|<[a-z][a-z0-9+.-]*:[^>\s]*>)/gi;

/**
 * The three things a rewrite looks at, in one pass so each position is decided
 * once: a ref word followed by a quoted or backticked name, an inline code
 * span (left verbatim), and a named PR. The named form wins at the ref word,
 * which is what lets `` doc `rulings` `` be linked while a bare `` `rulings` ``
 * stays code.
 */
const BODY_TOKENS =
  /\b(doc|document|card|topic)([ \t]+)(["`])([^"`\n]+)\3|`[^`\n]*`|\bPR[ \t]*#?\d+\b/gi;

/*
 * The same words and quoting the CLI's authoring lint recognises
 * (`cli/internal/app/ask_authoring.go`), so "a named resource" means one thing
 * on both sides. Prototype-free for the same reason as the tables above.
 */
const REF_PREFIX_WORDS = Object.freeze(
  Object.assign(Object.create(null), {
    doc: "document",
    document: "document",
    card: "card",
    topic: "topic",
  }),
);

function replaceOutsideProtectedSpans(source, replace) {
  let out = "";
  let index = 0;
  PROTECTED_SPANS.lastIndex = 0;
  let match;
  while ((match = PROTECTED_SPANS.exec(source)) !== null) {
    out += replace(source.slice(index, match.index));
    out += match[0];
    index = match.index + match[0].length;
  }
  return out + replace(source.slice(index));
}

/*
 * A markdown link destination may not carry an unescaped parenthesis, angle
 * bracket or space: `[x](https://h/a)b)` parses as a link to `https://h/a`
 * followed by the literal `b)`, which hands whoever wrote the URL the rest of
 * the line as markdown. Percent-encode those so the destination is exactly the
 * URL and nothing after it is reinterpreted.
 */
const LINK_DESTINATION_UNSAFE = /[()<>\s"'`\\]/g;

function linkDestination(href) {
  // Not `encodeURIComponent`: it leaves `(` and `)` as they are, which are
  // exactly the two characters that matter here.
  return String(href ?? "").replace(
    LINK_DESTINATION_UNSAFE,
    (character) =>
      `%${character.charCodeAt(0).toString(16).toUpperCase().padStart(2, "0")}`,
  );
}

/*
 * Link text is the author's own prose, so it can already be markdown — but a
 * bracket in it would end the link early and leave the destination on screen.
 */
function linkText(label) {
  return String(label ?? "").replace(/[[\]]/g, "\\$&");
}

/**
 * Names in the body that an evidence entry already identifies become links.
 *
 * This is the fix for an ask that named a document and left the reader to go
 * and find it. The authoring standard the CLI lints for is what makes it
 * mechanical: a named resource is written `doc "rulings"` or `` card `anx-12` ``
 * and must have a matching ref, and a named `PR #123` must have a matching
 * pull-request URL. So the rewrite only ever links a name the ask itself
 * already backed with evidence — it never guesses, and prose that names
 * nothing is returned exactly as written.
 *
 * `hrefFor(ref)` is the caller's: workspace routes belong to the page, not to
 * a model. A ref it cannot route is left as plain text.
 *
 * @param {string} body markdown
 * @param {{ refs?: Array, links?: Array }} evidence from `askEvidenceModel`
 * @param {{ hrefFor?: (ref: string) => string }} [options]
 */
export function linkifyAskEvidence(
  body,
  evidence,
  { hrefFor = () => "" } = {},
) {
  const source = String(body ?? "");
  if (!source) return "";
  const refs = Array.isArray(evidence?.refs) ? evidence.refs : [];
  const links = Array.isArray(evidence?.links) ? evidence.links : [];
  if (!refs.length && !links.length) return source;

  const hrefByName = new Map();
  for (const entry of refs) {
    const href = asText(hrefFor(entry.ref));
    if (!href) continue;
    hrefByName.set(`${entry.prefix}:${entry.id.toLowerCase()}`, href);
  }
  const pullUrlByNumber = new Map();
  for (const link of links) {
    const match = link.url.match(/\/(?:pull|merge_requests)\/(\d+)/);
    if (match && !pullUrlByNumber.has(match[1])) {
      pullUrlByNumber.set(match[1], link.url);
    }
  }
  if (!hrefByName.size && !pullUrlByNumber.size) return source;

  return replaceOutsideProtectedSpans(source, (text) =>
    text.replace(BODY_TOKENS, (whole, word, gap, quote, name) => {
      if (word) {
        const prefix = REF_PREFIX_WORDS[word.toLowerCase()];
        const href = hrefByName.get(`${prefix}:${name.trim().toLowerCase()}`);
        if (!href) return whole;
        const label = quote === "`" ? `\`${name}\`` : linkText(name);
        return `${word}${gap}[${label}](${linkDestination(href)})`;
      }
      if (whole.startsWith("`")) return whole;
      const number = whole.match(/(\d+)/)?.[1] ?? "";
      const url = pullUrlByNumber.get(number);
      return url ? `[${whole}](${linkDestination(url)})` : whole;
    }),
  );
}
