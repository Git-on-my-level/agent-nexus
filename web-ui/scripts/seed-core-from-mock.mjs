#!/usr/bin/env node

import { createPrivateKey, generateKeyPairSync, sign } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  failWithPrefix,
  normalizeBaseUrl,
  parseJson,
  requestJson,
  sleep,
  waitForCore,
} from "../../scripts/seed-core-lib.mjs";
import {
  listDevSeedInboxSubjectRefViolations,
  listDevSeedThreadRefViolations,
} from "../src/lib/devWorkspaceFixtures.js";
import { getDevSeedScenarioConfig } from "./dev-seed-scenarios.mjs";
import {
  listResolutionEvidenceViolations,
  resolutionEvidenceToCreateBeforeCard,
} from "./seed-resolution-evidence.mjs";

const coreBaseUrl = normalizeBaseUrl(
  process.env.ANX_CORE_BASE_URL ?? "http://127.0.0.1:8000",
);
const forceSeed = process.env.ANX_FORCE_SEED === "1";
const skipIfPresent = process.env.ANX_SEED_SKIP_IF_PRESENT !== "0";
const waitTimeoutMs = Number(process.env.ANX_CORE_WAIT_TIMEOUT_MS ?? 20000);
const scenarioName = String(
  process.env.ANX_DEV_SEED_SCENARIO ?? "default",
).trim();
const devIdentityBundlePath = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
  ".dev",
  "local-identities.json",
);

if (!coreBaseUrl) {
  failWithPrefix(
    "seed-core-from-mock failed",
    "ANX_CORE_BASE_URL must be set or defaultable.",
  );
}

const scenarioConfig = getDevSeedScenarioConfig(scenarioName);
if (!scenarioConfig) {
  const requested = scenarioName || "default";
  failWithPrefix(
    "seed-core-from-mock failed",
    `unknown dev seed scenario: ${requested}`,
  );
}

const seed = scenarioConfig.getSeedData();
const seedPersonas = Array.isArray(scenarioConfig.personas)
  ? scenarioConfig.personas
  : [];
const defaultActorId = seed.actors[0]?.id ?? scenarioConfig.defaultActorId;

const threadRefViolations = listDevSeedThreadRefViolations(seed);
if (threadRefViolations.length > 0) {
  failWithPrefix(
    "seed-core-from-mock failed",
    `dev seed thread ref integrity:\n${threadRefViolations.join("\n")}`,
  );
}

const inboxSubjectRefViolations = listDevSeedInboxSubjectRefViolations(seed);
if (inboxSubjectRefViolations.length > 0) {
  failWithPrefix(
    "seed-core-from-mock failed",
    `dev seed inbox subject ref integrity:\n${inboxSubjectRefViolations.join("\n")}`,
  );
}

const resolutionEvidenceViolations = listResolutionEvidenceViolations(seed);
if (resolutionEvidenceViolations.length > 0) {
  failWithPrefix(
    "seed-core-from-mock failed",
    `dev seed resolution evidence order:\n${resolutionEvidenceViolations.join("\n")}`,
  );
}

function normalizeSeedCardResolution(raw) {
  const s = String(raw ?? "").trim();
  if (!s || s === "unresolved" || s === "superseded") {
    return "";
  }
  if (s === "completed") {
    return "done";
  }
  if (s === "done") {
    return s;
  }
  return "";
}

const threadIdMap = new Map();
const topicIdMap = new Map();
const documentIdMap = new Map();
const boardIdMap = new Map();
const cardIdMap = new Map();
const postedEventIds = new Set();
const seededArtifactIds = new Set();

main().catch((error) => {
  const reason = error instanceof Error ? error.message : String(error);
  failWithPrefix("seed-core-from-mock failed", reason);
});

