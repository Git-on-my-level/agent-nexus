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
  summarizeBridgeFleet,
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
  it("counts only checked, unrevoked machines as live or not", () => {
    const at = Date.parse("2026-10-17T00:00:00Z");
    const live = { bridge: { expires_at: "2026-10-17T00:05:00Z" } };
    const stale = { bridge: { expires_at: "2026-10-16T23:00:00Z" } };
    const hosts = [
      { id: "a" },
      { id: "b" },
      { id: "c" },
      { id: "gone", revoked_at: "2026-10-01T00:00:00Z" },
      { id: "unchecked" },
    ];
    const status = { a: live, b: stale, c: { bridge: null }, gone: live };
    expect(
      summarizeBridgeFleet(hosts, status, ["a", "b", "c", "gone"], at),
    ).toEqual({ total: 3, live: 1 });
    // An old core or a failed read checks ids without observing them.
    expect(summarizeBridgeFleet(hosts, {}, ["a", "b", "c"], at)).toEqual({
      total: 0,
      live: 0,
    });
    expect(summarizeBridgeFleet(hosts, status, [], at)).toEqual({
      total: 0,
      live: 0,
    });
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

describe("host bridge initial and failed reads", () => {
  it("shows loading until a first attempt finishes and retains good data on later errors", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("document", {
      hidden: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    });
    let reject;
    coreClient.getHostBridgeStatus.mockImplementationOnce(
      () =>
        new Promise((_, fail) => {
          reject = fail;
        }),
    );
    releases.push(observeHostBridge("old-core", "host"));
    await vi.advanceTimersByTimeAsync(100);
    let state = get(hostBridgeStatus);
    expect(state.checked).toEqual([]);
    expect(
      bridgeBadge(
        { id: "host" },
        state.hosts.host,
        Date.now(),
        !state.checked.includes("host"),
      ).state,
    ).toBe("loading");
    reject(new Error("404"));
    await Promise.resolve();
    state = get(hostBridgeStatus);
    expect(state.checked).toEqual(["host"]);
    expect(bridgeBadge({ id: "host" }, state.hosts.host).label).toBe(
      "Bridge unavailable",
    );
    const snapshot = {
      host_id: "host",
      bridge: { expires_at: new Date(Date.now() + 120000).toISOString() },
    };
    coreClient.getHostBridgeStatus.mockResolvedValueOnce({ hosts: [snapshot] });
    await vi.advanceTimersByTimeAsync(60000);
    expect(get(hostBridgeStatus).hosts.host).toEqual(snapshot);
    coreClient.getHostBridgeStatus.mockRejectedValueOnce(new Error("offline"));
    await vi.advanceTimersByTimeAsync(60000);
    state = get(hostBridgeStatus);
    expect(state.error).toBe(true);
    expect(state.hosts.host).toEqual(snapshot);
  });
});
