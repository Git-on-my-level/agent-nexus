import { shortMimeBadge } from "./attachmentDisplay.js";
import { resourceRouteSegment } from "./resourceIdentity.js";
import { parseRef, renderRef } from "./typedRefs.js";
import { workspacePath } from "./workspacePaths.js";

function asPathSegment(value) {
  return encodeURIComponent(String(value));
}

function asText(value) {
  return String(value ?? "").trim();
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value
    : {};
}

function lookupLabelHint(raw, prefix, value, labelHints) {
  if (!labelHints || typeof labelHints !== "object") {
    return "";
  }

  const direct =
    labelHints[raw] ?? labelHints[`${prefix}:${value}`] ?? labelHints[value];
  return String(direct ?? "").trim();
}

function summarizeUrl(value) {
  try {
    const url = new URL(String(value));
    const path = String(url.pathname ?? "").replace(/\/+$/, "") || "/";
    const shownPath = path.length > 28 ? `${path.slice(0, 28)}...` : path;
    return `${url.hostname}${shownPath}`;
  } catch {
    return "External link";
  }
}

function shouldHumanizeByDefault(prefix) {
  return (
    prefix === "document" ||
    prefix === "document_revision" ||
    prefix === "card_revision"
  );
}

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function compactValue(value) {
  if (UUID_RE.test(value)) return value.slice(0, 10);
  return value;
}

function fileNameShowsExtension(name) {
  return /\.[a-zA-Z0-9]{1,12}$/.test(String(name ?? "").trim());
}

/**
 * Operator-facing label for `kind: attachment` artifacts (filename + format when helpful).
 * @param {Record<string, unknown>} artifact
 */
export function attachmentArtifactDisplayLabel(artifact) {
  const fileName = asText(
    artifact?.original_filename ?? artifact?.originalFilename ?? "",
  );
  const summary = asText(artifact?.summary ?? "");
  const displayName = fileName || summary;
  const mime = asText(
    artifact?.content_type ??
      artifact?.contentType ??
      artifact?.mime_type ??
      artifact?.mimeType ??
      "",
  );
  const formatLabel = shortMimeBadge(mime);

  if (displayName) {
    if (fileNameShowsExtension(displayName) || !formatLabel) return displayName;
    return `${displayName} · ${formatLabel}`;
  }

  if (formatLabel) return `${formatLabel} file`;
  return "";
}

function humanizedLabelForPrefix(prefix, value) {
  const short = compactValue(value);
  if (prefix === "artifact") return `Artifact ${short}`.trim();
  if (prefix === "card") return `Card ${short}`.trim();
  if (prefix === "message") return "Message";
  if (prefix === "thread") return `Thread ${short}`.trim();
  if (prefix === "topic") return `Topic ${short}`.trim();
  if (prefix === "event") return "Event";
  if (prefix === "document") return `Document ${short}`.trim();
  if (prefix === "document_revision")
    return `Document revision ${short}`.trim();
  if (prefix === "card_revision") return `Card revision ${short}`.trim();
  if (prefix === "url") return summarizeUrl(value);
  if (prefix === "inbox") return "Inbox item";
  if (prefix === "board") return `Board ${short}`.trim();
  return "";
}

function resolveRefLabels(raw, prefix, value, options = {}) {
  const humanize = Boolean(options.humanize) || shouldHumanizeByDefault(prefix);
  const labelHint = lookupLabelHint(raw, prefix, value, options.labelHints);

  if (!humanize) {
    return {
      label: raw,
      primaryLabel: raw,
      secondaryLabel: "",
    };
  }

  const primaryLabel =
    labelHint || humanizedLabelForPrefix(prefix, value) || raw;
  const secondaryLabel = primaryLabel === raw ? "" : raw;
  return {
    label: primaryLabel,
    primaryLabel,
    secondaryLabel,
  };
}

function toWorkspaceHref(organizationSlug, workspaceSlug, pathname) {
  const org = String(organizationSlug ?? "").trim();
  const ws = String(workspaceSlug ?? "").trim();
  if (!org || !ws) {
    return "";
  }
  return workspacePath(org, ws, pathname);
}

function buildInternalHref(workspaceSlug, pathname, organizationSlug) {
  return toWorkspaceHref(organizationSlug, workspaceSlug, pathname);
}

function splitTypedRef(value) {
  const parsed = parseRef(value);
  return {
    prefix: asText(parsed.prefix),
    value: asText(parsed.value),
  };
}

