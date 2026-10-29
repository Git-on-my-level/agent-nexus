import { expect, it, vi } from "vitest";
import { requestWorkspaceActivation } from "../../src/lib/workspaceActivation.js";
import {
  WORKSPACE_NOT_FOUND_MESSAGE,
  WORKSPACE_UNREACHABLE_MESSAGE,
} from "../../src/lib/workspaceFailure.js";
it("backs off on 429/503 then returns a successful activation", async () => {
  const fetchFn = vi
    .fn()
    .mockResolvedValueOnce(new Response("{}", { status: 429 }))
    .mockResolvedValueOnce(new Response("{}", { status: 503 }))
    .mockResolvedValueOnce(Response.json({ agent: { agent_id: "human" } }));
  const wait = vi.fn(async () => {}),
    onRetry = vi.fn();
  const result = await requestWorkspaceActivation({
    url: "/auth/workspace-session",
    input: {},
    fetchFn,
    wait,
    onRetry,
  });
  expect(result.agent.agent_id).toBe("human");
  expect(wait.mock.calls).toEqual([[500], [1000]]);
  expect(onRetry).toHaveBeenCalledTimes(2);
});
it("bounds retries and respects Retry-After without an unbounded wait", async () => {
  const wait = vi.fn(async () => {});
  const fetchFn = vi.fn(
    async () =>
      new Response("{}", { status: 503, headers: { "retry-after": "3600" } }),
  );
  await expect(
    requestWorkspaceActivation({
      url: "/auth/workspace-session",
      input: {},
      fetchFn,
      wait,
    }),
  ).rejects.toMatchObject({
    name: "WorkspaceUnavailableError",
    message: WORKSPACE_UNREACHABLE_MESSAGE,
  });
  expect(fetchFn).toHaveBeenCalledTimes(4);
  expect(wait.mock.calls).toEqual([[30000], [30000], [30000]]);
});
it("does not retry authorization failures or an obsolete account", async () => {
  const fetchFn = vi.fn(async () => new Response("{}", { status: 403 }));
  await expect(
    requestWorkspaceActivation({
      url: "/auth/workspace-session",
      input: {},
      fetchFn,
    }),
  ).rejects.toThrow();
  expect(fetchFn).toHaveBeenCalledTimes(1);
  expect(
    await requestWorkspaceActivation({
      url: "/auth/workspace-session",
      input: {},
      fetchFn,
      isCurrent: () => false,
    }),
  ).toBeNull();
  expect(fetchFn).toHaveBeenCalledTimes(1);
});
it("retries network errors and other 5xx answers like an outage", async () => {
  const fetchFn = vi
    .fn()
    .mockRejectedValueOnce(new TypeError("Failed to fetch"))
    .mockResolvedValueOnce(new Response("{}", { status: 502 }))
    .mockResolvedValueOnce(Response.json({ agent: { agent_id: "human" } }));
  const wait = vi.fn(async () => {});
  const onRetry = vi.fn();
  const result = await requestWorkspaceActivation({
    url: "/auth/workspace-session",
    input: {},
    fetchFn,
    wait,
    onRetry,
  });
  expect(result.agent.agent_id).toBe("human");
  expect(onRetry).toHaveBeenCalledTimes(2);
});
it("reports a workspace that stays unreachable as unavailable, not as a definitive failure", async () => {
  const fetchFn = vi.fn(async () => {
    throw new TypeError("Failed to fetch");
  });
  await expect(
    requestWorkspaceActivation({
      url: "/auth/workspace-session",
      input: {},
      fetchFn,
      wait: async () => {},
    }),
  ).rejects.toMatchObject({ code: "workspace_unavailable" });
  expect(fetchFn).toHaveBeenCalledTimes(4);
});
it("reports a missing workspace (404) as not found, without retrying", async () => {
  const fetchFn = vi.fn(async () => new Response("{}", { status: 404 }));
  await expect(
    requestWorkspaceActivation({
      url: "/auth/workspace-session",
      input: {},
      fetchFn,
    }),
  ).rejects.toMatchObject({
    code: "workspace_not_found",
    message: WORKSPACE_NOT_FOUND_MESSAGE,
  });
  expect(fetchFn).toHaveBeenCalledTimes(1);
});
it("retries a malformed success instead of accepting it", async () => {
  const fetchFn = vi
    .fn()
    .mockResolvedValueOnce(new Response("<html>proxy</html>"))
    .mockResolvedValueOnce(Response.json({ agent: null }))
    .mockResolvedValueOnce(Response.json({ agent: { agent_id: "human" } }));
  const result = await requestWorkspaceActivation({
    url: "/auth/workspace-session",
    input: {},
    fetchFn,
    wait: async () => {},
  });
  expect(result.agent.agent_id).toBe("human");
  expect(fetchFn).toHaveBeenCalledTimes(3);
});
it.each([
  ["a numeric", 42],
  ["a whitespace-only", "   "],
])("does not accept %s agent_id as an activation", async (_label, agentId) => {
  const fetchFn = vi
    .fn()
    .mockResolvedValueOnce(Response.json({ agent: { agent_id: agentId } }))
    .mockResolvedValueOnce(Response.json({ agent: { agent_id: "human" } }));
  const result = await requestWorkspaceActivation({
    url: "/auth/workspace-session",
    input: {},
    fetchFn,
    wait: async () => {},
  });
  expect(result.agent.agent_id).toBe("human");
  expect(fetchFn).toHaveBeenCalledTimes(2);
});
