#!/usr/bin/env node

/**
 * Capture one or more web-ui routes from a running server.
 *
 * This is intentionally destination-agnostic: pass --out to choose where PNGs
 * land, then copy or move them wherever the calling workflow needs.
 */

import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(__dirname, "..");

function printUsage() {
  console.log(
    `
Usage:
  node scripts/capture-screenshot.mjs --capture name=/route [options]

Options:
  --base-url <url>       Running web-ui origin (default: http://127.0.0.1:5173)
  --capture <name=path>  Screenshot route or URL. Repeat for multiple captures.
  --out <dir>            Output directory (default: web-ui/.screenshots)
  --viewport <WxH>       Viewport size (default: 945x2048)
  --wait <ms>            Extra settle time after DOM load (default: 800)
  --full-page            Capture full scrollable page (default)
  --no-full-page         Capture viewport only
  --fixture <name>       Optional route mocks. Available: topic-messages
  --help                 Show this help

Examples:
  node scripts/capture-screenshot.mjs --capture signin=/hosted/signin
  node scripts/capture-screenshot.mjs --fixture topic-messages \\
    --capture signin=/hosted/signin \\
    --capture messages='/o/local/w/local/topics/0ae18e22-f?qa=1'
`.trim(),
  );
}

function parseArgs(argv) {
  const opts = {
    baseUrl: "http://127.0.0.1:5173",
    captures: [],
    outDir: path.join(projectRoot, ".screenshots"),
    viewport: "945x2048",
    waitMs: 800,
    fullPage: true,
    fixture: "",
  };

  const args = argv.slice(2);
  for (let i = 0; i < args.length; i += 1) {
    const arg = args[i];
    if (arg === "--") {
      continue;
    }
    switch (arg) {
      case "--base-url":
        opts.baseUrl = args[++i];
        break;
      case "--capture": {
        const value = String(args[++i] ?? "");
        const eq = value.indexOf("=");
        if (eq <= 0) {
          throw new Error("--capture must use name=/path");
        }
        opts.captures.push({
          name: value.slice(0, eq).trim(),
          target: value.slice(eq + 1).trim(),
        });
        break;
      }
      case "--out":
        opts.outDir = path.resolve(args[++i]);
        break;
      case "--viewport":
        opts.viewport = args[++i];
        break;
      case "--wait":
        opts.waitMs = Number(args[++i]) || 0;
        break;
      case "--full-page":
        opts.fullPage = true;
        break;
      case "--no-full-page":
        opts.fullPage = false;
        break;
      case "--fixture":
        opts.fixture = String(args[++i] ?? "").trim();
        break;
      case "--help":
      case "-h":
        printUsage();
        process.exit(0);
        break;
      default:
        throw new Error(`Unknown option: ${arg}`);
    }
  }

  if (opts.captures.length === 0) {
    throw new Error("At least one --capture name=/path is required.");
  }

  const [width, height] = opts.viewport.split("x").map(Number);
  opts.viewportWidth = width || 945;
  opts.viewportHeight = height || 2048;
  opts.baseUrl = String(opts.baseUrl).replace(/\/+$/, "");

  return opts;
}

function safeFilename(name) {
  return (
    String(name ?? "")
      .trim()
      .replace(/[^a-zA-Z0-9._-]+/g, "-")
      .replace(/^-+|-+$/g, "") || "screenshot"
  );
}

function resolveTarget(baseUrl, target) {
  const raw = String(target ?? "").trim();
  if (/^https?:\/\//i.test(raw)) {
    return raw;
  }
  return `${baseUrl}${raw.startsWith("/") ? raw : `/${raw}`}`;
}

