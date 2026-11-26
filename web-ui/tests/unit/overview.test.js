import { describe, expect, it } from "vitest";

import { humanLiveRefreshDelay } from "../../src/lib/overview.js";

describe("tasks human live refresh", () => {
  it("runs a full rescan at most once per 10 seconds", () => {
    expect(humanLiveRefreshDelay(10_000, 0)).toBe(0);
    expect(humanLiveRefreshDelay(12_000, 10_000)).toBe(8_000);
    expect(humanLiveRefreshDelay(20_000, 10_000)).toBe(0);
  });
});
