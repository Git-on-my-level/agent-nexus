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
const threadIdMap = new Map();
const snapshotIdMap = new Map();

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
  await seedThreads();
  await seedCommitments();
  await seedArtifacts();
  await seedDocuments();
  const eventStats = await seedEvents();
  await rebuildDerived();

  console.log(
    `Seed complete. Events posted=${eventStats.posted}, events skipped=${eventStats.skipped}.`,
  );
}

async function detectSeededState() {
  const [actorsBody, threadsBody] = await Promise.all([
    request("GET", "/actors"),
    request("GET", "/threads"),
  ]);

  const actorIds = new Set((actorsBody?.actors ?? []).map((actor) => actor?.id));
  const threadTitles = new Set(
    (threadsBody?.threads ?? []).map((thread) => String(thread?.title ?? "")),
  );

  return (
    actorIds.has("actor-product-lead") &&
    threadTitles.has("Pilot Rescue Sprint: NorthWave Launch Readiness")
  );
}

async function seedActors() {
  for (const actor of seed.actors) {
    await request("POST", "/actors", { actor }, [201, 409]);
  }
}

async function seedThreads() {
  for (const sourceThread of seed.threads) {
    const actorId = pickActorId(sourceThread.updated_by);
    const response = await request("POST", "/threads", {
      actor_id: actorId,
      thread: {
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
      },
    });

    const newId = String(response?.thread?.id ?? "").trim();
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
    const response = await request("POST", "/commitments", {
      actor_id: actorId,
      commitment: {
        thread_id: mustMapThreadId(sourceCommitment.thread_id),
        title: sourceCommitment.title,
        owner: sourceCommitment.owner,
        due_at: sourceCommitment.due_at,
        status: sourceCommitment.status,
        definition_of_done: sourceCommitment.definition_of_done,
        links: mapRefs(sourceCommitment.links),
        provenance: sourceCommitment.provenance,
      },
    });

    const newId = String(response?.commitment?.id ?? "").trim();
    if (!newId) {
      throw new Error(`Commitment create returned no id for ${sourceCommitment.title}`);
    }
    snapshotIdMap.set(sourceCommitment.id, newId);
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
      document: sourceDocument.document,
      refs: mapRefs(sourceDocument.refs),
      content: sourceDocument.content,
      content_type: sourceDocument.content_type,
    }, [201, 409]);
  }
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
  if (prefix === "snapshot") {
    return `${prefix}:${snapshotIdMap.get(value) ?? value}`;
  }
  if (prefix === "commitment") {
    return `${prefix}:${snapshotIdMap.get(value) ?? value}`;
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

function normalizeEventPayload(type, payload) {
  const next = payload && typeof payload === "object" ? { ...payload } : {};
  if (type === "exception_raised" && !String(next.subtype ?? "").trim()) {
    next.subtype = "pilot_risk";
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

async function request(method, requestPath, body, okStatuses = [200, 201]) {
  return requestJson(coreBaseUrl, method, requestPath, body, okStatuses);
}
