import { describe, expect, it, vi } from "vitest";

const controlSessionMocks = vi.hoisted(() => ({
  writeControlInviteToken: vi.fn(),
}));

vi.mock("$lib/server/controlSession.js", () => ({
  writeControlInviteToken: controlSessionMocks.writeControlInviteToken,
}));

import { load } from "../../src/routes/invites/[invite_token]/+page.server.js";

describe("invite token route", () => {
  it("stores the invite token in a cookie and redirects to auth", async () => {
    await expect(
      load({
        params: {
          invite_token: "oinv_123",
        },
      }),
    ).rejects.toMatchObject({
      status: 307,
      location: "/auth?invite=1&redirect=%2Fdashboard",
    });

    expect(controlSessionMocks.writeControlInviteToken).toHaveBeenCalledWith(
      expect.objectContaining({
        params: {
          invite_token: "oinv_123",
        },
      }),
      "oinv_123",
    );
  });
});
