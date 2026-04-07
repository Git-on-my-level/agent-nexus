#!/usr/bin/env node

import {
  failWithPrefix,
  normalizeBaseUrl,
  requestJson,
  sleep,
  waitForCore,
} from "../../scripts/seed-core-lib.mjs";
import { getMockSeedData } from "../src/lib/mockCoreData.js";

const coreBaseUrl = normalizeBaseUrl(
  process.env.OAR_CORE_BASE_URL ?? "http://127.0.0.1:8000",
);
const forceSeed = process.env.OAR_FORCE_SEED === "1";
const skipIfPresent = process.env.OAR_SEED_SKIP_IF_PRESENT !== "0";
const waitTimeoutMs = Number(process.env.OAR_CORE_WAIT_TIMEOUT_MS ?? 20000);

if (!coreBaseUrl) {
  failWithPrefix(
    "seed-core-from-mock failed",
    "OAR_CORE_BASE_URL must be set or defaultable.",
  );
}

const seed = getMockSeedData();
const defaultActorId = seed.actors[0]?.id ?? "actor-ops-ai";

function normalizeSeedCardResolution(raw) {
  const s = String(raw ?? "").trim();
  if (!s || s === "unresolved" || s === "superseded") {
    return "";
  }
  if (s === "completed") {
    return "done";
  }
  if (s === "done" || s === "canceled") {
    return s;
  }
  return "";
}

const threadIdMap = new Map();
const topicIdMap = new Map();
const documentIdMap = new Map();
const boardIdMap = new Map();
const cardIdMap = new Map();

main().catch((error) => {
  const reason = error instanceof Error ? error.message : String(error);
  failWithPrefix("seed-core-from-mock failed", reason);
});

async function main() {
  await waitForCore(coreBaseUrl, waitTimeoutMs, {
    probes: ["/version", "/readyz"],
  });

  if (skipIfPresent && !forceSeed) {
    const alreadySeeded = await detectSeededState();
    if (alreadySeeded) {
      console.log("Seed data already present; skipping.");
      return;
    }
  }

  await seedActors();
  await seedTopics();
  await seedDocuments();
  await seedBoards();
  await seedPackets();
  await seedArtifacts();
  const eventStats = await seedEvents();
  await rebuildDerived();

  console.log(
    `Seed complete. Events posted=${eventStats.posted}, events skipped=${eventStats.skipped}.`,
  );
}

async function detectSeededState() {
  const [actorsBody, topicsBody, boardsBody] = await Promise.all([
    request("GET", "/actors"),
    request("GET", "/topics"),
    request("GET", "/boards"),
  ]);

  const actorIds = new Set(
    (actorsBody?.actors ?? []).map((actor) => actor?.id),
  );
  const threadTitles = new Set(
    (topicsBody?.topics ?? []).map((topic) => String(topic?.title ?? "")),
  );
  const boardCount = (boardsBody?.boards ?? []).length;

  return (
    actorIds.has("actor-ops-ai") &&
    threadTitles.has("Emergency: Lemon Supply Disruption") &&
    boardCount > 0
  );
}

async function seedActors() {
  for (const actor of seed.actors) {
    const body = { actor };
    await request("POST", "/actors", body, [201, 409]);
  }
}

