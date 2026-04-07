#!/usr/bin/env node

import {
  failWithPrefix,
  normalizeBaseUrl,
  requestJson,
  waitForCore,
} from "../../../../scripts/seed-core-lib.mjs";
import { getPilotRescueSeedData } from "./pilot-rescue-data.mjs";

const coreBaseUrl = normalizeBaseUrl(
  process.env.OAR_CORE_BASE_URL ?? "http://127.0.0.1:8000",
);
const forceSeed = process.env.OAR_FORCE_SEED === "1";
const skipIfPresent = process.env.OAR_SEED_SKIP_IF_PRESENT !== "0";
const waitTimeoutMs = Number(process.env.OAR_CORE_WAIT_TIMEOUT_MS ?? 20000);

if (!coreBaseUrl) {
  failWithPrefix(
    "cli pi seed failed",
    "OAR_CORE_BASE_URL must be set or defaultable.",
  );
}

const seed = getPilotRescueSeedData();
const defaultActorId = seed.actors[0]?.id ?? "actor-product-lead";
const topicIdMap = new Map();
const threadIdMap = new Map();

main().catch((error) => {
  const reason = error instanceof Error ? error.message : String(error);
  failWithPrefix("cli pi seed failed", reason);
});

async function main() {
  await waitForCore(coreBaseUrl, waitTimeoutMs);

  if (skipIfPresent && !forceSeed) {
    const alreadySeeded = await detectSeededState();
    if (alreadySeeded) {
      console.log("Seed data already present; skipping.");
      return;
    }
  }

  await seedActors();
  await seedTopics();
  await seedArtifacts();
  await seedDocuments();
  const eventStats = await seedEvents();
  await rebuildDerived();

  console.log(
    `Seed complete. Events posted=${eventStats.posted}, events skipped=${eventStats.skipped}.`,
  );
}

async function detectSeededState() {
  const [actorsBody, topicsBody] = await Promise.all([
    request("GET", "/actors"),
    request("GET", "/topics"),
  ]);

  const actorIds = new Set((actorsBody?.actors ?? []).map((actor) => actor?.id));
  const topicTitles = new Set(
    (topicsBody?.topics ?? []).map((topic) => String(topic?.title ?? "")),
  );

  return (
    actorIds.has("actor-product-lead") &&
    topicTitles.has("Pilot Rescue Sprint: NorthWave Launch Readiness")
  );
}

async function seedActors() {
  for (const actor of seed.actors) {
    await request("POST", "/actors", { actor }, [201, 409]);
  }
}

function normalizeTopicTypeFromPilotThread(type) {
  const t = String(type ?? "").trim();
  switch (t) {
    case "initiative":
      return "initiative";
    case "case":
      return "incident";
    case "process":
      return "other";
    default:
      return "other";
  }
}

async function seedTopics() {
  const sourceThreads = Array.isArray(seed.threads) ? seed.threads : [];

  for (const sourceThread of sourceThreads) {
    const actorId = pickActorId(sourceThread.updated_by);
    const topicPayload = {
      id: sourceThread.id,
      type: normalizeTopicTypeFromPilotThread(sourceThread.type),
      title: sourceThread.title,
      status: sourceThread.status,
      summary: String(
        sourceThread.current_summary ?? sourceThread.title ?? "",
      ).trim(),
      owner_refs: sourceThread.updated_by
        ? [`actor:${pickActorId(sourceThread.updated_by)}`]
        : [`actor:${defaultActorId}`],
      board_refs: [],
      document_refs: [],
      related_refs: normalizeArtifactRefs(sourceThread.key_artifacts ?? []),
      provenance: sourceThread.provenance,
    };

    const response = await request("POST", "/topics", {
      actor_id: actorId,
      topic: topicPayload,
    });

    const created = response?.topic;
    const newTopicId = String(created?.id ?? "").trim();
    const backingThreadId = String(created?.thread_id ?? "").trim();
    if (!newTopicId || !backingThreadId) {
      throw new Error(
        `Topic create returned incomplete data for ${sourceThread.title}`,
      );
    }

    const sourceTopicId = String(sourceThread.id ?? "").trim();
    topicIdMap.set(sourceTopicId, newTopicId);
    const topicAlias = topicRefAliasFromThreadLikeId(sourceTopicId);
    if (topicAlias && topicAlias !== sourceTopicId) {
      topicIdMap.set(topicAlias, newTopicId);
    }
    threadIdMap.set(sourceTopicId, backingThreadId);
  }
}

