import { describe, expect, it, vi } from "vitest";

vi.mock("$lib/authSession.js", () => ({
  getAuthenticatedActorId: vi.fn(() => "actor-1"),
  getAuthenticatedAgent: vi.fn(() => ({
    agent_id: "ag-1",
    actor_id: "actor-1",
  })),
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

import {
  getBrowserCoreClientOptions,
  captureInboxResponseSender,
} from "../../src/lib/coreClient.js";
import {
  getAuthenticatedActorId,
  getAuthenticatedAgent,
} from "../../src/lib/authSession.js";
import { buildCoreRequestContextHeaders } from "../../src/lib/coreClientRequestHeaders.js";

describe("getBrowserCoreClientOptions", () => {
  it("wires actor providers for createAnxCoreClient", () => {
    const opts = getBrowserCoreClientOptions();
    expect(opts.actorIdProvider()).toBe("actor-1");
    expect(opts.lockActorIdProvider()).toBe(true);
    expect(opts.requestContextHeadersProvider()).toMatchObject({
      "x-test": "1",
    });
  });

  it("sends a queued response with original routing and actor after the active scope changes", async () => {
    vi.mocked(getAuthenticatedActorId).mockReturnValue("actor-a");
    vi.mocked(getAuthenticatedAgent).mockReturnValue({ agent_id: "reader-a" });
    vi.mocked(buildCoreRequestContextHeaders).mockReturnValue({
      "x-anx-organization-slug": "org-a",
      "x-anx-workspace-slug": "workspace-a",
    });
    const fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ event: { id: "answer" } }), {
          status: 201,
          headers: { "content-type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", fetch);
    try {
      const send = captureInboxResponseSender();
      vi.mocked(getAuthenticatedActorId).mockReturnValue("actor-b");
      vi.mocked(getAuthenticatedAgent).mockReturnValue(null);
      vi.mocked(buildCoreRequestContextHeaders).mockReturnValue({
        "x-anx-organization-slug": "org-b",
        "x-anx-workspace-slug": "workspace-b",
      });
      await send("inbox:ask", {
        response_text: "Custom reply",
        outcome: "answered",
        actor_id: "spoofed",
      });
      const [, request] = fetch.mock.calls[0];
      expect(new Headers(request.headers).get("x-anx-workspace-slug")).toBe(
        "workspace-a",
      );
      expect(new Headers(request.headers).get("x-anx-organization-slug")).toBe(
        "org-a",
      );
      expect(JSON.parse(request.body).actor_id).toBe("actor-a");
      expect(fetch).toHaveBeenCalledOnce();
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
