import { describe, expect, it, vi } from "vitest";

vi.mock("$lib/authSession.js", () => ({
  getAuthenticatedActorId: vi.fn(() => "actor-1"),
  getAuthenticatedAgent: vi.fn(() => ({ agent_id: "ag-1", actor_id: "actor-1" })),
}));

vi.mock("$lib/actorSession.js", () => ({
  getSelectedActorId: vi.fn(() => ""),
}));

vi.mock("$lib/workspaceContext.js", () => ({
  getCurrentOrganizationSlug: vi.fn(() => "org"),
  getCurrentWorkspaceSlug: vi.fn(() => "ws"),
}));

vi.mock("$lib/coreClientRequestHeaders.js", () => ({
  buildCoreRequestContextHeaders: vi.fn(() => ({ "x-test": "1" })),
}));

vi.mock("$lib/workspacePaths.js", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    APP_BASE_PATH: "",
  };
});

import { getBrowserCoreClientOptions } from "../../src/lib/coreClient.js";

describe("getBrowserCoreClientOptions", () => {
  it("wires actor providers for createOarCoreClient", () => {
    const opts = getBrowserCoreClientOptions();
    expect(opts.actorIdProvider()).toBe("actor-1");
    expect(opts.lockActorIdProvider()).toBe(true);
    expect(opts.requestContextHeadersProvider()).toMatchObject({ "x-test": "1" });
  });
});