async function seedTopics() {
  const sourceTopics = Array.isArray(seed.topics) ? seed.topics : [];

  for (const sourceTopic of sourceTopics) {
    const actorId = pickActorId(
      sourceTopic.updated_by ?? sourceTopic.created_by,
    );
    const topicPayload = {
      id: sourceTopic.id,
      type: sourceTopic.type,
      title: sourceTopic.title,
      status: sourceTopic.status,
      summary:
        sourceTopic.summary ?? sourceTopic.current_summary ?? sourceTopic.title,
      owner_refs: mapRefs(
        sourceTopic.owner_refs ??
          (sourceTopic.created_by ? [`actor:${sourceTopic.created_by}`] : []),
      ),
      board_refs: mapRefs(sourceTopic.board_refs),
      document_refs: mapRefs(sourceTopic.document_refs),
      related_refs: mapRefs(sourceTopic.related_refs),
      provenance: sourceTopic.provenance,
    };

    const response = await request("POST", "/topics", {
      actor_id: actorId,
      topic: topicPayload,
    });

    const created = response?.topic;
    const newId = String(created?.id ?? "").trim();
    const backingThreadId = String(
      created?.thread_id ?? created?.id ?? "",
    ).trim();
    if (!newId || !backingThreadId) {
      throw new Error(`Topic create returned incomplete data for ${sourceTopic.title}`);
    }

    const sourceTopicId = String(sourceTopic.id ?? "").trim();
    topicIdMap.set(sourceTopicId, newId);
    const topicAlias = topicRefAliasFromThreadLikeId(sourceTopicId);
    if (topicAlias && topicAlias !== sourceTopicId) {
      topicIdMap.set(topicAlias, newId);
    }
    threadIdMap.set(String(sourceTopic.id ?? "").trim(), backingThreadId);
  }
}

async function seedPackets() {
  const sourcePackets =
    Array.isArray(seed.packets) && seed.packets.length > 0
      ? seed.packets
      : Array.isArray(seed.artifacts)
        ? seed.artifacts.filter((artifact) => Boolean(artifact?.packet))
        : [];

  const packetKinds = new Map([
    ["receipt", "/packets/receipts"],
    ["review", "/packets/reviews"],
  ]);

  for (const sourcePacket of sourcePackets) {
    const sourceArtifact = sourcePacket.artifact ?? sourcePacket;
    const kind = String(sourcePacket.kind ?? sourceArtifact.kind ?? "").trim();
    const path = packetKinds.get(kind);
    if (!path) {
      continue;
    }

    const packet = {
      ...sourcePacket.packet,
      subject_ref: mapRef(sourcePacket.subject_ref),
    };
    delete packet.thread_id;

    if (kind === "review") {
      if (packet.receipt_id && !packet.receipt_ref) {
        packet.receipt_ref = `artifact:${String(packet.receipt_id).trim()}`;
      }
      delete packet.receipt_id;
    }

    const payload = {
      actor_id: pickActorId(sourceArtifact.created_by),
      artifact: {
        id: sourceArtifact.id,
        kind,
        summary: sourceArtifact.summary,
        refs: mapRefs(sourceArtifact.refs),
        provenance: sourceArtifact.provenance,
      },
      packet,
    };

    await request("POST", path, payload);
  }
}

