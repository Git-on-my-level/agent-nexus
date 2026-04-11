import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import {
  analyzePiEventLog,
  cardPatchTemplate,
  cardTemplate,
  loadScenarioContent,
  parseArgs,
  roleCardState,
  validateAgentOutputs,
} from "./run.mjs";

test("analyzePiEventLog captures nested runtime errors and ignores clean records", () => {
  const content = [
    JSON.stringify({ type: "session", id: "s1" }),
    JSON.stringify({
      type: "turn_end",
      message: {
        role: "assistant",
        stopReason: "error",
        errorMessage: "quota exceeded",
      },
    }),
    JSON.stringify({
      type: "agent_end",
      messages: [
        {
          role: "assistant",
          stopReason: "error",
          errorMessage: "provider exploded",
        },
      ],
    }),
  ].join("\n");

  const diagnostics = analyzePiEventLog(content);
  assert.deepEqual(diagnostics.parseErrors, []);
  assert.deepEqual(diagnostics.runtimeErrors, ["quota exceeded", "provider exploded"]);
});

test("validateAgentOutputs fails when result.md is missing even if event log is clean", () => {
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-dogfood-test-"));
  const eventsPath = path.join(tmpDir, "events.jsonl");
  const resultPath = path.join(tmpDir, "result.md");
  fs.writeFileSync(eventsPath, `${JSON.stringify({ type: "session", id: "s1" })}\n`);

  const failures = validateAgentOutputs({ eventsPath, resultPath });
  assert.deepEqual(failures, ["required artifact missing: result.md"]);
});

test("validateAgentOutputs reports runtime errors from Pi event logs", () => {
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-dogfood-test-"));
  const eventsPath = path.join(tmpDir, "events.jsonl");
  const resultPath = path.join(tmpDir, "result.md");
  fs.writeFileSync(eventsPath, `${JSON.stringify({ type: "turn_end", errorMessage: "quota exceeded" })}\n`);
  fs.writeFileSync(resultPath, "# Result\n");

  const failures = validateAgentOutputs({ eventsPath, resultPath });
  assert.deepEqual(failures, ["pi runtime errors: quota exceeded"]);
});

test("cardTemplate anchors role cards to the role primary thread instead of the shared main thread", () => {
  const role = { name: "sales-kid" };
  const targets = {
    mainThread: { id: "thread-main" },
    primaryThread: { id: "thread-sales" },
    topic: { id: "topic-sales" },
    artifacts: [{ id: "artifact-one" }, { id: "artifact-two" }],
  };

  const template = cardTemplate(role, targets);
  assert.match(template, /"thread:thread-sales"/);
  assert.doesNotMatch(template, /"thread:thread-main"/);
});

test("parseArgs requires continue-run for later chapters", () => {
  assert.throws(
    () => parseArgs([
      "--scenario", "kids-lemonade-stand",
      "--chapter", "chapter-2",
      "--api-key", "test-key",
      "--agent-count", "3",
    ]),
    /requires --continue-run/,
  );
});

test("loadScenarioContent appends chapter markdown after the base scenario", () => {
  const loaded = loadScenarioContent("kids-lemonade-stand", "chapter-2", "http://127.0.0.1:43210");
  assert.match(loaded.combinedMarkdown, /# Kids Lemonade Stand/);
  assert.match(loaded.combinedMarkdown, /# Chapter 2:/);
  assert.match(loaded.chapterMarkdown, /continue from the existing stand state/i);
  assert.match(loaded.combinedMarkdown, /`http:\/\/127\.0\.0\.1:43210`/);
});

test("loadScenarioContent can load the cooperative tagging follow-up chapter", () => {
  const loaded = loadScenarioContent("kids-lemonade-stand", "chapter-3", "http://127.0.0.1:43210");
  assert.match(loaded.combinedMarkdown, /# Chapter 3:/);
  assert.match(loaded.chapterMarkdown, /auth principals list --taggable/);
  assert.match(loaded.chapterMarkdown, /triangle of tags and replies/i);
});

test("roleCardState prefers the existing card tied to the role thread", () => {
  const role = { name: "sales-kid", actorId: "actor-sales-kid" };
  const targets = {
    mainThread: { id: "thread-main" },
    primaryThread: { id: "thread-sales" },
    topic: { id: "topic-sales" },
    artifacts: [{ id: "artifact-one" }],
  };
  const chapterState = {
    cards: [
      {
        id: "card-main",
        parentThreadID: "thread-main",
        assigneeRefs: [],
      },
      {
        id: "card-sales",
        parentThreadID: "thread-sales",
        assigneeRefs: ["actor:actor-sales-kid"],
      },
    ],
  };

  const card = roleCardState(role, targets, chapterState);
  assert.equal(card?.id, "card-sales");
});

test("cardPatchTemplate reuses the existing card with a concurrency token", () => {
  const role = { name: "sales-kid", actorId: "actor-sales-kid" };
  const targets = {
    mainThread: { id: "thread-main" },
    primaryThread: { id: "thread-sales" },
    topic: { id: "topic-sales" },
    artifacts: [{ id: "artifact-one" }, { id: "artifact-two" }],
  };

  const template = cardPatchTemplate(role, targets, { id: "card-sales", updatedAt: "2026-04-11T00:00:00Z" });
  assert.match(template, /"if_updated_at": "2026-04-11T00:00:00Z"/);
  assert.match(template, /"actor:actor-sales-kid"/);
  assert.match(template, /"thread:thread-sales"/);
  assert.doesNotMatch(template, /"thread:thread-main"/);
});