function firstRouteMapValue(routes, prefix, value) {
  if (!routes || typeof routes !== "object") {
    return null;
  }
  const normalizedValue = asText(value);
  if (!normalizedValue) {
    return null;
  }
  return (
    routes[normalizedValue] ?? routes[`${prefix}:${normalizedValue}`] ?? null
  );
}

function routeLabelHint(route) {
  const label = asText(route?.label);
  if (label) return label;
  const title = asText(route?.title);
  if (title) return title;
  return "";
}

function resolvePrimitiveRoute(prefix, value, options = {}) {
  if (prefix === "artifact") {
    return normalizeArtifactRoute(
      firstRouteMapValue(options.artifactRoutesById, prefix, value),
      value,
    );
  }
  if (prefix === "event") {
    return normalizeEventRoute(
      firstRouteMapValue(options.eventRoutesById, prefix, value),
      value,
      options,
    );
  }
  return null;
}

function normalizeArtifactRoute(route, artifactId) {
  const candidate = asObject(route);
  const kind = asText(candidate.kind || candidate.targetKind);
  const targetPrefix = asText(
    candidate.targetPrefix || candidate.prefix || candidate.refPrefix,
  );
  const targetValue = asText(
    candidate.targetValue || candidate.value || candidate.id,
  );

  if (targetPrefix && targetValue) {
    return {
      ...candidate,
      sourcePrefix: "artifact",
      sourceValue: asText(artifactId),
      prefix: targetPrefix,
      value: targetValue,
      kind: kind || targetPrefix,
      label: routeLabelHint(candidate),
    };
  }

  const documentId = asText(candidate.document_id || candidate.documentId);
  if (documentId) {
    return {
      ...candidate,
      sourcePrefix: "artifact",
      sourceValue: asText(artifactId),
      prefix: "document",
      value: documentId,
      kind: "document",
      label: routeLabelHint(candidate),
    };
  }

  const cardId = asText(candidate.card_id || candidate.cardId);
  if (cardId) {
    return {
      ...candidate,
      sourcePrefix: "artifact",
      sourceValue: asText(artifactId),
      prefix: "card",
      value: cardId,
      kind: "card",
      label: routeLabelHint(candidate),
    };
  }

  return null;
}

function normalizeEventRoute(route, eventId, options = {}) {
  const candidate = asObject(route);
  const eventType = asText(candidate.type || candidate.eventType);
  const targetKind = asText(candidate.kind || candidate.targetKind);
  if (eventType !== "message_posted" && targetKind !== "message") {
    return null;
  }
  return {
    ...candidate,
    sourcePrefix: "event",
    sourceValue: asText(eventId),
    prefix: "message",
    value: asText(eventId),
    kind: "message",
    label: routeLabelHint(candidate),
    topicId: asText(candidate.topicId || candidate.topic_id),
    threadId: asText(
      candidate.threadId || candidate.thread_id || options.threadId,
    ),
  };
}

const LINK_RESOLVERS = {
  artifact: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/artifacts/${asPathSegment(value)}`,
      organizationSlug,
    ),
  thread: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/threads/${asPathSegment(value)}`,
      organizationSlug,
    ),
  topic: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/topics/${asPathSegment(value)}`,
      organizationSlug,
    ),
  card: ({ workspaceSlug, organizationSlug, boardId, value }) =>
    boardId
      ? buildInternalHref(
          workspaceSlug,
          `/boards/${asPathSegment(boardId)}?card=${asPathSegment(value)}`,
          organizationSlug,
        )
      : "",
  message: ({ workspaceSlug, organizationSlug, topicId, threadId, value }) =>
    buildInternalHref(
      workspaceSlug,
      topicId
        ? `/topics/${asPathSegment(topicId)}?tab=messages#message-${asPathSegment(value)}`
        : threadId
          ? `/threads/${asPathSegment(threadId)}?tab=messages#message-${asPathSegment(value)}`
          : `/events#${asPathSegment(value)}`,
      organizationSlug,
    ),
  event: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/events#${asPathSegment(value)}`,
      organizationSlug,
    ),
  url: ({ value }) => value,
  inbox: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/inbox#inbox-${asPathSegment(value)}`,
      organizationSlug,
    ),
  document: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/docs/${asPathSegment(value)}`,
      organizationSlug,
    ),
  document_revision: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/docs/revisions/${asPathSegment(value)}`,
      organizationSlug,
    ),
  board: ({ workspaceSlug, organizationSlug, value }) =>
    buildInternalHref(
      workspaceSlug,
      `/boards/${asPathSegment(value)}`,
      organizationSlug,
    ),
};