async function seedArtifacts() {
  for (const sourceArtifact of seed.artifacts) {
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

    await request("POST", "/artifacts", {
      actor_id: actorId,
      artifact: {
        id: sourceArtifact.id,
        kind: sourceArtifact.kind,
        thread_id: mapThreadId(sourceArtifact.thread_id),
        summary: sourceArtifact.summary,
        refs: mapRefs(sourceArtifact.refs),
        provenance: sourceArtifact.provenance,
      },
      content_type: contentType,
      content,
    });
  }
}

async function seedDocuments() {
  for (const sourceDocument of seed.documents ?? []) {
    await request("POST", "/docs", {
      actor_id: pickActorId(sourceDocument.actor_id),
      document: sanitizeDocumentWrite(sourceDocument.document),
      refs: mapRefs(sourceDocument.refs),
      content: sourceDocument.content,
      content_type: sourceDocument.content_type,
    }, [201, 409]);
  }
}

function sanitizeDocumentWrite(document) {
  const next = { ...(document ?? {}) };
  delete next.status;
  delete next.state;
  return next;
}

async function seedEvents() {
  let posted = 0;
  let skipped = 0;
  const sortedEvents = [...seed.events].sort((a, b) => String(a?.ts ?? "").localeCompare(String(b?.ts ?? "")));

  for (const sourceEvent of sortedEvents) {
    const actorId = pickActorId(sourceEvent.actor_id);
    const mappedThreadId = mapThreadId(sourceEvent.thread_id);
    const refs = normalizeEventRefs(sourceEvent.type, mapRefs(sourceEvent.refs), mappedThreadId);

    try {
      await request("POST", "/events", {
        actor_id: actorId,
        event: {
          type: sourceEvent.type,
          thread_id: mappedThreadId,
          refs,
          summary: sourceEvent.summary,
          payload: normalizeEventPayload(sourceEvent.type, sourceEvent.payload),
          provenance: sourceEvent.provenance,
        },
      });
      posted += 1;
    } catch (error) {
      skipped += 1;
      const reason = error instanceof Error ? error.message : String(error);
      console.warn(`Skipping event ${sourceEvent.id} (${sourceEvent.type}): ${reason}`);
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
    return `${prefix}:${mapThreadId(value)}`;
  }
  if (prefix === "topic") {
    return `${prefix}:${topicIdMap.get(value) ?? value}`;
  }
  return text;
}

function topicRefAliasFromThreadLikeId(topicId) {
  const raw = String(topicId ?? "").trim();
  if (!raw) {
    return "";
  }
  return raw.startsWith("thread-") ? raw.slice("thread-".length) : raw;
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

function normalizeEventPayload(type, payload) {
  const next = payload && typeof payload === "object" ? { ...payload } : {};
  if (type === "exception_raised" && !String(next.subtype ?? "").trim()) {
    next.subtype = "pilot_risk";
  }
  return next;
}

function normalizeEventRefs(type, refs, mappedThreadId) {
  const nextRefs = Array.isArray(refs) ? [...refs] : [];
  if (type === "thread_updated" || type === "thread_created") {
    const hasThreadRef = nextRefs.some((ref) => ref.startsWith("thread:"));
    if (!hasThreadRef && mappedThreadId) {
      nextRefs.push(`thread:${mappedThreadId}`);
    }
  }
  return nextRefs;
}

async function request(method, requestPath, body, okStatuses = [200, 201]) {
  return requestJson(coreBaseUrl, method, requestPath, body, okStatuses);
}