async function main() {
  await waitForCore(coreBaseUrl, waitTimeoutMs, {
    probes: ["/version", "/readyz"],
  });

  console.log(`Using dev seed scenario: ${scenarioName || "default"}`);

  let domainSeeded = true;
  if (skipIfPresent && !forceSeed) {
    const alreadySeeded = await detectSeededState();
    if (alreadySeeded) {
      console.log("Seed data already present; skipping domain seed.");
      domainSeeded = false;
    }
  }

  let identityBundle = [];
  if (domainSeeded) {
    await seedActors();
    await seedTopics();
    await seedDocuments();
    // Artifacts (and packets) that done cards cite as resolution evidence must
    // exist before those cards are created.
    await seedPackets();
    await seedArtifacts();
    await seedBoards();
    await applySeedTopicAndBoardLifecycle();
    // Derive seeded personas under the dev host before mention-heavy events.
    if (process.env.ANX_DEV_SEED_IDENTITIES === "1") {
      identityBundle = (await seedDevFixtureIdentities()) ?? [];
    }
    const eventStats = await seedEvents();
    await rebuildDerived();
    await seedCommandCenter(identityBundle);

    console.log(
      `Seed complete. Events posted=${eventStats.posted}, events skipped=${eventStats.skipped}.`,
    );
  } else if (process.env.ANX_DEV_SEED_IDENTITIES === "1") {
    identityBundle = (await seedDevFixtureIdentities()) ?? [];
    await seedCommandCenter(identityBundle);
  }
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
  const boardTitles = new Set(
    (boardsBody?.boards ?? []).map((boardEntry) =>
      String(boardEntry?.title ?? boardEntry?.board?.title ?? ""),
    ),
  );
  const boardCount = (boardsBody?.boards ?? []).length;

  const basicScenarioMarkersPresent =
    actorIds.has(scenarioConfig.detectActorId) &&
    threadTitles.has(scenarioConfig.detectTopicTitle) &&
    (!scenarioConfig.requireBoards || boardCount > 0) &&
    (!scenarioConfig.detectBoardTitle ||
      boardTitles.has(scenarioConfig.detectBoardTitle));

  if (!basicScenarioMarkersPresent) {
    return false;
  }

  const needsDocumentRevisionCheck = expectedDocumentRevisionCounts().size > 0;
  const needsCardCheck = expectedSeedCards().length > 0;
  const needsEventCheck = expectedSeedEventIDs().length > 0;
  const needsIdentityCheck =
    process.env.ANX_DEV_SEED_IDENTITIES === "1" && seedPersonas.length > 0;

  if (
    !needsDocumentRevisionCheck &&
    !needsCardCheck &&
    !needsEventCheck &&
    !needsIdentityCheck
  ) {
    return true;
  }

  const [docsBody, cardsBody, eventsBody] = await Promise.all([
    needsDocumentRevisionCheck
      ? request("GET", "/docs")
      : Promise.resolve(null),
    needsCardCheck ? request("GET", "/cards") : Promise.resolve(null),
    needsEventCheck
      ? request(
          "GET",
          `/events?limit=${Math.max(expectedSeedEventIDs().length + 20, 200)}`,
        )
      : Promise.resolve(null),
  ]);

  const domainReady =
    hasExpectedDocumentRevisions(docsBody) &&
    hasExpectedCards(cardsBody) &&
    hasExpectedEvents(eventsBody);
  if (!domainReady) {
    return false;
  }
  if (!needsIdentityCheck) {
    return true;
  }
  return hasExpectedIdentityRegistrations();
}

function expectedDocumentRevisionCounts() {
  const revisionsByDocument =
    seed.documentRevisions && typeof seed.documentRevisions === "object"
      ? seed.documentRevisions
      : {};
  const counts = new Map();

  for (const [documentId, revisions] of Object.entries(revisionsByDocument)) {
    const normalizedId = String(documentId ?? "").trim();
    if (!normalizedId || !Array.isArray(revisions) || revisions.length === 0) {
      continue;
    }
    counts.set(normalizedId, revisions.length);
  }

  return counts;
}

function hasExpectedDocumentRevisions(docsBody) {
  const expectedCounts = expectedDocumentRevisionCounts();
  if (expectedCounts.size === 0) {
    return true;
  }

  const actualCounts = new Map();
  for (const document of docsBody?.documents ?? []) {
    const documentId = String(document?.id ?? "").trim();
    if (!documentId) {
      continue;
    }
    const revisionNumber =
      Number(document?.head_revision_number ?? 0) ||
      Number(document?.head_revision?.revision_number ?? 0);
    actualCounts.set(documentId, revisionNumber);
  }

  for (const [documentId, expectedCount] of expectedCounts.entries()) {
    if ((actualCounts.get(documentId) ?? 0) < expectedCount) {
      return false;
    }
  }

  return true;
}

function expectedSeedCards() {
  return Array.isArray(seed.cards) ? seed.cards : [];
}

function expectedSeedEventIDs() {
  return Array.isArray(seed.events)
    ? seed.events.map((event) => String(event?.id ?? "").trim()).filter(Boolean)
    : [];
}

function hasExpectedCards(cardsBody) {
  const expectedCards = expectedSeedCards();
  if (expectedCards.length === 0) {
    return true;
  }

  const actualCards = Array.isArray(cardsBody?.cards) ? cardsBody.cards : [];
  if (actualCards.length < expectedCards.length) {
    return false;
  }

  const actualCardIds = new Set(
    actualCards.map((card) => String(card?.id ?? "").trim()).filter(Boolean),
  );
  const actualSummaries = new Set(
    actualCards
      .map((card) => String(card?.summary ?? "").trim())
      .filter(Boolean),
  );

  return expectedCards.every((card) => {
    const expectedId = String(card?.id ?? "").trim();
    if (expectedId && actualCardIds.has(expectedId)) {
      return true;
    }
    const expectedSummary = String(card?.summary ?? card?.title ?? "").trim();
    return Boolean(expectedSummary) && actualSummaries.has(expectedSummary);
  });
}