function createResolvedLink(raw, prefix, value, labels, { href, isExternal }) {
  return {
    raw,
    prefix,
    value,
    kind: prefix,
    ...labels,
    href,
    isExternal,
    isLink: Boolean(href),
  };
}

function attachmentMetaFromRoute(route) {
  if (!route || String(route.kind ?? "").toLowerCase() !== "attachment") {
    return null;
  }
  const sz =
    route.size_bytes ?? route.sizeBytes ?? route.byte_size ?? route.byteSize;
  const sizeNum = sz != null && sz !== "" ? Number(sz) : NaN;
  return {
    content_type: asText(route.content_type ?? route.contentType),
    original_filename: asText(
      route.original_filename ?? route.originalFilename,
    ),
    size_bytes: Number.isFinite(sizeNum) ? sizeNum : undefined,
    trashed_at: route.trashed_at ?? route.trashedAt ?? null,
  };
}

function labelsForRoutedPrimitive(raw, route, options = {}) {
  const labelHint =
    route.label ||
    lookupLabelHint(
      `${route.prefix}:${route.value}`,
      route.prefix,
      route.value,
      options.labelHints,
    );
  const primaryLabel =
    labelHint || humanizedLabelForPrefix(route.prefix, route.value) || raw;
  return {
    label: primaryLabel,
    primaryLabel,
    // Routed primitive refs deliberately hide the artifact/event id in normal
    // badge text; the primitive id is still available on source pages and in
    // tooltips via `raw`.
    secondaryLabel: "",
  };
}

export function resolveRefLink(refValue, options = {}) {
  const parsed = parseRef(refValue);
  const raw = renderRef(parsed);
  const prefix = parsed.prefix;
  const value = parsed.value;
  const workspaceSlug = options.workspaceSlug;
  const organizationSlug = String(options.organizationSlug ?? "").trim();
  const boardId = options.boardId;
  const threadId = options.threadId;

  if (!prefix) {
    return {
      raw,
      prefix,
      value,
      kind: "raw",
      ...resolveRefLabels(raw, prefix, value, options),
      href: "",
      isExternal: false,
      isLink: false,
    };
  }

  const route = resolvePrimitiveRoute(prefix, value, options);
  if (route) {
    const linkResolver = LINK_RESOLVERS[route.prefix];
    const labels = labelsForRoutedPrimitive(raw, route, options);
    if (linkResolver) {
      const attachmentMeta = attachmentMetaFromRoute(route);
      return {
        ...createResolvedLink(raw, prefix, value, labels, {
          href: linkResolver({
            workspaceSlug,
            organizationSlug,
            topicId: route.topicId || route.topic_id,
            threadId: route.threadId || threadId,
            boardId: route.boardId || route.board_id || boardId,
            value: route.value,
          }),
          isExternal: route.prefix === "url",
        }),
        routed: true,
        routedKind: route.kind,
        routedPrefix: route.prefix,
        routedValue: route.value,
        attachmentMeta,
      };
    }
  }

  const labels = resolveRefLabels(raw, prefix, value, options);
  const linkResolver = LINK_RESOLVERS[prefix];
  if (linkResolver) {
    return createResolvedLink(raw, prefix, value, labels, {
      href: linkResolver({
        workspaceSlug,
        organizationSlug,
        threadId,
        boardId,
        value,
      }),
      isExternal: prefix === "url",
      boardId,
    });
  }

  return {
    raw,
    prefix,
    value,
    kind: "unknown",
    label: raw,
    primaryLabel: raw,
    secondaryLabel: "",
    href: "",
    isExternal: false,
    isLink: false,
  };
}

/**
 * Thread timeline responses encode `artifacts` / `documents` as JSON objects
 * keyed by id; topic timeline and workspace payloads use arrays. Normalize so
 * ref routing always sees a list.
 * @param {unknown} value
 * @returns {unknown[]}
 */
export function coerceTimelineResourceList(value) {
  if (value == null) return [];
  if (Array.isArray(value)) return value;
  if (typeof value === "object") return Object.values(value);
  return [];
}

