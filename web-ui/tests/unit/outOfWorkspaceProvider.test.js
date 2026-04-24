import { beforeEach, describe, expect, it } from "vitest";

import {
  __resetOutOfWorkspaceProviderCacheForTests,
  createOutOfWorkspaceProvider,
  getOutOfWorkspaceProvider,
} from "../../src/lib/server/outOfWorkspace/index.js";

describe("outOfWorkspace provider selection", () => {
  beforeEach(() => {
    __resetOutOfWorkspaceProviderCacheForTests();
  });

  it("creates local provider when ANX_CONTROL_BASE_URL is unset", () => {
    const provider = createOutOfWorkspaceProvider({});
    expect(provider.mode).toBe("local");
    expect(Object.isFrozen(provider)).toBe(true);
  });

  it("creates hosted provider when ANX_CONTROL_BASE_URL is set", () => {
    const provider = createOutOfWorkspaceProvider({
      ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
    });
    expect(provider.mode).toBe("hosted");
    expect(provider.describeShellCapabilities().mode).toBe("hosted");
  });

  it("caches by env reference and control-plane URL", () => {
    const env = { ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100" };
    const a = getOutOfWorkspaceProvider(env);
    const b = getOutOfWorkspaceProvider(env);
    expect(a).toBe(b);
  });

  it("invalidates cache when env reference changes", () => {
    const a = getOutOfWorkspaceProvider({
      ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
    });
    const b = getOutOfWorkspaceProvider({
      ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
    });
    expect(a).not.toBe(b);
  });
});