async function seedDocuments() {
  const sourceDocuments = Array.isArray(seed.documents) ? seed.documents : [];
  const revisionsByDocument =
    seed.documentRevisions && typeof seed.documentRevisions === "object"
      ? seed.documentRevisions
      : {};

  for (const sourceDocument of sourceDocuments) {
    const documentId = String(sourceDocument.id ?? "").trim();
    if (!documentId) {
      console.warn("Skipping document with no id in mock seed data.");
      continue;
    }

    const revisions = [...(revisionsByDocument[documentId] ?? [])].sort(
      (left, right) => {
        const leftNumber = Number(left?.revision_number ?? 0);
        const rightNumber = Number(right?.revision_number ?? 0);
        return leftNumber - rightNumber;
      },
    );

    if (revisions.length === 0) {
      console.warn(`Skipping document ${documentId}: no revisions found.`);
      continue;
    }

    const firstRevision = revisions[0];
    const actorId = pickActorId(
      firstRevision.created_by ?? sourceDocument.created_by,
    );
    const rawDocumentTopicRef = String(sourceDocument.thread_id ?? "").trim();
    const topicRef = rawDocumentTopicRef
      ? mapRef(
          rawDocumentTopicRef.includes(":")
            ? rawDocumentTopicRef
            : `topic:${rawDocumentTopicRef}`,
        )
      : "";
    const refs = topicRef ? [topicRef] : [];

    let createResponse;
    try {
      createResponse = await requestRetryOnServerError("POST", "/docs", {
        actor_id: actorId,
        document: {
          id: documentId,
          title: sourceDocument.title,
          slug: sourceDocument.slug,
          labels: sourceDocument.labels,
          supersedes: sourceDocument.supersedes,
        },
        refs,
        content: firstRevision.content,
        content_type: normalizeDocumentContentType(firstRevision.content_type),
      });
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (isAlreadyExistsConflict(msg)) {
        documentIdMap.set(documentId, documentId);
        continue;
      }
      throw err;
    }

    const createdDocument = createResponse?.document;
    const createdRevision = createResponse?.revision;
    const newDocumentId = String(createdDocument?.id ?? "").trim();
    let baseRevisionId = String(createdRevision?.revision_id ?? "").trim();

    if (!newDocumentId) {
      throw new Error(`Document create returned no id for ${documentId}`);
    }
    if (!baseRevisionId) {
      throw new Error(
        `Document create returned no revision id for ${documentId}`,
      );
    }

    documentIdMap.set(documentId, newDocumentId);

    for (const revision of revisions.slice(1)) {
      const updateResponse = await requestRetryOnServerError(
        "PATCH",
        `/docs/${encodeURIComponent(newDocumentId)}`,
        {
          actor_id: pickActorId(
            revision.created_by ?? sourceDocument.updated_by,
          ),
          if_base_revision: baseRevisionId,
          refs,
          content: revision.content,
          content_type: normalizeDocumentContentType(revision.content_type),
        },
      );

      baseRevisionId = String(
        updateResponse?.revision?.revision_id ?? "",
      ).trim();
      if (!baseRevisionId) {
        throw new Error(
          `Document update returned no revision id for ${documentId}`,
        );
      }
    }

    if (sourceDocument.trashed_at) {
      await request(
        "POST",
        `/docs/${encodeURIComponent(newDocumentId)}/trash`,
        {
          actor_id: pickActorId(
            sourceDocument.trashed_by ?? sourceDocument.updated_by,
          ),
          reason:
            sourceDocument.trash_reason ??
            "Trashed while seeding mock data.",
        },
      );
    }
  }
}

async function trashSeedArtifactIfNeeded(sourceArtifact) {
  if (!sourceArtifact?.trashed_at) {
    return;
  }
  const id = String(sourceArtifact.id ?? "").trim();
  if (!id) {
    return;
  }
  await request("POST", `/artifacts/${encodeURIComponent(id)}/trash`, {
    actor_id: pickActorId(
      sourceArtifact.trashed_by ?? sourceArtifact.created_by,
    ),
    reason:
      sourceArtifact.trash_reason ?? "Trashed while seeding mock data.",
  });
}

async function seedArtifacts() {
  const packetKinds = new Set(["receipt", "review"]);
  const sourceArtifacts = Array.isArray(seed.artifacts) ? seed.artifacts : [];

  for (const sourceArtifact of sourceArtifacts) {
    const kind = String(sourceArtifact.kind ?? "").trim();
    if (packetKinds.has(kind)) {
      continue;
    }

    const actorId = pickActorId(sourceArtifact.created_by);
    let contentType = "structured";
    let content = {
      artifact_id: sourceArtifact.id,
      summary: sourceArtifact.summary ?? "",
    };

    if (typeof sourceArtifact.content_text === "string") {
      contentType = "text";
      content = sourceArtifact.content_text;
    }

    try {
      await request("POST", "/artifacts", {
        actor_id: actorId,
        artifact: {
          id: sourceArtifact.id,
          kind,
          thread_id: mapThreadId(sourceArtifact.thread_id),
          summary: sourceArtifact.summary,
          refs: mapRefs(sourceArtifact.refs),
          provenance: sourceArtifact.provenance,
        },
        content_type: contentType,
        content,
      });
      await trashSeedArtifactIfNeeded(sourceArtifact);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (isAlreadyExistsConflict(msg)) {
        await trashSeedArtifactIfNeeded(sourceArtifact);
        continue;
      }
      throw err;
    }
  }
}