function hasExpectedEvents(eventsBody) {
  const expectedIDs = expectedSeedEventIDs();
  if (expectedIDs.length === 0) {
    return true;
  }

  const actualEventIDs = new Set(
    (eventsBody?.events ?? [])
      .map((event) => String(event?.id ?? "").trim())
      .filter(Boolean),
  );
  return expectedIDs.every((eventID) => actualEventIDs.has(eventID));
}

async function hasExpectedIdentityRegistrations() {
  const bundle = await readDevIdentityBundle();
  if (
    !bundle ||
    !Array.isArray(bundle.personas) ||
    bundle.personas.length === 0
  ) {
    return false;
  }

  const probePersona = bundle.personas.find((persona) =>
    String(persona?.refresh_token ?? "").trim(),
  );
  if (!probePersona) {
    return false;
  }

  let accessToken = "";
  try {
    const tokenResponse = await requestJson(
      coreBaseUrl,
      "POST",
      "/auth/token",
      {
        grant_type: "refresh_token",
        refresh_token: probePersona.refresh_token,
      },
    );
    accessToken = String(tokenResponse?.tokens?.access_token ?? "").trim();
  } catch {
    return false;
  }
  if (!accessToken) {
    return false;
  }

  let principalsBody;
  try {
    principalsBody = await requestAuthJson(
      "GET",
      "/auth/principals",
      undefined,
      accessToken,
      [200],
    );
  } catch {
    return false;
  }

  const principals = Array.isArray(principalsBody?.principals)
    ? principalsBody.principals
    : [];
  // Match by actor_id: passkey dev registration derives usernames from display_name
  // (passkey.*) while fixtures use stable dev.* labels — actor_id is authoritative.
  return seedPersonas.every((persona) =>
    principals.some(
      (principal) =>
        String(principal?.actor_id ?? "").trim() ===
          String(persona?.actor_id ?? "").trim() &&
        principal?.wake_routing?.taggable === true,
    ),
  );
}

async function readDevIdentityBundle() {
  try {
    const raw = await readFile(devIdentityBundlePath, "utf8");
    return parseJson(raw);
  } catch {
    return null;
  }
}

async function seedActors() {
  for (const actor of seed.actors) {
    const body = { actor };
    await requestRetryOnServerError("POST", "/actors", body, [201, 409]);
  }
}

async function applySeedTopicAndBoardLifecycle() {
  const sourceTopics = Array.isArray(seed.topics) ? seed.topics : [];
  for (const sourceTopic of sourceTopics) {
    const life = String(sourceTopic.dev_seed_topic_lifecycle ?? "").trim();
    if (life !== "archive" && life !== "trash") {
      continue;
    }
    const sourceTopicId = String(sourceTopic.id ?? "").trim();
    const newId = topicIdMap.get(sourceTopicId) ?? sourceTopicId;
    const actorId = pickActorId(
      sourceTopic.updated_by ?? sourceTopic.created_by,
    );
    if (life === "archive") {
      await requestRetryOnServerError(
        "POST",
        `/topics/${encodeURIComponent(newId)}/archive`,
        { actor_id: actorId },
      );
    } else {
      await requestRetryOnServerError(
        "POST",
        `/topics/${encodeURIComponent(newId)}/trash`,
        {
          actor_id: actorId,
          reason:
            sourceTopic.trash_reason ??
            "Dev seed: topic marked trashed for local trash coverage.",
        },
      );
    }
  }

  const sourceBoards = Array.isArray(seed.boards) ? seed.boards : [];
  for (const sourceBoard of sourceBoards) {
    const life = String(sourceBoard.dev_seed_board_lifecycle ?? "").trim();
    if (life !== "archive" && life !== "trash") {
      continue;
    }
    const sourceBoardId = String(sourceBoard.id ?? "").trim();
    const newId = boardIdMap.get(sourceBoardId) ?? sourceBoardId;
    const actorId = pickActorId(
      sourceBoard.updated_by ?? sourceBoard.created_by,
    );
    if (life === "archive") {
      await requestRetryOnServerError(
        "POST",
        `/boards/${encodeURIComponent(newId)}/archive`,
        { actor_id: actorId },
      );
    } else {
      await requestRetryOnServerError(
        "POST",
        `/boards/${encodeURIComponent(newId)}/trash`,
        {
          actor_id: actorId,
          reason:
            sourceBoard.trash_reason ??
            "Dev seed: board marked trashed for local trash coverage.",
        },
      );
    }
  }
}

