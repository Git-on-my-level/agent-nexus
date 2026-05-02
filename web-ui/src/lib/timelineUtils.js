import {
  attachmentArtifactDisplayLabel,
  buildPrimitiveRefRoutes,
  coerceTimelineResourceList,
  resolveRefLink,
} from "./refLinkModel.js";

const EVENT_TYPE_LABELS = {
  message_posted: "Message posted",
  card_created: "Card created",
  card_updated: "Card updated",
  card_moved: "Card moved",
  card_resolved: "Card resolved",
  exception_raised: "Exception raised",
  human_attention_requested: "Human attention requested",
  human_attention_responded: "Human response recorded",
};

export const KNOWN_EVENT_TYPES = new Set(Object.keys(EVENT_TYPE_LABELS));

const EVENT_TYPE_DOT_CLASSES = {
  message_posted: "bg-indigo-400",
  card_created: "bg-purple-400",
  card_updated: "bg-purple-400",
  card_moved: "bg-purple-400",
  card_resolved: "bg-purple-400",
  exception_raised: "bg-red-400",
  human_attention_requested: "bg-cyan-400",
  human_attention_responded: "bg-teal-400",
};

export function eventTypeDotClass(type) {
  return EVENT_TYPE_DOT_CLASSES[type] ?? "bg-gray-500";
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value
    : {};
}

function artifactLabel(artifact, id) {
  const record = asObject(artifact);
  const kind = String(record.kind ?? "")
    .trim()
    .toLowerCase();
  if (kind === "attachment") {
    const attachmentLabel = attachmentArtifactDisplayLabel(record);
    if (attachmentLabel) return attachmentLabel;
  }

  const summary = String(record.summary ?? record.title ?? "").trim();
  if (summary) {
    return summary;
  }
  const kindFallback = String(record.kind ?? "Artifact").trim();
  return `${kindFallback} ${id}`.trim();
}

function documentLabel(document, id) {
  const record = asObject(document);
  const title = String(record.title ?? "").trim();
  if (title) {
    return title;
  }
  return `Document ${id}`.trim();
}

function documentRevisionLabel(revision, id, documents = {}) {
  const record = asObject(revision);
  const documentId = String(record.document_id ?? "").trim();
  const document = documentId ? asObject(documents[documentId]) : {};
  const title = String(document.title ?? "").trim();
  const revisionNumber = record.revision_number;

  if (title && Number.isFinite(Number(revisionNumber))) {
    return `${title} revision ${revisionNumber}`.trim();
  }

  if (title) {
    return `${title} revision`.trim();
  }

  return `Document revision ${id}`.trim();
}

export function buildTimelineRefLabelHints(
  artifacts = {},
  documents = {},
  documentRevisions = {},
) {
  const hints = {};

  const artifactHintIds = new Set();
  for (const artifact of coerceTimelineResourceList(artifacts)) {
    const id = String(artifact?.id ?? "").trim();
    if (!id) continue;
    hints[`artifact:${id}`] = artifactLabel(artifact, id);
    artifactHintIds.add(id);
  }
  for (const [artifactId, artifact] of Object.entries(asObject(artifacts))) {
    const id = String(artifactId ?? "").trim();
    if (!id || artifactHintIds.has(id)) continue;
    hints[`artifact:${id}`] = artifactLabel(artifact, id);
  }

  const documentRows = coerceTimelineResourceList(documents);
  const documentsKeyed = {};
  const documentHintIds = new Set();

  for (const document of documentRows) {
    const id = String(document?.id ?? "").trim();
    if (!id) continue;
    documentsKeyed[id] = document;
    hints[`document:${id}`] = documentLabel(document, id);
    documentHintIds.add(id);
  }

  for (const [documentId, document] of Object.entries(asObject(documents))) {
    const id = String(documentId ?? "").trim();
    if (!id || documentHintIds.has(id)) continue;
    documentsKeyed[id] = document;
    hints[`document:${id}`] = documentLabel(document, id);
  }

  for (const [revisionId, revision] of Object.entries(
    asObject(documentRevisions),
  )) {
    const id = String(revisionId ?? "").trim();
    if (!id) continue;
    hints[`document_revision:${id}`] = documentRevisionLabel(
      revision,
      id,
      documentsKeyed,
    );
  }

  return hints;
}