async function seedBoards() {
  const sourceBoards = Array.isArray(seed.boards) ? seed.boards : [];
  const sourceCards =
    Array.isArray(seed.cards) && seed.cards.length > 0
      ? seed.cards
      : Array.isArray(seed.boardCards)
        ? seed.boardCards
        : [];

  for (const sourceBoard of sourceBoards) {
    const actorId = pickActorId(
      sourceBoard.created_by ?? sourceBoard.updated_by,
    );
    const explicitRefs = mapRefs(sourceBoard.refs);
    const documentRefs = mapRefs(sourceBoard.document_refs);
    const cardRefs = mapRefs(sourceBoard.card_refs);
    const pinnedRefs = mapRefs(sourceBoard.pinned_refs);
    const createResponse = await requestRetryOnServerError("POST", "/boards", {
      actor_id: actorId,
      board: {
        id: sourceBoard.id,
        title: sourceBoard.title,
        status: sourceBoard.status,
        labels: sourceBoard.labels,
        owners: sourceBoard.owners,
        ...(explicitRefs.length > 0 ? { refs: explicitRefs } : {}),
        ...(documentRefs.length > 0 ? { document_refs: documentRefs } : {}),
        ...(cardRefs.length > 0 ? { card_refs: cardRefs } : {}),
        ...(pinnedRefs.length > 0 ? { pinned_refs: pinnedRefs } : {}),
      },
    });

    const createdBoard = createResponse?.board;
    const newBoardId = String(createdBoard?.id ?? "").trim();
    if (!newBoardId) {
      throw new Error(`Board create returned no id for ${sourceBoard.title}`);
    }
    boardIdMap.set(String(sourceBoard.id ?? ""), newBoardId);

    let currentBoard = createdBoard;
    const orderedCards = sourceCards
      .filter(
        (card) => String(card.board_id ?? "") === String(sourceBoard.id ?? ""),
      )
      .sort(compareBoardCardsForSeed);

    const lastAnchorByColumn = new Map();

    for (const sourceCard of orderedCards) {
      const threadId =
        normalizeMappedOptionalThreadRef(sourceCard.thread_id) ||
        normalizeMappedOptionalThreadRef(sourceCard.parent_thread) ||
        normalizeMappedOptionalThreadRef(sourceCard.thread_ref) ||
        normalizeMappedOptionalThreadRef(sourceCard.topic_ref);
      const linkedThreadId = threadId;
      const cardSummaryText =
        String(sourceCard.summary ?? "").trim() ||
        String(sourceCard.title ?? "").trim() ||
        String(sourceCard.body ?? "").trim();

      if (!linkedThreadId && !cardSummaryText) {
        console.warn(
          `Skipping board card on ${newBoardId}: need thread_id, parent_thread, or summary.`,
        );
        continue;
      }
      if (
        linkedThreadId &&
        linkedThreadId === String(currentBoard?.thread_id ?? "").trim()
      ) {
        console.warn(
          `Skipping board card ${linkedThreadId} on ${newBoardId}: backing thread cannot be added as a card.`,
        );
        continue;
      }

      const pinnedDocumentId = mapOptionalDocumentId(
        sourceCard.pinned_document_id,
      );
      const columnKey =
        String(sourceCard.column_key ?? "backlog").trim() || "backlog";
      const afterAnchor = lastAnchorByColumn.get(columnKey);

      const cardSummaryForWrite =
        String(sourceCard.summary ?? "").trim() ||
        String(sourceCard.title ?? "").trim() ||
        String(sourceCard.body ?? "").trim();
      const mappedAssigneeRefs = seedCardAssigneeRefs(sourceCard);
      const rawTopicRef = String(sourceCard.topic_ref ?? "").trim();
      let mappedTopicRef = rawTopicRef
        ? mapRef(rawTopicRef.includes(":") ? rawTopicRef : `topic:${rawTopicRef}`)
        : "";
      const rawRelatedRefs = Array.isArray(sourceCard.related_refs)
        ? sourceCard.related_refs.map((entry) => String(entry ?? "").trim()).filter(Boolean)
        : [];
      const rawSelfBoardRef = `board:${String(sourceCard.board_id ?? newBoardId).trim()}`;

      let mappedRelatedRefs = Array.isArray(sourceCard.related_refs)
        ? mapRefs(sourceCard.related_refs)
        : [];
      const explicitBoardCard =
        Boolean(String(sourceCard.id ?? "").trim()) || Boolean(cardSummaryForWrite);
      if (explicitBoardCard) {
        mappedRelatedRefs = mappedRelatedRefs.filter((ref) => ref !== rawSelfBoardRef);
      }
      const rawThreadRefs = rawRelatedRefs.filter((ref) => ref.startsWith("thread:"));
      if (!mappedTopicRef && explicitBoardCard && rawThreadRefs.length === 1) {
        const rawThreadID = rawThreadRefs[0].slice("thread:".length);
        const inferredTopicID = mapTopicId(rawThreadID);
        const hasTopicForThread =
          topicIdMap.has(rawThreadID) ||
          topicIdMap.has(topicRefAliasFromThreadLikeId(rawThreadID));
        if (inferredTopicID && hasTopicForThread) {
          mappedTopicRef = `topic:${inferredTopicID}`;
          mappedRelatedRefs = mappedRelatedRefs.filter((ref) => !ref.startsWith("thread:"));
        }
      }
      const boardCardRelatedRefs =
        !mappedTopicRef && linkedThreadId
          ? uniqueSeedRefs([...mappedRelatedRefs, `thread:${linkedThreadId}`])
          : mappedRelatedRefs;

      const baseBody = {
        actor_id: pickActorId(sourceCard.created_by ?? sourceCard.updated_by),
        column_key: columnKey,
        ...(mappedTopicRef ? { topic_ref: mappedTopicRef } : {}),
        ...(pinnedDocumentId ? { pinned_document_id: pinnedDocumentId } : {}),
        ...(cardSummaryForWrite
          ? {
              summary: cardSummaryForWrite,
              title: cardSummaryForWrite,
            }
          : {}),
        ...(sourceCard.risk ? { risk: String(sourceCard.risk) } : {}),
        ...(normalizeSeedCardResolution(sourceCard.resolution)
          ? {
              resolution: normalizeSeedCardResolution(sourceCard.resolution),
            }
          : {}),
        ...(boardCardRelatedRefs.length > 0
          ? { related_refs: boardCardRelatedRefs }
          : {}),
        ...(Array.isArray(sourceCard.resolution_refs) &&
        sourceCard.resolution_refs.length > 0
          ? { resolution_refs: mapRefs(sourceCard.resolution_refs) }
          : {}),
        ...(mappedAssigneeRefs.length > 0
          ? { assignee_refs: mappedAssigneeRefs }
          : {}),
      };

      const placementAfter = (anchor) => {
        if (!anchor) {
          return {};
        }
        return { after_card_id: String(anchor) };
      };

      let addResponse;
      try {
        if (linkedThreadId) {
          addResponse = await requestRetryOnServerError(
            "POST",
            `/boards/${encodeURIComponent(newBoardId)}/cards`,
            {
              ...baseBody,
              ...placementAfter(afterAnchor),
            },
          );
        } else {
          addResponse = await requestRetryOnServerError(
            "POST",
            `/boards/${encodeURIComponent(newBoardId)}/cards`,
            {
              ...baseBody,
              ...placementAfter(afterAnchor),
              ...(sourceCard.priority
                ? { priority: String(sourceCard.priority) }
                : {}),
              ...(sourceCard.status ? { status: String(sourceCard.status) } : {}),
            },
          );
        }
      } catch (error) {
        const sourceCardLabel =
          String(sourceCard.id ?? "").trim() ||
          String(sourceCard.thread_id ?? "").trim() ||
          String(sourceCard.summary ?? "").trim() ||
          "(anonymous source card)";
        throw new Error(
          `board ${newBoardId} card ${sourceCardLabel}: ${
            error instanceof Error ? error.message : String(error)
          }`,
        );
      }

      currentBoard = addResponse?.board ?? currentBoard;
      const created = addResponse?.card;
      const createdCardId = String(created?.id ?? "").trim();
      const sourceCardId = String(sourceCard.id ?? "").trim();
      if (sourceCardId && createdCardId) {
        cardIdMap.set(sourceCardId, createdCardId);
      }
      const nextAnchor =
        createdCardId ||
        String(created?.thread_id ?? "").trim() ||
        linkedThreadId ||
        "";
      if (nextAnchor) {
        lastAnchorByColumn.set(columnKey, nextAnchor);
      }
    }
  }
}

