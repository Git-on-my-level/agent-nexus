import { expect, it, vi } from "vitest";
import { requestWorkspaceActivation } from "../../src/lib/workspaceActivation.js";
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
  ).rejects.toThrow("Please try again");
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
