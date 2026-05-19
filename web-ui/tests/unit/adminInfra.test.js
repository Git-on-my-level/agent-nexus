import { describe, expect, it } from "vitest";

import {
  filesystemUsage,
  formatBytePair,
  hostWarning,
  percentUsed,
  runtimeCountsForHost,
  telemetryResourceCards,
} from "../../src/lib/hosted/adminInfra.js";

describe("admin infra helpers", () => {
  it("formats host metric percentages and empty telemetry explicitly", () => {
    expect(percentUsed(25, 100)).toBe(25);
    expect(percentUsed(1, 0)).toBeNull();
    expect(formatBytePair({})).toBe("Not wired");
    expect(filesystemUsage({}).byteLabel).toBe("Not wired");
  });

  it("keeps unknown telemetry visible instead of rendering healthy zeroes", () => {
    const cards = telemetryResourceCards({
      telemetry_freshness: "unknown",
      latest_snapshot: null,
    });
    expect(cards.map((card) => card.value)).toEqual([
      "Not wired",
      "Not wired",
      "Not wired",
      "Not wired",
    ]);
    expect(hostWarning({ telemetry_freshness: "unknown" })).toBe(
      "Live resource telemetry is not wired",
    );
  });

  it("summarizes resource telemetry from the signed host snapshot shape", () => {
    const cards = telemetryResourceCards({
      latest_snapshot: {
        payload: {
          cpu: { load1: 1.234, load5: 0.5, load15: 0.25 },
          memory: {
            total_bytes: 100,
            used_bytes: 60,
            free_bytes: 40,
          },
          workspace_root_disk: {
            bytes: {
              total_bytes: 100,
              used_bytes: 75,
              free_bytes: 25,
            },
          },
          docker_root_disk: {
            bytes: {
              total_bytes: 100,
              used_bytes: 10,
              free_bytes: 90,
            },
          },
        },
      },
    });
    expect(cards[0]).toMatchObject({ label: "CPU load", value: "1.23" });
    expect(cards[1]).toMatchObject({ label: "Memory", value: "60%" });
    expect(cards[2]).toMatchObject({ label: "Workspace disk", value: "75%" });
  });

  it("derives host runtime counts and stale runtime metadata from workspaces", () => {
    const counts = runtimeCountsForHost(
      [
        {
          id: "ws_1",
          host_id: "host_a",
          runtime_power_state: "running",
          container_id_short: "abcdef123456",
          runtime_image_tag: "anx-core:a",
          heartbeat_freshness: "fresh",
        },
        {
          id: "ws_2",
          host_id: "host_a",
          runtime_power_state: "running",
          runtime_image_tag: "anx-core:a",
          heartbeat_freshness: "stale",
        },
        {
          id: "ws_3",
          host_id: "host_b",
          runtime_power_state: "stopped",
          heartbeat_freshness: "fresh",
        },
      ],
      { id: "host_a", label: "Host A" },
    );
    expect(counts.total).toBe(2);
    expect(counts.running).toBe(2);
    expect(counts.staleHeartbeat).toBe(1);
    expect(counts.staleRuntimeMetadata).toBe(1);
    expect(counts.imageTags).toEqual([{ reference: "anx-core:a", count: 2 }]);
  });
});
