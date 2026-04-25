import { beforeEach, describe, expect, it, vi } from "vitest";

const anxCoreClientMocks = vi.hoisted(() => ({
  createAnxCoreClient: vi.fn(),
  verifyCoreSchemaVersion: vi.fn(),
}));

vi.mock("$lib/anxCoreClient", () => ({
  createAnxCoreClient: anxCoreClientMocks.createAnxCoreClient,
  verifyCoreSchemaVersion: anxCoreClientMocks.verifyCoreSchemaVersion,
}));

import { load } from "../../src/routes/o/[organization]/w/[workspace]/+layout.js";

describe("workspace +layout.js", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    anxCoreClientMocks.createAnxCoreClient.mockReturnValue({
      baseUrl: "https://anx.scalingforever.com/ws/david-zhang/personal",
    });
    anxCoreClientMocks.verifyCoreSchemaVersion.mockResolvedValue({
      schema_version: "test",
    });
  });

  it("verifies the workspace schema against workspace coreBaseUrl", async () => {
    const fetch = vi.fn();

    await load({
      fetch,
      url: new URL("https://anx.scalingforever.com/o/david-zhang/w/personal"),
      data: {
        workspace: {
          slug: "personal",
          organizationSlug: "david-zhang",
          coreBaseUrl: "https://anx.scalingforever.com/ws/david-zhang/personal",
        },
      },
    });

    expect(anxCoreClientMocks.createAnxCoreClient).toHaveBeenCalledWith(
      expect.objectContaining({
        baseUrl: "https://anx.scalingforever.com/ws/david-zhang/personal",
        fetchFn: fetch,
      }),
    );
    expect(anxCoreClientMocks.verifyCoreSchemaVersion).toHaveBeenCalledTimes(1);
  });
});
