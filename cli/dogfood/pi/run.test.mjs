import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { analyzePiEventLog, validateAgentOutputs } from "./run.mjs";

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