async function seedEvents() {
  let posted = 0;
  let skipped = 0;

  const sortedEvents = [...seed.events].sort((a, b) => {
    return String(a?.ts ?? "").localeCompare(String(b?.ts ?? ""));
  });

  for (const sourceEvent of sortedEvents) {
    if (!shouldSeedLegacyEvent(sourceEvent)) {
      continue;
    }
    const actorId = pickActorId(sourceEvent.actor_id);
    const mappedThreadId = mapThreadId(sourceEvent.thread_id);
    const payload = normalizeEventPayload(
      sourceEvent.type,
      sourceEvent.payload,
    );
    const refs = mapRefs(sourceEvent.refs);
    const sourceId = String(sourceEvent.id ?? "").trim();
    const eventPayload = {
      type: sourceEvent.type,
      thread_id: mappedThreadId,
      refs,
      summary: sourceEvent.summary,
      payload,
      provenance: sourceEvent.provenance,
      ...(sourceId ? { id: sourceId } : {}),
    };

    try {
      await request("POST", "/events", {
        actor_id: actorId,
        event: eventPayload,
      });
      posted += 1;
    } catch (error) {
      skipped += 1;
      const reason = error instanceof Error ? error.message : String(error);
      console.warn(
        `Skipping event ${sourceEvent.id} (${sourceEvent.type}): ${reason}`,
      );
    }
  }

  return { posted, skipped };
}

