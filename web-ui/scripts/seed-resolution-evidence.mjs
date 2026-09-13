/**
 * Seed ordering helpers for terminal card evidence.
 *
 * Core requires every done-card `resolution_refs` event:/artifact: to exist
 * before the card is created. Topic and document backing threads exist before
 * cards; card-owned threads do not.
 */

export function typedRefParts(ref) {
  const text = String(ref ?? "").trim();
  const separator = text.indexOf(":");
  if (separator <= 0 || separator === text.length - 1) {
    return { prefix: "", id: "" };
  }
  return {
    prefix: text.slice(0, separator),
    id: text.slice(separator + 1),
  };
}

export function cardResolutionRefs(card) {
  if (!Array.isArray(card?.resolution_refs)) {
    return [];
  }
  return card.resolution_refs
    .map((entry) => String(entry ?? "").trim())
    .filter(Boolean);
}

export function backingThreadsAvailableBeforeCards(seed) {
  const ids = new Set();
  const add = (value) => {
    const id = String(value ?? "").trim();
    if (id) {
      ids.add(id);
    }
  };

  for (const topic of seed?.topics ?? []) {
    add(topic?.thread_id);
    add(topic?.id);
    add(topic?.backing_thread_id);
  }

  for (const document of seed?.documents ?? []) {
    const inner =
      document?.document && typeof document.document === "object"
        ? document.document
        : document;
    add(document?.backing_thread_id);
    add(document?.document_thread_id);
    add(document?.thread_id);
    add(inner?.thread_id);
    add(inner?.backing_thread_id);
  }

  return ids;
}

export function findSeedEvent(seed, eventId) {
  const id = String(eventId ?? "").trim();
  if (!id) {
    return null;
  }
  return (
    (seed?.events ?? []).find(
      (event) => String(event?.id ?? "").trim() === id,
    ) ?? null
  );
}

export function findSeedArtifact(seed, artifactId) {
  const id = String(artifactId ?? "").trim();
  if (!id) {
    return null;
  }
  const fromArtifacts = (seed?.artifacts ?? []).find(
    (artifact) => String(artifact?.id ?? "").trim() === id,
  );
  if (fromArtifacts) {
    return fromArtifacts;
  }
  const packets = Array.isArray(seed?.packets) ? seed.packets : [];
  return (
    packets.find((packet) => {
      const artifact = packet?.artifact ?? packet;
      return String(artifact?.id ?? "").trim() === id;
    }) ?? null
  );
}

/**
 * Events and artifacts that must exist before `card` is POSTed.
 * Order is the card's resolution_refs order.
 *
 * @returns {{kind: "event"|"artifact", id: string, record: object}[]}
 */
export function resolutionEvidenceToCreateBeforeCard(seed, card) {
  const items = [];
  const seen = new Set();
  for (const ref of cardResolutionRefs(card)) {
    const { prefix, id } = typedRefParts(ref);
    if (!id || (prefix !== "event" && prefix !== "artifact")) {
      continue;
    }
    const key = `${prefix}:${id}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    const record =
      prefix === "event" ? findSeedEvent(seed, id) : findSeedArtifact(seed, id);
    items.push({ kind: prefix, id, record });
  }
  return items;
}

export function listResolutionEvidenceViolations(seed) {
  if (!seed || typeof seed !== "object") {
    return ["listResolutionEvidenceViolations: seed is missing"];
  }

  const beforeThreads = backingThreadsAvailableBeforeCards(seed);
  const violations = [];

  for (const card of seed.cards ?? []) {
    const cardLabel =
      String(card?.id ?? "").trim() ||
      String(card?.thread_id ?? "").trim() ||
      "(anonymous card)";
    for (const item of resolutionEvidenceToCreateBeforeCard(seed, card)) {
      if (!item.record) {
        violations.push(
          `card:${cardLabel}: missing ${item.kind} ${item.id} for resolution_refs`,
        );
        continue;
      }
      if (item.kind !== "event") {
        continue;
      }
      const threadId = String(item.record.thread_id ?? "").trim();
      if (threadId && !beforeThreads.has(threadId)) {
        violations.push(
          `card:${cardLabel}: resolution event ${item.id} is on thread ${threadId}, which is not a topic/document thread available before cards`,
        );
      }
    }
  }

  return violations;
}
