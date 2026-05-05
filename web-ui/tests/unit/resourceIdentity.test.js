import { describe, expect, it } from "vitest";

import {
  resourceCopyValue,
  resourceDisplayLabel,
  resourceRouteSegment,
  revisionRouteSegment,
  typedResourceRef,
} from "../../src/lib/resourceIdentity.js";

describe("resourceIdentity", () => {
  it("prefers handles for route segments over internal ids", () => {
    const board = {
      id: "42a2f537-894e-49ca-a167-a67059e89155",
      ref: "board:anx-features",
      handle: "anx-features",
      title: "Agent Nexus features",
    };

    expect(resourceRouteSegment(board, "board")).toBe("anx-features");
    expect(resourceCopyValue("board", board)).toBe("board:anx-features");
    expect(resourceDisplayLabel(board)).toBe("Agent Nexus features");
  });

  it("falls back to typed refs when handle is absent", () => {
    const card = {
      id: "cafc4d04-d401-474a-8979-f4185a4eeb90",
      ref: "card:cli-json-body-input",
      title: "CLI JSON body input",
    };

    expect(resourceRouteSegment(card, "card")).toBe("cli-json-body-input");
    expect(typedResourceRef("card", card)).toBe("card:cli-json-body-input");
  });

  it("uses public revision handles before revision debug ids", () => {
    const revision = {
      revision_id: "c9c4bbba-e076-4909-8c86-7aeef88cab4a",
      ref: "document_revision:product-constitution-v3",
      handle: "product-constitution-v3",
    };

    expect(revisionRouteSegment(revision, "document_revision")).toBe(
      "product-constitution-v3",
    );
  });

  it("does not synthesize copy refs from internal UUIDs", () => {
    const topic = {
      id: "42a2f537-894e-49ca-a167-a67059e89155",
    };

    expect(resourceCopyValue("topic", topic)).toBe("");
    expect(resourceDisplayLabel(topic)).toBe("Untitled resource");
  });
});
