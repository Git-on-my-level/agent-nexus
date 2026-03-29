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
  await seedThreads();
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
  const [actorsBody, threadsBody, boardsBody] = await Promise.all([
    request("GET", "/actors"),
    request("GET", "/threads"),
    request("GET", "/boards"),
  ]);

  const actorIds = new Set((actorsBody?.actors ?? []).map((actor) => actor?.id));
  const threadTitles = new Set(
    (threadsBody?.threads ?? []).map((thread) => String(thread?.title ?? "")),
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

async function seedThreads() {
  for (const sourceThread of seed.threads) {
    const actorId = pickActorId(sourceThread.updated_by);
    const threadPayload = {
      type: sourceThread.type,
      title: sourceThread.title,
      status: sourceThread.status,
      priority: sourceThread.priority,
      tags: sourceThread.tags,
      key_artifacts: normalizeArtifactRefs(sourceThread.key_artifacts),
      cadence: sourceThread.cadence,
      current_summary: sourceThread.current_summary,
      next_actions: sourceThread.next_actions,
      next_check_in_at: sourceThread.next_check_in_at,
      provenance: sourceThread.provenance,
    };

    const response = await request("POST", "/threads", {
      actor_id: actorId,
      thread: threadPayload,
    });

    const created = response?.thread;
    const newId = String(created?.id ?? "").trim();
    if (!newId) {
      throw new Error(`Thread create returned no id for ${sourceThread.title}`);
    }

    threadIdMap.set(sourceThread.id, newId);
    snapshotIdMap.set(sourceThread.id, newId);
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
      throw new Error(`Commitment create returned no id for ${sourceCommitment.title}`);
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
          actor_id: pickActorId(revision.created_by ?? sourceDocument.updated_by),
          if_base_revision: baseRevisionId,
          refs,
          content: revision.content,
          content_type: normalizeDocumentContentType(revision.content_type),
        },
      );

      baseRevisionId = String(updateResponse?.revision?.revision_id ?? "").trim();
      if (!baseRevisionId) {
        throw new Error(
          `Document update returned no revision id for ${documentId}`,
        );
      }
    }

    if (sourceDocument.tombstoned_at) {
      await request("POST", `/docs/${encodeURIComponent(newDocumentId)}/tombstone`, {
        actor_id: pickActorId(
          sourceDocument.tombstoned_by ?? sourceDocument.updated_by,
        ),
        reason:
          sourceDocument.tombstone_reason ??
          "Tombstoned while seeding mock data.",
      });
    }
  }
}

async function seedArtifacts() {
  const packetOrder = {
    work_order: 1,
    receipt: 2,
    review: 3,
  };

  const sortedArtifacts = [...seed.artifacts].sort((a, b) => {
    const aOrder = packetOrder[String(a?.kind ?? "")] ?? 0;
    const bOrder = packetOrder[String(b?.kind ?? "")] ?? 0;
    return aOrder - bOrder;
  });

  for (const sourceArtifact of sortedArtifacts) {
    const actorId = pickActorId(sourceArtifact.created_by);
    const kind = String(sourceArtifact.kind ?? "").trim();

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
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        if (isAlreadyExistsConflict(msg)) {
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
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        if (isAlreadyExistsConflict(msg)) {
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
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        if (isAlreadyExistsConflict(msg)) {
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

    if (sourceArtifact.packet && typeof sourceArtifact.packet === "object") {
      contentType = "structured";
      content = sourceArtifact.packet;
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
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (isAlreadyExistsConflict(msg)) {
        continue;
      }
      throw err;
    }
  }
}

async function seedBoards() {
  const sourceBoards = Array.isArray(seed.boards) ? seed.boards : [];
  const sourceCards = Array.isArray(seed.boardCards) ? seed.boardCards : [];

  for (const sourceBoard of sourceBoards) {
    const primaryThreadId = normalizeMappedOptionalThreadId(
      sourceBoard.primary_thread_id,
    );
    if (!primaryThreadId) {
      console.warn(
        `Skipping board ${String(sourceBoard.id ?? "<unknown>")}: primary thread is not seedable.`,
      );
      continue;
    }

    const actorId = pickActorId(sourceBoard.created_by ?? sourceBoard.updated_by);
    const primaryDocumentId = mapOptionalDocumentId(
      sourceBoard.primary_document_id,
    );
    const createResponse = await requestRetryOnServerError("POST", "/boards", {
      actor_id: actorId,
      board: {
        id: sourceBoard.id,
        title: sourceBoard.title,
        status: sourceBoard.status,
        labels: sourceBoard.labels,
        owners: sourceBoard.owners,
        primary_thread_id: primaryThreadId,
        ...(primaryDocumentId
          ? { primary_document_id: primaryDocumentId }
          : {}),
        column_schema: sourceBoard.column_schema,
        pinned_refs: mapRefs(sourceBoard.pinned_refs),
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
      .filter((card) => String(card.board_id ?? "") === String(sourceBoard.id ?? ""))
      .sort(compareBoardCardsForSeed);

    const lastThreadByColumn = new Map();

    for (const sourceCard of orderedCards) {
      const threadId = normalizeMappedOptionalThreadId(sourceCard.thread_id);
      if (!threadId) {
        console.warn(
          `Skipping board card ${String(sourceCard.thread_id ?? "<unknown>")} on ${newBoardId}: thread is not seedable.`,
        );
        continue;
      }
      if (threadId === primaryThreadId) {
        console.warn(
          `Skipping board card ${threadId} on ${newBoardId}: primary thread cannot be added as a card.`,
        );
        continue;
      }

      const pinnedDocumentId = mapOptionalDocumentId(
        sourceCard.pinned_document_id,
      );
      const columnKey = String(sourceCard.column_key ?? "backlog").trim() || "backlog";
      const afterThreadId = lastThreadByColumn.get(columnKey);
      const boardUpdatedAt = String(currentBoard?.updated_at ?? "").trim();
      const addResponse = await requestRetryOnServerError(
        "POST",
        `/boards/${encodeURIComponent(newBoardId)}/cards`,
        {
          actor_id: pickActorId(sourceCard.created_by ?? sourceCard.updated_by),
          ...(boardUpdatedAt ? { if_board_updated_at: boardUpdatedAt } : {}),
          thread_id: threadId,
          column_key: columnKey,
          ...(afterThreadId ? { after_thread_id: afterThreadId } : {}),
          ...(pinnedDocumentId ? { pinned_document_id: pinnedDocumentId } : {}),
        },
      );

      currentBoard = addResponse?.board ?? currentBoard;
      lastThreadByColumn.set(columnKey, threadId);
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
    const payload = normalizeEventPayload(sourceEvent.type, sourceEvent.payload);
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

function normalizeArtifactRefs(values) {
  if (!Array.isArray(values)) {
    return [];
  }

  return values
    .map((entry) => String(entry ?? "").trim())
    .filter(Boolean)
    .map((entry) => (entry.includes(":") ? mapRef(entry) : `artifact:${entry}`));
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

  return String(left?.thread_id ?? "").localeCompare(
    String(right?.thread_id ?? ""),
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
    next["subtype (e.g. stale_thread)"] = String(next.subtype ?? "stale_thread");
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