async function seedTopics() {
  const sourceTopics = Array.isArray(seed.topics) ? seed.topics : [];

  for (const sourceTopic of sourceTopics) {
    const actorId = pickActorId(
      sourceTopic.updated_by ?? sourceTopic.created_by,
    );
    const requestedBackingThreadId = String(
      sourceTopic.thread_id ?? sourceTopic.id ?? "",
    ).trim();
    const topicPayload = {
      id: sourceTopic.id,
      ...(requestedBackingThreadId
        ? { thread_id: requestedBackingThreadId }
        : {}),
      type: sourceTopic.type ?? "other",
      title: sourceTopic.title,
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

    const response = await requestRetryOnServerError("POST", "/topics", {
      actor_id: actorId,
      topic: topicPayload,
    });

    const created = response?.topic;
    const newId = String(created?.id ?? "").trim();
    const createdBackingThreadId = String(
      created?.thread_id ?? created?.id ?? "",
    ).trim();
    if (!newId || !createdBackingThreadId) {
      throw new Error(
        `Topic create returned incomplete data for ${sourceTopic.title}`,
      );
    }

    const sourceTopicId = String(sourceTopic.id ?? "").trim();
    topicIdMap.set(sourceTopicId, newId);
    const topicAlias = topicRefAliasFromThreadLikeId(sourceTopicId);
    if (topicAlias && topicAlias !== sourceTopicId) {
      topicIdMap.set(topicAlias, newId);
    }
    threadIdMap.set(
      String(sourceTopic.id ?? "").trim(),
      createdBackingThreadId,
    );
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
    const artifactId = String(sourceArtifact.id ?? "").trim();
    if (artifactId) {
      seededArtifactIds.add(artifactId);
    }
  }
}

async function seedDocuments() {
  const sourceDocuments = Array.isArray(seed.documents) ? seed.documents : [];
  const revisionsByDocument =
    seed.documentRevisions && typeof seed.documentRevisions === "object"
      ? seed.documentRevisions
      : {};

  for (const sourceDocument of sourceDocuments) {
    if (
      sourceDocument?.document &&
      typeof sourceDocument.document === "object"
    ) {
      await seedInlineDocument(sourceDocument);
      continue;
    }

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
    const requestedBackingThreadId = String(
      sourceDocument.backing_thread_id ??
        sourceDocument.document_thread_id ??
        "",
    ).trim();
    const rawDocumentTopicRef = String(
      sourceDocument.topic_ref ?? sourceDocument.thread_id ?? "",
    ).trim();
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
          ...(requestedBackingThreadId
            ? { thread_id: requestedBackingThreadId }
            : {}),
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
        "POST",
        `/docs/${encodeURIComponent(newDocumentId)}/revisions`,
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
            sourceDocument.trash_reason ?? "Trashed while seeding mock data.",
        },
      );
    }
  }
}

/**
 * Keep the legacy revision-based seed shape, but also accept the simpler
 * inline doc shape used by Pi scenarios so `make serve` can switch scenarios
 * without maintaining a second seeding script.
 */
async function seedInlineDocument(sourceDocument) {
  const document = { ...(sourceDocument.document ?? {}) };
  const documentId = String(document.id ?? "").trim();
  if (!documentId) {
    console.warn("Skipping inline document with no id in mock seed data.");
    return;
  }

  const actorId = pickActorId(sourceDocument.actor_id ?? document.owner);

  try {
    const createResponse = await requestRetryOnServerError(
      "POST",
      "/docs",
      {
        actor_id: actorId,
        document: sanitizeInlineDocumentWrite(document),
        refs: mapRefs(sourceDocument.refs),
        content: sourceDocument.content,
        content_type: normalizeDocumentContentType(sourceDocument.content_type),
      },
      [201, 409],
    );
    const newDocumentId = String(createResponse?.document?.id ?? "").trim();
    if (newDocumentId) {
      documentIdMap.set(documentId, newDocumentId);
      return;
    }
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (isAlreadyExistsConflict(msg)) {
      documentIdMap.set(documentId, documentId);
      return;
    }
    throw err;
  }

  documentIdMap.set(documentId, documentId);
}

function sanitizeInlineDocumentWrite(document) {
  const next = { ...(document ?? {}) };
  delete next.status;
  delete next.state;
  return next;
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
    reason: sourceArtifact.trash_reason ?? "Trashed while seeding mock data.",
  });
}