/** @param {unknown} raw */
export function normalizeDocumentRevisionsInput(raw) {
  if (raw == null || raw === "") return {};
  if (Array.isArray(raw)) {
    const o = {};
    for (const r of raw) {
      if (!r || typeof r !== "object") continue;
      const id = String(/** @type {any} */ (r).revision_id ?? "").trim();
      if (id) o[id] = r;
    }
    return o;
  }
  if (typeof raw === "object" && !Array.isArray(raw)) return raw;
  return {};
}

function payloadObject(event) {
  const p = event?.payload;
  return p && typeof p === "object" && !Array.isArray(p) ? p : {};
}

/** @param {Record<string, unknown>} payload */
export function changedFieldsFromPayload(payload) {
  const cf = payload?.changed_fields;
  if (!Array.isArray(cf)) return [];
  return [...new Set(cf.map((x) => String(x).trim()).filter(Boolean))];
}

const PREVIEW_LINE_MAX = 140;
const PREVIEW_LINES_CAP = 5;

function clipLine(s, max = PREVIEW_LINE_MAX) {
  const t = String(s ?? "")
    .trim()
    .replace(/\s+/g, " ");
  if (t.length <= max) return t;
  return `${t.slice(0, max - 1)}…`;
}

function fieldTitle(field) {
  return String(field ?? "")
    .replace(/_/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

function coalesceList(val) {
  if (Array.isArray(val)) {
    return val.map((x) => String(x).trim()).filter(Boolean);
  }
  return [];
}

function refDeltaSummary(prev, next, labelHints) {
  const ps = new Set(coalesceList(prev));
  const ns = new Set(coalesceList(next));
  const added = [...ns].filter((x) => !ps.has(x));
  const removed = [...ps].filter((x) => !ns.has(x));
  const fmt = (arr, prefix) => {
    if (!arr.length) return "";
    const shown = arr.slice(0, 2).map((r) => {
      const h = labelHints?.[r];
      return h || clipLine(r, 48);
    });
    const extra = arr.length > 2 ? ` (+${arr.length - 2} more)` : "";
    return `${prefix}${shown.join(", ")}${extra}`;
  };
  const bits = [fmt(removed, "Removed: "), fmt(added, "Added: ")].filter(
    Boolean,
  );
  return bits.join(" · ") || "No ref changes";
}

/**
 * @param {Record<string, unknown>} event
 * @param {{ labelHints?: Record<string, string> }} [ctx]
 * @returns {string[]}
 */
export function buildTimelineEventChangePreview(event, ctx = {}) {
  const labelHints = ctx.labelHints ?? {};
  const type = String(event?.type ?? "");
  const payload = payloadObject(event);
  const lines = [];

  if (
    type === "card_updated" ||
    type === "topic_updated" ||
    type === "board_updated"
  ) {
    const fields = changedFieldsFromPayload(payload);
    for (const field of fields) {
      const prevKey = `previous_${field}`;
      const hasPrev = Object.prototype.hasOwnProperty.call(payload, prevKey);
      const nextVal = payload[field];
      const prevVal = hasPrev ? payload[prevKey] : undefined;
      const label = fieldTitle(field);
      switch (field) {
        case "assignee_refs":
        case "related_refs":
        case "resolution_refs":
        case "definition_of_done":
          lines.push(
            `${label}: ${refDeltaSummary(prevVal, nextVal, labelHints)}`,
          );
          break;
        default: {
          const a = prevVal == null ? "" : String(prevVal).trim();
          const b = nextVal == null ? "" : String(nextVal).trim();
          if (a || b) {
            lines.push(
              `${label}: ${clipLine(a || "—", 48)} → ${clipLine(b || "—", 48)}`,
            );
          }
          break;
        }
      }
    }
  }

  if (type === "card_moved") {
    const from = String(payload.from_column_key ?? "").trim();
    const to = String(payload.column_key ?? "").trim();
    if (from || to) {
      lines.push(`Column: ${from || "—"} → ${to || "—"}`);
    }
    const bTh = String(payload.before_thread_id ?? "").trim();
    const aTh = String(payload.after_thread_id ?? "").trim();
    if (bTh !== aTh && (bTh || aTh)) {
      lines.push(
        `Thread context: ${clipLine(bTh || "—", 24)} → ${clipLine(aTh || "—", 24)}`,
      );
    }
  }

  if (type === "card_created") {
    if (payload.title) lines.push(`Title: ${clipLine(payload.title, 80)}`);
    if (payload.column_key) {
      lines.push(`Column: ${String(payload.column_key)}`);
    }
    if (payload.summary)
      lines.push(`Summary: ${clipLine(payload.summary, 100)}`);
  }

  return lines
    .slice(0, PREVIEW_LINES_CAP)
    .map((l) => clipLine(l, PREVIEW_LINE_MAX));
}

export function timelineEventUsesMarkdownSummary(type) {
  return (
    type === "message_posted" ||
    type === "human_attention_requested" ||
    type === "human_attention_responded"
  );
}

export function toTimelineViewEvent(event, options = {}) {
  const type = String(event?.type ?? "");
  const isKnownType = KNOWN_EVENT_TYPES.has(type);
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  const threadId = options.threadId ?? event?.thread_id ?? "";
  const documentRevisionsNorm = normalizeDocumentRevisionsInput(
    options.documentRevisions,
  );
  const labelHints =
    options.labelHints ??
    buildTimelineRefLabelHints(
      options.artifacts,
      options.documents,
      documentRevisionsNorm,
    );
  const routeMaps =
    options.routeMaps ??
    buildPrimitiveRefRoutes({
      artifacts: options.artifacts,
      events: options.events,
      cards: options.cards,
      documents: options.documents,
      threadId,
    });

  const payload = payloadObject(event);
  const changedFields = changedFieldsFromPayload(payload);
  const changePreviewLines = buildTimelineEventChangePreview(event, {
    labelHints,
  });
  const useMarkdownSummary = timelineEventUsesMarkdownSummary(type);

  return {
    ...event,
    refs,
    isKnownType,
    typeLabel: EVENT_TYPE_LABELS[type] ?? "Unknown event type",
    rawType: type,
    changedFields,
    changePreviewLines,
    useMarkdownSummary,
    resolvedRefs: refs.map((refValue) =>
      resolveRefLink(refValue, {
        threadId,
        humanize: true,
        labelHints,
        ...routeMaps,
      }),
    ),
  };
}

function parseEventTimeMs(event) {
  const ts = event?.ts;
  if (ts == null || ts === "") {
    return Number.NEGATIVE_INFINITY;
  }
  const ms = Date.parse(String(ts));
  return Number.isFinite(ms) ? ms : Number.NEGATIVE_INFINITY;
}

function compareEventsNewestFirst(a, b) {
  const tb = parseEventTimeMs(b);
  const ta = parseEventTimeMs(a);
  if (tb !== ta) {
    return tb - ta;
  }
  return String(b.id ?? "").localeCompare(String(a.id ?? ""));
}

export function toTimelineView(events = [], options = {}) {
  const ordered = Array.isArray(events)
    ? [...events].sort(compareEventsNewestFirst)
    : [];
  const routeMaps =
    options.routeMaps ??
    buildPrimitiveRefRoutes({
      artifacts: options.artifacts,
      events: ordered,
      cards: options.cards,
      documents: options.documents,
      threadId: options.threadId,
    });
  const documentRevisionsNorm = normalizeDocumentRevisionsInput(
    options.documentRevisions,
  );
  const labelHintsPre = buildTimelineRefLabelHints(
    options.artifacts,
    options.documents,
    documentRevisionsNorm,
  );

  return ordered.map((event) =>
    toTimelineViewEvent(event, {
      ...options,
      routeMaps,
      events: ordered,
      documentRevisions: documentRevisionsNorm,
      labelHints: options.labelHints ?? labelHintsPre,
    }),
  );
}