export function buildPrimitiveRefRoutes({
  artifacts = [],
  events = [],
  cards = [],
  documents = [],
  threadId = "",
} = {}) {
  const artifactRows = coerceTimelineResourceList(artifacts);
  const cardRows = coerceTimelineResourceList(cards);
  const documentRows = coerceTimelineResourceList(documents);
  const eventRows = coerceTimelineResourceList(events);

  const cardById = new Map(
    cardRows.flatMap((card) =>
      [asText(card?.id), resourceRouteSegment(card, "card")]
        .filter(Boolean)
        .map((id) => [id, card]),
    ),
  );
  const documentById = new Map(
    documentRows.flatMap((document) =>
      [asText(document?.id), resourceRouteSegment(document, "document")]
        .filter(Boolean)
        .map((id) => [id, document]),
    ),
  );

  const artifactRoutesById = {};
  const eventRoutesById = {};

  for (const document of documentById.values()) {
    const documentId = resourceRouteSegment(document, "document");
    const artifactId = asText(
      document?.artifact_id ||
        document?.artifactId ||
        document?.head_revision?.artifact_id ||
        document?.headRevision?.artifactId,
    );
    if (!documentId || !artifactId) continue;
    artifactRoutesById[artifactId] = {
      kind: "document",
      targetPrefix: "document",
      targetValue: documentId,
      label: asText(document?.title),
    };
  }

  for (const artifact of artifactRows) {
    const storageId = asText(artifact?.id);
    const id = resourceRouteSegment(artifact, "artifact") || storageId;
    if (!id) continue;
    const kind = asText(artifact?.kind).toLowerCase();
    const owner = splitTypedRef(artifact?.owner_ref);
    const directDocumentId =
      asText(artifact?.document_id || artifact?.documentId) ||
      (owner.prefix === "document" ? owner.value : "");
    const directCardId =
      asText(artifact?.card_id || artifact?.cardId) ||
      (owner.prefix === "card" ? owner.value : "");

    if (kind === "doc" && directDocumentId) {
      const document = documentById.get(directDocumentId);
      artifactRoutesById[id] = {
        kind: "document",
        targetPrefix: "document",
        targetValue:
          resourceRouteSegment(document, "document") || directDocumentId,
        label: asText(document?.title),
      };
      if (storageId && storageId !== id) {
        artifactRoutesById[storageId] = artifactRoutesById[id];
      }
      continue;
    }

    if (kind === "card" && directCardId) {
      const card = cardById.get(directCardId);
      const board = splitTypedRef(card?.board_ref || artifact?.board_ref);
      artifactRoutesById[id] = {
        kind: "card",
        targetPrefix: "card",
        targetValue: resourceRouteSegment(card, "card") || directCardId,
        boardId:
          asText(artifact?.board_id || artifact?.boardId) ||
          (board.prefix === "board" ? board.value : ""),
        label: asText(card?.title),
      };
      if (storageId && storageId !== id) {
        artifactRoutesById[storageId] = artifactRoutesById[id];
      }
      continue;
    }

    if (kind === "attachment") {
      const label = attachmentArtifactDisplayLabel(artifact) || "Attachment";
      const sz =
        artifact?.size_bytes ??
        artifact?.sizeBytes ??
        artifact?.byte_size ??
        artifact?.byteSize;
      const sizeNum = sz != null && sz !== "" ? Number(sz) : NaN;
      artifactRoutesById[id] = {
        kind: "attachment",
        targetPrefix: "artifact",
        targetValue: id,
        label,
        content_type: asText(
          artifact?.content_type ?? artifact?.contentType ?? "",
        ),
        original_filename: asText(
          artifact?.original_filename ?? artifact?.originalFilename ?? "",
        ),
        size_bytes: Number.isFinite(sizeNum) ? sizeNum : undefined,
        trashed_at: artifact?.trashed_at ?? artifact?.trashedAt ?? null,
      };
    }
    if (storageId && storageId !== id && artifactRoutesById[id]) {
      artifactRoutesById[storageId] = artifactRoutesById[id];
    }
  }

  for (const event of eventRows) {
    const id = asText(event?.id);
    if (!id || asText(event?.type) !== "message_posted") continue;
    const eventThread = splitTypedRef(event?.thread_ref);
    const eventTopicRef = splitTypedRef(event?.topic_ref);
    const eventTopic =
      eventTopicRef.prefix === "topic"
        ? eventTopicRef.value
        : asText(event?.topic_id || event?.topicId) ||
          ((Array.isArray(event?.refs) ? event.refs : [])
            .map((ref) => splitTypedRef(ref))
            .find((ref) => ref.prefix === "topic")?.value ??
            "");
    eventRoutesById[id] = {
      kind: "message",
      type: "message_posted",
      topicId: eventTopic,
      threadId:
        asText(event?.thread_id || event?.threadId) ||
        (eventThread.prefix === "thread" ? eventThread.value : "") ||
        asText(threadId),
    };
  }

  return { artifactRoutesById, eventRoutesById };
}
