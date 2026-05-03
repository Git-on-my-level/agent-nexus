#!/usr/bin/env node

import {
  failWithPrefix,
  normalizeBaseUrl,
  requestJson,
  waitForCore,
} from "./seed-core-lib.mjs";

const prefix = "scenario validation failed";
const baseUrl = normalizeBaseUrl(
  process.env.ANX_CORE_BASE_URL ?? "http://127.0.0.1:8000",
);
const scenario = String(process.env.ANX_DEV_SEED_SCENARIO ?? "").trim();
const minimums = {
  topics: Number(process.env.ANX_SCENARIO_MIN_TOPICS ?? 4),
  boards: Number(process.env.ANX_SCENARIO_MIN_BOARDS ?? 3),
  docs: Number(process.env.ANX_SCENARIO_MIN_DOCS ?? 5),
  cards: Number(process.env.ANX_SCENARIO_MIN_CARDS ?? 10),
  messages: Number(process.env.ANX_SCENARIO_MIN_MESSAGES ?? 100),
};
const requiredSurfaces = new Set(
  String(process.env.ANX_SCENARIO_REQUIRED_MESSAGE_SURFACES ?? "topic,document,card")
    .split(",")
    .map((entry) => entry.trim())
    .filter(Boolean),
);

if (!baseUrl) {
  failWithPrefix(prefix, "ANX_CORE_BASE_URL must be set or defaultable.");
}

main().catch((error) => {
  failWithPrefix(prefix, error instanceof Error ? error.message : String(error));
});

async function main() {
  await waitForCore(baseUrl, Number(process.env.ANX_CORE_WAIT_TIMEOUT_MS ?? 20000), {
    probes: ["/version", "/readyz"],
  });

  const [topics, boards, docs, cards, events] = await Promise.all([
    listAll("/topics", "topics"),
    listAll("/boards", "boards"),
    listAll("/docs", "documents"),
    listAll("/cards", "cards"),
    listAll("/events?type=message_posted", "events"),
  ]);

  const messageCounts = countMessageSurfaces(events);
  const failures = [];
  assertAtLeast(failures, "topics", topics.length, minimums.topics);
  assertAtLeast(failures, "boards", boards.length, minimums.boards);
  assertAtLeast(failures, "docs", docs.length, minimums.docs);
  assertAtLeast(failures, "cards", cards.length, minimums.cards);
  assertAtLeast(
    failures,
    "message_posted events",
    messageCounts.total,
    minimums.messages,
  );

  for (const surface of requiredSurfaces) {
    assertAtLeast(
      failures,
      `${surface} messages`,
      messageCounts.bySurface[surface] ?? 0,
      1,
    );
    assertAtLeast(
      failures,
      `${surface} replies`,
      messageCounts.repliesBySurface[surface] ?? 0,
      1,
    );
  }

  if (failures.length > 0) {
    throw new Error(failures.join("\n"));
  }

  console.log(
    [
      `Scenario validation passed${scenario ? ` for ${scenario}` : ""}.`,
      `topics=${topics.length}`,
      `boards=${boards.length}`,
      `docs=${docs.length}`,
      `cards=${cards.length}`,
      `messages=${messageCounts.total}`,
      `topic_messages=${messageCounts.bySurface.topic ?? 0}`,
      `doc_messages=${messageCounts.bySurface.document ?? 0}`,
      `card_messages=${messageCounts.bySurface.card ?? 0}`,
      `topic_replies=${messageCounts.repliesBySurface.topic ?? 0}`,
      `doc_replies=${messageCounts.repliesBySurface.document ?? 0}`,
      `card_replies=${messageCounts.repliesBySurface.card ?? 0}`,
    ].join(" "),
  );
}

async function listAll(pathWithQuery, fieldName) {
  const items = [];
  let cursor = "";
  for (;;) {
    const separator = pathWithQuery.includes("?") ? "&" : "?";
    const pagePath = `${pathWithQuery}${separator}limit=200${
      cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""
    }`;
    const body = await requestJson(baseUrl, "GET", pagePath);
    const pageItems = Array.isArray(body?.[fieldName]) ? body[fieldName] : [];
    items.push(...pageItems);

    const pageInfo = body?.page_info ?? body?.pageInfo ?? {};
    cursor = String(
      pageInfo?.next_cursor ?? pageInfo?.nextCursor ?? body?.next_cursor ?? "",
    ).trim();
    if (!cursor || pageItems.length === 0) {
      return items;
    }
  }
}

function countMessageSurfaces(events) {
  const bySurface = {};
  const repliesBySurface = {};

  for (const event of events) {
    const payload = event?.payload && typeof event.payload === "object"
      ? event.payload
      : {};
    const surface = normalizeSurface(
      payload.subject_kind ?? payload.kind ?? event?.summary,
    );
    if (!surface) {
      continue;
    }
    bySurface[surface] = (bySurface[surface] ?? 0) + 1;
    if (String(payload.reply_to_event_id ?? "").trim()) {
      repliesBySurface[surface] = (repliesBySurface[surface] ?? 0) + 1;
    }
  }

  return {
    total: Object.values(bySurface).reduce((sum, count) => sum + count, 0),
    bySurface,
    repliesBySurface,
  };
}

function normalizeSurface(value) {
  const raw = String(value ?? "").trim();
  if (raw === "topic" || raw === "topic_message") {
    return "topic";
  }
  if (
    raw === "document" ||
    raw === "document_message" ||
    raw === "document_text_comment"
  ) {
    return "document";
  }
  if (raw === "card" || raw === "card_message") {
    return "card";
  }
  return "";
}

function assertAtLeast(failures, name, actual, expected) {
  if (actual < expected) {
    failures.push(`${name}: got ${actual}, want at least ${expected}`);
  }
}
