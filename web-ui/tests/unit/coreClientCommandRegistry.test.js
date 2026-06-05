import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import {
  assertCoreClientCommandRegistry,
  extractCoreClientCommandIdsFromSource,
  findCoreClientCommandRegistryIssues,
} from "../../src/lib/coreClientCommandRegistry.js";

const anxCoreClientSource = fs.readFileSync(
  path.join(
    path.dirname(fileURLToPath(import.meta.url)),
    "../../src/lib/anxCoreClient.js",
  ),
  "utf8",
);

describe("coreClientCommandRegistry", () => {
  it("extracts adapter and invokeCommand bindings from anxCoreClient.js", () => {
    const commandIds =
      extractCoreClientCommandIdsFromSource(anxCoreClientSource);

    expect(commandIds).toContain("cards.create");
    expect(commandIds).toContain("home.unread");
    expect(commandIds).toContain("inbox.respond");
    expect(commandIds).not.toContain("boards.cards.create");
  });

  it("flags command ids that are absent from the generated registry", () => {
    const issues = findCoreClientCommandRegistryIssues({
      commandIds: ["boards.cards.create", "cards.create"],
      commands: [{ command_id: "cards.create", ts_method: "cardsCreate" }],
    });

    expect(issues).toEqual([
      {
        commandId: "boards.cards.create",
        reason: "missing from generated command registry",
      },
    ]);
  });

  it("requires every anxCoreClient binding to exist in the generated registry", () => {
    expect(() =>
      assertCoreClientCommandRegistry({ source: anxCoreClientSource }),
    ).not.toThrow();
  });
});
