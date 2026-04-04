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

const threadIdMap = new Map();
const documentIdMap = new Map();
const boardIdMap = new Map();
const snapshotIdMap = new Map();

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
  await seedCommitments();
  await seedDocuments();
  await seedArtifacts();
  await seedBoards();
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
  const sourceTopics =
    Array.isArray(seed.topics) && seed.topics.length > 0
      ? seed.topics
      : Array.isArray(seed.threads)
        ? seed.threads
        : [];

  for (const sourceTopic of sourceTopics) {
    const actorId = pickActorId(
      sourceTopic.updated_by ?? sourceTopic.created_by,
    );
    const topicPayload = {
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
      ...(sourceTopic.primary_thread_ref
        ? { primary_thread_ref: String(sourceTopic.primary_thread_ref) }
        : {}),
    };

    const response = await request("POST", "/topics", {
      actor_id: actorId,
      topic: topicPayload,
    });

    const created = response?.topic;
    const newId = String(created?.id ?? "").trim();
    if (!newId) {
      throw new Error(`Topic create returned no id for ${sourceTopic.title}`);
    }

    threadIdMap.set(String(sourceTopic.id ?? "").trim(), newId);
    snapshotIdMap.set(String(sourceTopic.id ?? "").trim(), newId);
  }
}

async function seedCommitments() {
  for (const sourceCommitment of seed.commitments) {
    const actorId = pickActorId(sourceCommitment.updated_by);
    const newThreadId = mustMapThreadId(sourceCommitment.thread_id);

    const payload = {
      thread_id: newThreadId,
      title: sourceCommitment.title,
      owner: sourceCommitment.owner,
      due_at: sourceCommitment.due_at,
      status: sourceCommitment.status,
      definition_of_done: sourceCommitment.definition_of_done,
      links: mapRefs(sourceCommitment.links),
      provenance: sourceCommitment.provenance,
    };

    const response = await request("POST", "/commitments", {
      actor_id: actorId,
      commitment: payload,
    });

    const created = response?.commitment;
    const newId = String(created?.id ?? "").trim();
    if (!newId) {
      throw new Error(
        `Commitment create returned no id for ${sourceCommitment.title}`,
      );
    }

    snapshotIdMap.set(sourceCommitment.id, newId);
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
    const threadId = normalizeMappedOptionalThreadId(sourceDocument.thread_id);
    const refs = threadId ? [`thread:${threadId}`] : [];

    let createResponse;
    try {
      createResponse = await request("POST", "/docs", {
        actor_id: actorId,
        document: {
          id: documentId,
          title: sourceDocument.title,
          slug: sourceDocument.slug,
          status: sourceDocument.status,
          labels: sourceDocument.labels,
          supersedes: sourceDocument.supersedes,
          ...(threadId ? { thread_id: threadId } : {}),
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
      const updateResponse = await request(
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

    if (sourceDocument.tombstoned_at) {
      await request(
        "POST",
        `/docs/${encodeURIComponent(newDocumentId)}/tombstone`,
        {
          actor_id: pickActorId(
            sourceDocument.tombstoned_by ?? sourceDocument.updated_by,
          ),
          reason:
            sourceDocument.tombstone_reason ??
            "Tombstoned while seeding mock data.",
        },
      );
    }
  }
}

async function tombstoneSeedArtifactIfNeeded(sourceArtifact) {
  if (!sourceArtifact?.tombstoned_at) {
    return;
  }
  const id = String(sourceArtifact.id ?? "").trim();
  if (!id) {
    return;
  }
  await request("POST", `/artifacts/${encodeURIComponent(id)}/tombstone`, {
    actor_id: pickActorId(
      sourceArtifact.tombstoned_by ?? sourceArtifact.created_by,
    ),
    reason:
      sourceArtifact.tombstone_reason ?? "Tombstoned while seeding mock data.",
  });
}

async function seedArtifacts() {
  const packetOrder = {
    work_order: 1,
    receipt: 2,
    review: 3,
  };

  const sourcePackets =
    Array.isArray(seed.packets) && seed.packets.length > 0
      ? seed.packets
      : Array.isArray(seed.artifacts)
        ? seed.artifacts
            .filter((artifact) => Boolean(artifact?.packet))
            .map((artifact) => ({
              id: artifact.id,
              kind: artifact.kind,
              artifact,
              packet: artifact.packet,
            }))
        : [];

  const sortedArtifacts = [...sourcePackets].sort((a, b) => {
    const aOrder = packetOrder[String(a?.kind ?? "")] ?? 0;
    const bOrder = packetOrder[String(b?.kind ?? "")] ?? 0;
    return aOrder - bOrder;
  });

  for (const sourcePacket of sortedArtifacts) {
    const sourceArtifact = sourcePacket.artifact ?? sourcePacket;
    const actorId = pickActorId(sourceArtifact.created_by);
    const kind = String(sourceArtifact.kind ?? sourcePacket.kind ?? "").trim();

    if (kind === "work_order") {
      try {
        await request("POST", "/work_orders", {
          actor_id: actorId,
          artifact: {
            id: sourceArtifact.id,
            kind,
            thread_id: mapThreadId(sourceArtifact.thread_id),
            summary: sourceArtifact.summary,
            refs: mapRefs(sourceArtifact.refs),
            provenance: sourceArtifact.provenance,
          },
          packet: {
            ...sourceArtifact.packet,
            thread_id: mapThreadId(sourceArtifact.packet?.thread_id),
            context_refs: mapRefs(sourceArtifact.packet?.context_refs),
          },
        });
        await tombstoneSeedArtifactIfNeeded(sourceArtifact);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        if (isAlreadyExistsConflict(msg)) {
          await tombstoneSeedArtifactIfNeeded(sourceArtifact);
          continue;
        }
        throw err;
      }
      continue;
    }

    if (kind === "receipt") {
      try {
        await request("POST", "/receipts", {
          actor_id: actorId,
          artifact: {
            id: sourceArtifact.id,
            kind,
            thread_id: mapThreadId(sourceArtifact.thread_id),
            summary: sourceArtifact.summary,
            refs: mapRefs(sourceArtifact.refs),
            provenance: sourceArtifact.provenance,
          },
          packet: {
            ...sourceArtifact.packet,
            thread_id: mapThreadId(sourceArtifact.packet?.thread_id),
            outputs: mapRefs(sourceArtifact.packet?.outputs),
            verification_evidence: mapRefs(
              sourceArtifact.packet?.verification_evidence,
            ),
          },
        });
        await tombstoneSeedArtifactIfNeeded(sourceArtifact);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        if (isAlreadyExistsConflict(msg)) {
          await tombstoneSeedArtifactIfNeeded(sourceArtifact);
          continue;
        }
        throw err;
      }
      continue;
    }

    if (kind === "review") {
      try {
        await request("POST", "/reviews", {
          actor_id: actorId,
          artifact: {
            id: sourceArtifact.id,
            kind,
            thread_id: mapThreadId(sourceArtifact.thread_id),
            summary: sourceArtifact.summary,
            refs: mapRefs(sourceArtifact.refs),
            provenance: sourceArtifact.provenance,
          },
          packet: {
            ...sourceArtifact.packet,
            evidence_refs: mapRefs(sourceArtifact.packet?.evidence_refs),
          },
        });
        await tombstoneSeedArtifactIfNeeded(sourceArtifact);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        if (isAlreadyExistsConflict(msg)) {
          await tombstoneSeedArtifactIfNeeded(sourceArtifact);
          continue;
        }
        throw err;
      }
      continue;
    }

    let contentType = "structured";
    let content = {
      artifact_id: sourceArtifact.id,
      summary: sourceArtifact.summary ?? "",
    };

    if (sourcePacket.packet && typeof sourcePacket.packet === "object") {
      contentType = "structured";
      content = sourcePacket.packet;
    } else if (typeof sourceArtifact.content_text === "string") {
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
      await tombstoneSeedArtifactIfNeeded(sourceArtifact);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (isAlreadyExistsConflict(msg)) {
        await tombstoneSeedArtifactIfNeeded(sourceArtifact);
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
    const primaryThreadRef = String(
      sourceBoard.primary_topic_ref ?? sourceBoard.primary_thread_ref ?? "",
    ).trim();
    const primaryThreadId = normalizeMappedOptionalThreadId(
      sourceBoard.primary_thread_id ?? primaryThreadRef,
    );
    if (!primaryThreadId) {
      console.warn(
        `Skipping board ${String(sourceBoard.id ?? "<unknown>")}: primary thread is not seedable.`,
      );
      continue;
    }

    const actorId = pickActorId(
      sourceBoard.created_by ?? sourceBoard.updated_by,
    );
    const primaryDocumentId = mapOptionalDocumentId(
      sourceBoard.primary_document_id,
    );
    const primaryTopicRef = String(
      sourceBoard.primary_topic_ref ?? sourceBoard.primary_thread_ref ?? "",
    ).trim();
    const documentRefs = mapRefs(
      sourceBoard.document_refs ??
        (primaryDocumentId ? [`document:${primaryDocumentId}`] : []),
    );
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
        ...(primaryTopicRef ? { primary_topic_ref: primaryTopicRef } : {}),
        primary_thread_id: primaryThreadId,
        ...(primaryDocumentId
          ? { primary_document_id: primaryDocumentId }
          : {}),
        ...(documentRefs.length > 0 ? { document_refs: documentRefs } : {}),
        ...(cardRefs.length > 0 ? { card_refs: cardRefs } : {}),
        column_schema: sourceBoard.column_schema,
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
      const threadId = normalizeMappedOptionalThreadId(sourceCard.thread_id);
      const parentThreadId = normalizeMappedOptionalThreadId(
        sourceCard.parent_thread,
      );
      const linkedThreadId = threadId || parentThreadId;
      const standaloneTitle = String(sourceCard.title ?? "").trim();

      if (!linkedThreadId && !standaloneTitle) {
        console.warn(
          `Skipping board card on ${newBoardId}: need thread_id, parent_thread, or title.`,
        );
        continue;
      }
      if (linkedThreadId && linkedThreadId === primaryThreadId) {
        console.warn(
          `Skipping board card ${linkedThreadId} on ${newBoardId}: primary thread cannot be added as a card.`,
        );
        continue;
      }

      const pinnedDocumentId = mapOptionalDocumentId(
        sourceCard.pinned_document_id,
      );
      const columnKey =
        String(sourceCard.column_key ?? "backlog").trim() || "backlog";
      const afterAnchor = lastAnchorByColumn.get(columnKey);
      const boardUpdatedAt = String(currentBoard?.updated_at ?? "").trim();

      const baseBody = {
        actor_id: pickActorId(sourceCard.created_by ?? sourceCard.updated_by),
        ...(boardUpdatedAt ? { if_board_updated_at: boardUpdatedAt } : {}),
        column_key: columnKey,
        ...(threadId
          ? {
              topic_ref: `topic:${threadId}`,
              thread_ref: `thread:${threadId}`,
            }
          : {}),
        ...(pinnedDocumentId ? { pinned_document_id: pinnedDocumentId } : {}),
        ...(String(sourceCard.title ?? "").trim()
          ? { title: String(sourceCard.title).trim() }
          : {}),
        ...(String(sourceCard.summary ?? "").trim()
          ? { summary: String(sourceCard.summary).trim() }
          : {}),
        ...(sourceCard.risk ? { risk: String(sourceCard.risk) } : {}),
        ...(sourceCard.resolution
          ? { resolution: String(sourceCard.resolution) }
          : {}),
        ...(Array.isArray(sourceCard.related_refs) &&
        sourceCard.related_refs.length > 0
          ? { related_refs: mapRefs(sourceCard.related_refs) }
          : {}),
        ...(Array.isArray(sourceCard.resolution_refs) &&
        sourceCard.resolution_refs.length > 0
          ? { resolution_refs: mapRefs(sourceCard.resolution_refs) }
          : {}),
        ...(Array.isArray(sourceCard.assignee_refs) &&
        sourceCard.assignee_refs.length > 0
          ? { assignee_refs: mapRefs(sourceCard.assignee_refs) }
          : {}),
      };

      const placementAfter = (anchor) => {
        if (!anchor) {
          return {};
        }
        const a = String(anchor);
        if (a.startsWith("thread-")) {
          return { after_thread_id: a };
        }
        return { after_card_id: a };
      };

      let addResponse;
      if (linkedThreadId) {
        addResponse = await requestRetryOnServerError(
          "POST",
          `/boards/${encodeURIComponent(newBoardId)}/cards`,
          {
            ...baseBody,
            thread_id: linkedThreadId,
            ...placementAfter(afterAnchor),
          },
        );
      } else {
        addResponse = await requestRetryOnServerError(
          "POST",
          `/boards/${encodeURIComponent(newBoardId)}/cards`,
          {
            ...baseBody,
            title: standaloneTitle,
            ...(sourceCard.body ? { body: String(sourceCard.body) } : {}),
            ...placementAfter(afterAnchor),
            ...(sourceCard.assignee
              ? { assignee: String(sourceCard.assignee) }
              : {}),
            ...(sourceCard.priority
              ? { priority: String(sourceCard.priority) }
              : {}),
            ...(sourceCard.status ? { status: String(sourceCard.status) } : {}),
          },
        );
      }

      currentBoard = addResponse?.board ?? currentBoard;
      const created = addResponse?.card;
      const nextAnchor =
        String(created?.id ?? "").trim() ||
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
    const actorId = pickActorId(sourceEvent.actor_id);
    const mappedThreadId = mapThreadId(sourceEvent.thread_id);
    const payload = normalizeEventPayload(
      sourceEvent.type,
      sourceEvent.payload,
    );
    const refs = normalizeEventRefs(
      sourceEvent.type,
      mapRefs(sourceEvent.refs),
      mappedThreadId,
    );
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

function normalizeMappedOptionalThreadId(threadId) {
  const raw = String(threadId ?? "").trim();
  if (!raw) {
    return "";
  }
  return threadIdMap.get(raw) ?? "";
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

function mustMapThreadId(threadId) {
  const mapped = mapThreadId(threadId);
  if (!mapped) {
    throw new Error(`Missing thread mapping for ${String(threadId ?? "")}`);
  }
  return mapped;
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

  if (prefix === "snapshot") {
    const mapped = snapshotIdMap.get(value) ?? value;
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

function normalizeEventRefs(type, refs, mappedThreadId) {
  const nextRefs = Array.isArray(refs) ? [...refs] : [];

  if (type === "snapshot_updated") {
    const hasSnapshotRef = nextRefs.some((ref) => ref.startsWith("snapshot:"));
    if (!hasSnapshotRef && mappedThreadId) {
      nextRefs.push(`snapshot:${mappedThreadId}`);
    }
  }

  return nextRefs;
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
