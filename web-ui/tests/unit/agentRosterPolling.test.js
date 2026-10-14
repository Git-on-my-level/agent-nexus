// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({ fetch: vi.fn(), onChange: null }));
vi.mock("$lib/coreClient", () => ({ coreClient: { listAgents: mocks.fetch } }));
vi.mock("$lib/actorSession", () => ({
  replaceAgentRegistry: vi.fn(),
  markAgentDirectoryRead: vi.fn(),
}));
vi.mock("$lib/liveWorkspaceEvents.js", () => ({
  liveAgentChanges: ({ onChange }) => {
    mocks.onChange = onChange;
    return () => {};
  },
  liveWorkspaceEvents: () => () => {},
}));
import {
  resetAgentRoster,
  startAgentRoster,
} from "../../src/lib/agentRoster.js";
afterEach(() => {
  resetAgentRoster();
  vi.useRealTimers();
  vi.restoreAllMocks();
});
describe("roster read admission", () => {
  it("debounces an event received during a slow read and pauses hidden-tab refreshes", async () => {
    vi.useFakeTimers();
    vi.spyOn(document, "hidden", "get").mockReturnValue(false);
    let finish;
    mocks.fetch
      .mockReset()
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      )
      .mockResolvedValue({ agents: [] });
    const stop = startAgentRoster("test");
    mocks.onChange();
    mocks.onChange();
    expect(mocks.fetch).toHaveBeenCalledTimes(1);
    finish({ agents: [] });
    await vi.advanceTimersByTimeAsync(799);
    expect(mocks.fetch).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(mocks.fetch).toHaveBeenCalledTimes(2);
    vi.spyOn(document, "hidden", "get").mockReturnValue(true);
    mocks.onChange();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(mocks.fetch).toHaveBeenCalledTimes(2);
    vi.spyOn(document, "hidden", "get").mockReturnValue(false);
    document.dispatchEvent(new Event("visibilitychange"));
    expect(mocks.fetch).toHaveBeenCalledTimes(3);
    stop();
  });
});