const seedSkippedLegacyEventIds = new Set([
  "evt-price-004",
  "evt-price-006",
  "evt-price-007",
  "evt-price-009",
  "evt-price-010",
  "evt-menu-wo-created",
  "evt-menu-receipt-added",
  "evt-menu-review-completed",
]);

function shouldSeedLegacyEvent(event) {
  const sourceId = String(event?.id ?? "").trim();
  return !seedSkippedLegacyEventIds.has(sourceId);
}

async function rebuildDerived() {
  await request("POST", "/derived/rebuild", { actor_id: defaultActorId });
}

function mapThreadId(threadId) {
  const raw = String(threadId ?? "").trim();
  if (!raw) {
    return raw;
  }

  return threadIdMap.get(raw) ?? raw;
}

function normalizeMappedOptionalThreadRef(ref) {
  const raw = String(ref ?? "").trim();
  if (!raw) {
    return "";
  }

  const separator = raw.indexOf(":");
  const value = separator > 0 ? raw.slice(separator + 1) : raw;
  return threadIdMap.get(value) ?? "";
}

function mapTopicId(topicId) {
  const raw = String(topicId ?? "").trim();
  if (!raw) {
    return raw;
  }

  return topicIdMap.get(raw) ?? raw;
}

function topicRefAliasFromThreadLikeId(topicId) {
  const raw = String(topicId ?? "").trim();
  if (!raw) {
    return "";
  }

  return raw.startsWith("thread-") ? raw.slice("thread-".length) : raw;
}

function mapDocumentId(documentId) {
  const raw = String(documentId ?? "").trim();
  if (!raw) {
    return raw;
  }
  return documentIdMap.get(raw) ?? raw;
}

function mapOptionalDocumentId(documentId) {
  const raw = String(documentId ?? "").trim();
  if (!raw) {
    return "";
  }
  return documentIdMap.get(raw) ?? "";
}

function mapRef(ref) {
  const text = String(ref ?? "").trim();
  if (!text) {
    return text;
  }

  const separator = text.indexOf(":");
  if (separator <= 0) {
    return text;
  }

  const prefix = text.slice(0, separator);
  const value = text.slice(separator + 1);

  if (prefix === "thread") {
    const mapped = mapThreadId(value);
    return `${prefix}:${mapped}`;
  }

  if (prefix === "topic") {
    const mapped = mapTopicId(value);
    return `${prefix}:${mapped}`;
  }

  if (prefix === "card") {
    const mapped = cardIdMap.get(value) ?? value;
    return `${prefix}:${mapped}`;
  }

  if (prefix === "document") {
    const mapped = mapDocumentId(value);
    return `${prefix}:${mapped}`;
  }

  if (prefix === "board") {
    const mapped = boardIdMap.get(value) ?? value;
    return `${prefix}:${mapped}`;
  }

  return text;
}

