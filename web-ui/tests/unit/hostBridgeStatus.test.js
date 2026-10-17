import { afterEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
vi.mock("$lib/coreClient", () => ({
  coreClient: { getHostBridgeStatus: vi.fn() },
}));
vi.mock("$lib/liveWorkspaceEvents.js", () => ({
  liveAgentChanges: () => () => {},
}));
import { coreClient } from "$lib/coreClient";
import {
  bridgeBadge,
  hostBridgeStatus,
  observeHostBridge,
} from "$lib/hostBridgeStatus.js";
let releases = [];
afterEach(() => {
  releases.forEach((r) => r());
  releases = [];
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});
describe("host bridge liveness", () => {
  it("ages a zero-agent host and never turns an unknown read into absence", () => {
    const h = { id: "host" };
    const seen = { bridge: { expires_at: "2026-10-17T00:05:00Z" }, agents: [] };
    expect(bridgeBadge(h, undefined).state).toBe("unknown");
    expect(bridgeBadge(h, { bridge: null }).state).toBe("none");
    expect(bridgeBadge(h, seen, Date.parse("2026-10-17T00:04:59Z")).state).toBe(
      "live",
    );
    expect(bridgeBadge(h, seen, Date.parse("2026-10-17T00:05:00Z")).state).toBe(
      "stale",
    );
    expect(bridgeBadge({ ...h, revoked_at: "now" }, seen).state).toBe(
      "revoked",
    );
  });
  it("coalesces visible host reads and discards late workspace responses", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("document", {
      hidden: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    });
    let finish;
    coreClient.getHostBridgeStatus.mockImplementationOnce(
      () => new Promise((resolve) => (finish = resolve)),
    );
    releases.push(observeHostBridge("one", "a"), observeHostBridge("one", "b"));
    await vi.advanceTimersByTimeAsync(100);
    expect(coreClient.getHostBridgeStatus).toHaveBeenCalledWith(["a", "b"]);
    coreClient.getHostBridgeStatus.mockResolvedValue({
      hosts: [{ host_id: "new", bridge: null }],
    });
    releases.push(observeHostBridge("two", "new"));
    await vi.advanceTimersByTimeAsync(100);
    finish({ hosts: [{ host_id: "a", bridge: { version: "old" } }] });
    await Promise.resolve();
    expect(get(hostBridgeStatus).workspace).toBe("two");
    expect(get(hostBridgeStatus).hosts.a).toBeUndefined();
  });
});