async function installTopicMessagesFixture(page) {
  const actorId = "cursor";
  const topicId = "0ae18e22-f";
  const timeline = [
    {
      id: "evt-topic-message-1",
      ts: "2026-05-03T13:14:00.000Z",
      type: "message_posted",
      actor_id: actorId,
      thread_id: topicId,
      refs: [`thread:${topicId}`, `topic:${topicId}`],
      summary:
        "Dogfooding canonical topic message: this uses topics message rather than the removed discuss alias.",
      payload: {
        text: "Dogfooding canonical topic message: this uses topics message rather than the removed discuss alias.",
      },
      provenance: { sources: ["event:evt-topic-message-1"] },
    },
    {
      id: "evt-topic-reply-1",
      ts: "2026-05-03T13:15:00.000Z",
      type: "message_posted",
      actor_id: actorId,
      thread_id: topicId,
      parent_event_id: "evt-topic-message-1",
      refs: [`thread:${topicId}`, `topic:${topicId}`],
      summary:
        "Dogfooding canonical topic reply: short displayed event ids resolve on the topic backing thread.",
      payload: {
        text: "Dogfooding canonical topic reply: short displayed event ids resolve on the topic backing thread.",
      },
      provenance: { sources: ["event:evt-topic-reply-1"] },
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.route(/\/meta\/handshake$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ schema_version: "test", dev_actor_mode: true }),
    }),
  );
  await page.route(/\/auth\/session$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ agent: null }),
    }),
  );
  await page.route(/\/auth\/bootstrap\/status$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ available: false }),
    }),
  );
  await page.route(/\/auth\/principals(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ principals: [] }),
    }),
  );
  await page.route(/\/auth\/dev\/default-persona$/, (route) =>
    route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({ error: { message: "none" } }),
    }),
  );
  await page.route(/\/actors$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "cursor" }],
      }),
    }),
  );
  await page.route(/\/topics\/0ae18e22-f\/workspace(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        topic_id: topicId,
        topic: {
          id: topicId,
          topic_ref: `topic:${topicId}`,
          thread_id: topicId,
          title: "CLI message affordances",
          state: "active",
          summary: "Dogfood canonical topic messages.",
          current_summary: "Dogfood canonical topic messages.",
          owner_refs: [],
          document_refs: [],
          board_refs: [],
          related_refs: [],
          updated_at: "2026-05-03T13:15:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["event:evt-topic-message-1"] },
        },
        context: {
          recent_events: timeline,
          key_artifacts: [],
          open_cards: [],
          documents: [],
        },
        boards: [],
        board_memberships: [],
        documents: [],
      }),
    }),
  );
  await page.route(/\/topics\/0ae18e22-f\/timeline$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: timeline }),
    }),
  );
  await page.route(/\/events\/stream(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: ": keepalive\n\n",
    }),
  );
  await page.route(/\/artifacts(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ artifacts: [] }),
    }),
  );
  await page.route(/\/docs\?thread_id=0ae18e22-f$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ documents: [] }),
    }),
  );
}

async function installFixture(page, fixture) {
  if (!fixture) return;
  if (fixture === "topic-messages") {
    await installTopicMessagesFixture(page);
    return;
  }
  throw new Error(`Unknown fixture: ${fixture}`);
}

async function main() {
  const opts = parseArgs(process.argv);
  await mkdir(opts.outDir, { recursive: true });

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: opts.viewportWidth, height: opts.viewportHeight },
    deviceScaleFactor: 1,
    isMobile: opts.viewportWidth < 1024,
    hasTouch: opts.viewportWidth < 1024,
  });

  try {
    const page = await context.newPage();
    await installFixture(page, opts.fixture);

    for (const capture of opts.captures) {
      const url = resolveTarget(opts.baseUrl, capture.target);
      await page.goto(url, { waitUntil: "domcontentloaded" });
      if (opts.waitMs > 0) {
        await page.waitForTimeout(opts.waitMs);
      }
      const file = path.join(opts.outDir, `${safeFilename(capture.name)}.png`);
      await page.screenshot({ path: file, fullPage: opts.fullPage });
      console.log(file);
    }
  } finally {
    await browser.close();
  }
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
});