function mapRefs(values) {
  if (!Array.isArray(values)) {
    return [];
  }

  return values.map((entry) => mapRef(entry)).filter(Boolean);
}

function uniqueSeedRefs(values) {
  return [...new Set((values ?? []).map((entry) => String(entry ?? "").trim()).filter(Boolean))];
}

function seedCardAssigneeRefs(sourceCard) {
  const fromRefs = mapRefs(sourceCard.assignee_refs ?? []);
  if (fromRefs.length > 0) {
    return fromRefs;
  }
  const raw = sourceCard.assignee;
  if (raw == null || String(raw).trim() === "") {
    return [];
  }
  const s = String(raw).trim();
  return mapRefs([s.includes(":") ? s : `actor:${s}`]);
}

function pickActorId(candidate) {
  const id = String(candidate ?? "").trim();
  return id || defaultActorId;
}

function normalizeDocumentContentType(value) {
  const type = String(value ?? "").trim();
  switch (type) {
    case "text":
    case "structured":
    case "binary":
      return type;
    default:
      return "text";
  }
}

function isAlreadyExistsConflict(message) {
  return message.includes("409") && message.includes("already exists");
}

function compareBoardCardsForSeed(left, right) {
  const leftColumn = String(left?.column_key ?? "");
  const rightColumn = String(right?.column_key ?? "");
  const leftColumnOrder = canonicalBoardColumnOrder(leftColumn);
  const rightColumnOrder = canonicalBoardColumnOrder(rightColumn);
  if (leftColumnOrder !== rightColumnOrder) {
    return leftColumnOrder - rightColumnOrder;
  }

  const leftRank = String(left?.rank ?? "");
  const rightRank = String(right?.rank ?? "");
  const rankDelta = leftRank.localeCompare(rightRank);
  if (rankDelta !== 0) {
    return rankDelta;
  }

  return String(
    left?.thread_id ?? left?.topic_ref ?? left?.id ?? "",
  ).localeCompare(
    String(right?.thread_id ?? right?.topic_ref ?? right?.id ?? ""),
  );
}

function canonicalBoardColumnOrder(columnKey) {
  switch (String(columnKey ?? "").trim()) {
    case "backlog":
      return 0;
    case "ready":
      return 1;
    case "in_progress":
      return 2;
    case "blocked":
      return 3;
    case "review":
      return 4;
    case "done":
      return 5;
    default:
      return 99;
  }
}

function normalizeEventPayload(type, payload) {
  const next = payload && typeof payload === "object" ? { ...payload } : {};

  if (type === "exception_raised" && !String(next.subtype ?? "").trim()) {
    next.subtype = "stale_thread";
  }
  if (
    type === "exception_raised" &&
    !String(next["subtype (e.g. stale_thread)"] ?? "").trim()
  ) {
    next["subtype (e.g. stale_thread)"] = String(
      next.subtype ?? "stale_thread",
    );
  }

  return next;
}

async function request(method, path, body, okStatuses = [200, 201]) {
  return requestJson(coreBaseUrl, method, path, body, okStatuses);
}

/** Retries POSTs that fail with 5xx (e.g. brief SQLite contention right after core startup). */
async function requestRetryOnServerError(
  method,
  path,
  body,
  okStatuses = [200, 201],
  { attempts = 4, baseDelayMs = 200 } = {},
) {
  let lastError;
  for (let attempt = 0; attempt < attempts; attempt++) {
    try {
      return await request(method, path, body, okStatuses);
    } catch (err) {
      lastError = err;
      const msg = err instanceof Error ? err.message : String(err);
      const is5xx = /->\s5\d\d:/.test(msg);
      if (!is5xx || attempt === attempts - 1) {
        throw err;
      }
      await sleep(baseDelayMs * (attempt + 1));
    }
  }
  throw lastError;
}
