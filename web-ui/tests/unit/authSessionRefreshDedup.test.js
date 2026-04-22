import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  refreshWorkspaceAuthSession,
  resetWorkspaceAuthRefreshStateForTests,
} from "../../src/lib/server/authSession.js";

describe("refreshWorkspaceAuthSession dedup", () => {
  beforeEach(() => {
    resetWorkspaceAuthRefreshStateForTests();
  });

  afterEach(() => {
    resetWorkspaceAuthRefreshStateForTests();
    vi.unstubAllGlobals();
  });

  it("shares one in-flight refresh for concurrent callers with the same refresh token", async () => {
    let inFlight = 0;
    let maxConcurrent = 0;
    const fetchMock = vi.fn(async () => {
      inFlight += 1;
      maxConcurrent = Math.max(maxConcurrent, inFlight);
      await new Promise((r) => {
        setTimeout(r, 5);
      });
      inFlight -= 1;
      return new Response(
        JSON.stringify({
          tokens: {
            access_token: "at-new",
            refresh_token: "rt-same",
          },
        }),
        { status: 200, headers: { "content-type": "application/json" } },
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    const event = {
      cookies: {
        get: vi.fn((name) =>
          name === "oar_ui_session_alpha" ? "rt-same" : "",
        ),
        set: vi.fn(),
        delete: vi.fn(),
      },
      url: new URL("http://localhost/"),
    };

    const p1 = refreshWorkspaceAuthSession({
      event,
      workspaceSlug: "alpha",
      coreBaseUrl: "http://127.0.0.1:9000",
    });
    const p2 = refreshWorkspaceAuthSession({
      event,
      workspaceSlug: "alpha",
      coreBaseUrl: "http://127.0.0.1:9000",
    });
    await Promise.all([p1, p2]);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(maxConcurrent).toBe(1);
  });
});