async function seedArtifacts() {
  const packetKinds = new Set(["receipt", "review"]);
  const sourceArtifacts = Array.isArray(seed.artifacts) ? seed.artifacts : [];

  for (const sourceArtifact of sourceArtifacts) {
    const artifactId = String(sourceArtifact.id ?? "").trim();
    if (artifactId && seededArtifactIds.has(artifactId)) {
      continue;
    }
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
      if (artifactId) {
        seededArtifactIds.add(artifactId);
      }
      await trashSeedArtifactIfNeeded(sourceArtifact);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (isAlreadyExistsConflict(msg)) {
        if (artifactId) {
          seededArtifactIds.add(artifactId);
        }
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
      await seedResolutionEvidenceForCard(sourceCard);
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
        ? mapRef(
            rawTopicRef.includes(":") ? rawTopicRef : `topic:${rawTopicRef}`,
          )
        : "";
      const rawRelatedRefs = Array.isArray(sourceCard.related_refs)
        ? sourceCard.related_refs
            .map((entry) => String(entry ?? "").trim())
            .filter(Boolean)
        : [];
      const rawSelfBoardRef = `board:${String(sourceCard.board_id ?? newBoardId).trim()}`;

      let mappedRelatedRefs = Array.isArray(sourceCard.related_refs)
        ? mapRefs(sourceCard.related_refs)
        : [];
      const explicitBoardCard =
        Boolean(String(sourceCard.id ?? "").trim()) ||
        Boolean(cardSummaryForWrite);
      if (explicitBoardCard) {
        mappedRelatedRefs = mappedRelatedRefs.filter(
          (ref) => ref !== rawSelfBoardRef,
        );
      }
      const rawThreadRefs = rawRelatedRefs.filter((ref) =>
        ref.startsWith("thread:"),
      );
      if (!mappedTopicRef && explicitBoardCard && rawThreadRefs.length === 1) {
        const rawThreadID = rawThreadRefs[0].slice("thread:".length);
        const inferredTopicID = mapTopicId(rawThreadID);
        const hasTopicForThread =
          topicIdMap.has(rawThreadID) ||
          topicIdMap.has(topicRefAliasFromThreadLikeId(rawThreadID));
        if (inferredTopicID && hasTopicForThread) {
          mappedTopicRef = `topic:${inferredTopicID}`;
          mappedRelatedRefs = mappedRelatedRefs.filter(
            (ref) => !ref.startsWith("thread:"),
          );
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
        ...(pinnedDocumentId
          ? { document_ref: `document:${pinnedDocumentId}` }
          : {}),
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
              ...(sourceCard.status
                ? { status: String(sourceCard.status) }
                : {}),
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
      const createdCardThreadId = String(created?.thread_id ?? "").trim();
      const sourceCardId = String(sourceCard.id ?? "").trim();
      if (sourceCardId && createdCardId) {
        const publicValue = publicRefValue(created, "card", createdCardId);
        cardIdMap.set(sourceCardId, publicValue);
      }
      for (const sourceThreadAlias of [
        sourceCardId,
        sourceCard.message_thread_id,
        sourceCard.backing_thread_alias,
      ]) {
        const alias = String(sourceThreadAlias ?? "").trim();
        if (alias && createdCardThreadId) {
          threadIdMap.set(alias, createdCardThreadId);
        }
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

async function seedResolutionEvidenceForCard(sourceCard) {
  for (const item of resolutionEvidenceToCreateBeforeCard(seed, sourceCard)) {
    if (item.kind === "event") {
      await seedEventById(item.id);
      continue;
    }
    if (item.kind === "artifact") {
      await seedArtifactById(item.id);
    }
  }
}

async function seedEventById(eventId) {
  const id = String(eventId ?? "").trim();
  if (!id || postedEventIds.has(id)) {
    return;
  }
  const sourceEvent = (seed.events ?? []).find(
    (event) => String(event?.id ?? "").trim() === id,
  );
  if (!sourceEvent) {
    throw new Error(`resolution evidence event ${id} is not in the seed`);
  }
  if (!shouldSeedLegacyEvent(sourceEvent)) {
    throw new Error(
      `resolution evidence event ${id} is skipped by the seed runner`,
    );
  }
  await postSeedEvent(sourceEvent);
}

async function seedArtifactById(artifactId) {
  const id = String(artifactId ?? "").trim();
  if (!id || seededArtifactIds.has(id)) {
    return;
  }
  throw new Error(
    `resolution evidence artifact ${id} is not in the seed or was not created before cards`,
  );
}

async function postSeedEvent(sourceEvent) {
  const sourceId = String(sourceEvent.id ?? "").trim();
  if (sourceId && postedEventIds.has(sourceId)) {
    return;
  }
  const actorId = pickActorId(sourceEvent.actor_id);
  const mappedThreadId = mapThreadId(sourceEvent.thread_id);
  const payload = mapInboxSubjectInPayload(
    normalizeEventPayload(sourceEvent.type, sourceEvent.payload),
  );
  const refs = mapRefs(sourceEvent.refs);
  const eventPayload = {
    type: sourceEvent.type,
    thread_id: mappedThreadId,
    refs,
    summary: sourceEvent.summary,
    payload,
    provenance: sourceEvent.provenance,
    ...(sourceId ? { id: sourceId } : {}),
  };
  await requestRetryOnServerError("POST", "/events", {
    actor_id: actorId,
    event: eventPayload,
  });
  if (sourceId) {
    postedEventIds.add(sourceId);
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
    const sourceId = String(sourceEvent.id ?? "").trim();
    if (sourceId && postedEventIds.has(sourceId)) {
      posted += 1;
      continue;
    }
    try {
      await postSeedEvent(sourceEvent);
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

function mapInboxSubjectInPayload(payload) {
  const next = payload && typeof payload === "object" ? { ...payload } : {};
  if (next.subject_ref) {
    next.subject_ref = mapRef(next.subject_ref);
  }
  if (Array.isArray(next.related_refs)) {
    next.related_refs = mapRefs(next.related_refs);
  }
  const title = String(next.subject_title ?? next.title ?? "").trim();
  if (title) {
    next.subject_title = title;
  }
  return next;
}

function publicRefValue(created, prefix, fallbackId) {
  const ref = String(created?.ref ?? "").trim();
  if (ref.startsWith(`${prefix}:`)) {
    return ref.slice(prefix.length + 1);
  }
  const handle = String(created?.handle ?? "").trim();
  if (handle) {
    return handle;
  }
  return String(fallbackId ?? "").trim();
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
  return [
    ...new Set(
      (values ?? []).map((entry) => String(entry ?? "").trim()).filter(Boolean),
    ),
  ];
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

// CLI profiles store Go's ed25519.PrivateKey (seed||public, 64 bytes, base64).
function generateCliEd25519KeyPair() {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  const spki = publicKey.export({ type: "spki", format: "der" });
  const pkcs8 = privateKey.export({ type: "pkcs8", format: "der" });
  if (spki.length < 32) {
    throw new Error("ed25519 spki too short");
  }
  const pubRaw = spki.subarray(spki.length - 32);
  const seed = ed25519SeedFromPkcs8(pkcs8);
  const privRaw = Buffer.concat([seed, pubRaw]);
  if (privRaw.length !== 64) {
    throw new Error("ed25519 private key must be 64 bytes");
  }
  return {
    publicKeyBase64: pubRaw.toString("base64"),
    privateKeyBase64: privRaw.toString("base64"),
  };
}

function ed25519SeedFromPkcs8(der) {
  if (
    der.length >= 34 &&
    der[der.length - 34] === 0x04 &&
    der[der.length - 33] === 0x20
  ) {
    return der.subarray(der.length - 32);
  }
  throw new Error("unexpected ed25519 pkcs8 encoding");
}

function pkcs8FromCliPrivateKey(privateKeyBase64) {
  const raw = Buffer.from(privateKeyBase64, "base64");
  if (raw.length !== 64) {
    throw new Error("ed25519 private key must be 64 bytes");
  }
  return Buffer.concat([
    Buffer.from("302e020100300506032b657004220420", "hex"),
    raw.subarray(0, 32),
  ]);
}

async function requestAuthJson(
  method,
  requestPath,
  body,
  accessToken,
  okStatuses = [200, 201],
) {
  const headers = {
    accept: "application/json",
    "content-type": "application/json",
    authorization: `Bearer ${accessToken}`,
  };
  const response = await fetch(`${coreBaseUrl}${requestPath}`, {
    method,
    headers,
    body: JSON.stringify(body),
  });
  const rawText = await response.text();
  const parsed = parseJson(rawText);
  if (!okStatuses.includes(response.status)) {
    const message =
      parsed?.error?.message ?? rawText ?? `${method} ${requestPath} failed`;
    throw new Error(
      `${method} ${requestPath} -> ${response.status}: ${message}`,
    );
  }
  return parsed;
}

async function seedDevFixtureIdentities() {
  const bootstrapToken = String(process.env.ANX_BOOTSTRAP_TOKEN ?? "").trim();
  if (!bootstrapToken) {
    console.warn("ANX_DEV_SEED_IDENTITIES=1 requires ANX_BOOTSTRAP_TOKEN; skipping identities.");
    return;
  }
  const status = await request("GET", "/auth/bootstrap/status");
  if (status?.bootstrap_registration_available !== true) {
    console.log("Dev fixture identities skipped (bootstrap already consumed).");
    return;
  }
  const human = seedPersonas.find((p) => p.principal_kind === "human" && p.default === true);
  if (!human) throw new Error("dev seed requires a default human persona");
  const registered = await requestJson(coreBaseUrl, "POST", "/auth/passkey/dev/register", {
    display_name: human.display_label,
    bootstrap_token: bootstrapToken,
    existing_actor_id: human.actor_id,
  }, [201]);
  const adminToken = registered.tokens.access_token;
  const bundle = [{
    persona_id: human.persona_id,
    actor_id: human.actor_id,
    agent_id: registered.agent.agent_id,
    auth_username: registered.agent.username,
    display_label: human.display_label,
    principal_kind: "human",
    default: true,
    dev_bridge: false,
    access_token: adminToken,
    refresh_token: registered.tokens.refresh_token,
  }];
  const keyPair = generateCliEd25519KeyPair();
  const nonce = Buffer.from(generateKeyPairSync("ed25519").privateKey.export({ type: "pkcs8", format: "der" })).subarray(-16).toString("base64url");
  const expiresAt = new Date(Date.now() + 20 * 60_000).toISOString();
  const created = await requestAuthJson("POST", "/auth/hosts/enrollment-tokens", {
    label: "dev-host seed", expires_at: expiresAt,
  }, adminToken, [201]);
  const slug = "dev-host";
  const privateKey = createPrivateKey({
    key: pkcs8FromCliPrivateKey(keyPair.privateKeyBase64), format: "der", type: "pkcs8",
  });
  const enrollmentMessage = `anx-host-headless-enroll|${nonce}|${slug}|${keyPair.publicKeyBase64}`;
  const hostResponse = await requestJson(coreBaseUrl, "POST", "/auth/hosts/enrollments/headless", {
    public_key: keyPair.publicKeyBase64,
    requested_slug: slug,
    os_user: "dev-seed",
    hostname: slug,
    discovered_adapters: ["generic"],
    request_nonce: nonce,
    adoptions: [],
    enrollment_token: created.token,
    signature: sign(null, Buffer.from(enrollmentMessage), privateKey).toString("base64"),
  }, [201]);
  const host = hostResponse.host;
  for (const p of seedPersonas.filter((p) => p.principal_kind === "agent")) {
    const signedAt = new Date().toISOString();
    const name = p.persona_id.toLowerCase().replace(/[^a-z0-9-]/g, "-");
    const message = `anx-host-agent-token|${host.id}|${host.key_id}|${name}|${signedAt}`;
    const granted = await requestJson(coreBaseUrl, "POST", "/auth/token", {
      grant_type: "host_assertion",
      host_id: host.id,
      key_id: host.key_id,
      agent_name: name,
      signed_at: signedAt,
      signature: sign(null, Buffer.from(message), privateKey).toString("base64"),
      existing_actor_id: p.actor_id,
    });
    bundle.push({
      persona_id: p.persona_id,
      actor_id: p.actor_id,
      agent_id: granted.agent.id,
      auth_username: granted.agent.handle,
      display_label: p.display_label,
      principal_kind: "agent",
      default: false,
      dev_bridge: p.dev_bridge,
      access_token: granted.tokens.access_token,
      host_id: host.id,
      key_id: host.key_id,
      host_private_key: keyPair.privateKeyBase64,
    });
  }
  if (["default", "game-dev-studio"].includes(scenarioName)) {
    const signedAt = new Date().toISOString();
    const name = "release-bot";
    const message = `anx-host-agent-token|${host.id}|${host.key_id}|${name}|${signedAt}`;
    const granted = await requestJson(coreBaseUrl, "POST", "/auth/token", {
      grant_type: "host_assertion",
      host_id: host.id,
      key_id: host.key_id,
      agent_name: name,
      signed_at: signedAt,
      signature: sign(null, Buffer.from(message), privateKey).toString("base64"),
    });
    bundle.push({
      persona_id: name,
      actor_id: granted.agent.actor_id,
      agent_id: granted.agent.id,
      auth_username: granted.agent.handle,
      display_label: "Release Bot",
      principal_kind: "agent",
      default: false,
      dev_bridge: false,
      access_token: granted.tokens.access_token,
      host_id: host.id,
      key_id: host.key_id,
      host_private_key: keyPair.privateKeyBase64,
    });
  }
  const outDir = path.join(path.dirname(fileURLToPath(import.meta.url)), "..", ".dev");
  await mkdir(outDir, { recursive: true });
  await writeFile(path.join(outDir, "local-identities.json"), `${JSON.stringify({ generated_at: new Date().toISOString(), host, personas: bundle }, null, 2)}\n`, "utf8");
  console.log(`Wrote dev host and ${bundle.length} persona identities`);
  return bundle;
}

async function seedCommandCenter(bundle) {
  if (!bundle.length || !["default", "game-dev-studio"].includes(scenarioName)) {
    return;
  }
  const byActor = new Map(bundle.map((p) => [p.actor_id, p]));
  const operator = byActor.get("actor-gds-producer");
  if (!operator?.access_token) return;
  const stalePersona = bundle.find((p) => p.persona_id === "release-bot");
  if (!stalePersona?.actor_id) throw new Error("command-center seed requires release-bot");
  const inbox = await requestAuthGet("/inbox", operator.access_token);
  for (const sourceEventID of ["evt-gds-human-attn-combat", "evt-gds-human-attn-vertical"]) {
    const item = (inbox.items ?? []).find((candidate) => candidate.source_event_id === sourceEventID);
    if (!item?.id) throw new Error(`command-center seed requires inbox item for ${sourceEventID}`);
    await requestAuthJson("POST", `/inbox/${encodeURIComponent(item.id)}/respond`, {
      actor_id: operator.actor_id,
      response_text: "Decision recorded for the development scenario.",
      notify_mode: "none",
      related_refs: [],
    }, operator.access_token, [201]);
  }
  const roster = await requestAuthGet("/agents", operator.access_token);
  const rosterByActor = new Map((roster.agents ?? []).map((a) => [a.actor_id, a]));
  const roles = [
    ["working", "actor-gds-gameplay"],
    ["waiting_on_human", "actor-gds-art"],
    ["idle", "actor-gds-narrative"],
    ["stale", stalePersona.actor_id],
  ];
  if (roles.some(([, actorId]) => rosterByActor.get(actorId)?.identity_kind === "standalone")) {
    console.warn("Command-center roster seed awaits host adoption of game-studio fixture agents.");
    return;
  }
  if (roles.some(([, actorId]) => !rosterByActor.get(actorId) || !byActor.get(actorId)?.access_token)) {
    console.warn("Command-center roster seed awaits derived fixture identity tokens.");
    return;
  }
  const cards = await requestAuthGet("/cards", operator.access_token);
  const card = (cards.cards ?? []).find((c) =>
    String(c.title ?? "") === "Run QA bug bash and triage release blockers",
  );
  if (!card?.ref || !card?.thread_id) {
    throw new Error("command-center seed requires the bug-bash card ref and thread");
  }
  const now = new Date();
  const observedAt = now.toISOString();
  const runBase = (actorId, externalId, state) => ({
    launcher: "agentctl",
    external_id: externalId,
    host_id: rosterByActor.get(actorId).host_id,
    agent_id: rosterByActor.get(actorId).id,
    adapter: actorId === "actor-gds-gameplay" ? "codex" : "generic",
    state,
    liveness: state === "running" ? "alive" : "stale",
    result_collected: state === "completed",
    labels: [`anx.card.${String(card.ref).slice(5)}`],
    card_ref: card.ref,
    started_at: new Date(now.getTime() - 12 * 60_000).toISOString(),
    ...(state === "completed" ? { ended_at: observedAt } : {}),
    last_observed_at: observedAt,
  });
  await requestAuthJson("POST", "/runs", runBase("actor-gds-gameplay", "exec-dev-working", "running"), byActor.get("actor-gds-gameplay").access_token);
  await requestAuthJson("PATCH", "/agents/me/presence", {
    current_card_ref: card.ref,
    note: "Implementing the combat pass; capture build is next.",
  }, byActor.get("actor-gds-gameplay").access_token);
  await requestAuthJson("POST", "/runs", runBase("actor-gds-narrative", "exec-dev-idle", "completed"), byActor.get("actor-gds-narrative").access_token);
  const waiting = rosterByActor.get("actor-gds-art");
  if ((waiting.open_asks_count ?? 0) === 0) {
    await requestAuthJson("POST", "/events", {
      request_key: "dev-roster-art-review",
      event: {
        type: "human_attention_requested",
        thread_id: card.thread_id,
        refs: [`thread:${card.thread_id}`, card.ref],
        summary: "Review the capture UI contrast before sign-off",
        payload: {
          kind: "review",
          title: "Review capture UI contrast",
          requester_actor_id: "actor-gds-art",
          requester_agent_id: waiting.id,
          subject_ref: card.ref,
          response_proposals: ["Approve the contrast pass", "Request revisions"],
        },
        provenance: { sources: ["seed:game-dev-studio"] },
      },
    }, byActor.get("actor-gds-art").access_token);
  }
  const after = await requestAuthGet("/agents", operator.access_token);
  const actual = new Map((after.agents ?? []).map((a) => [a.actor_id, a.state]));
  for (const [expected, actorId] of roles) {
    if (actual.get(actorId) !== expected) {
      throw new Error(`command-center roster seed: ${actorId} is ${actual.get(actorId)}, expected ${expected}`);
    }
  }
  console.log("Seeded command-center roster states: working, waiting, idle, stale.");
}

async function requestAuthGet(path, accessToken) {
  const response = await fetch(`${coreBaseUrl}${path}`, {
    headers: { accept: "application/json", authorization: `Bearer ${accessToken}` },
  });
  const parsed = parseJson(await response.text());
  if (!response.ok) throw new Error(`GET ${path} -> ${response.status}: ${parsed?.error?.message ?? "failed"}`);
  return parsed;
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
