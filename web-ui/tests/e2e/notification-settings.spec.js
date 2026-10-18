import { expect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * The workspace Notifications page with a principal-addressed channel: one
 * on/off switch with the declared label, no address field, and a save that
 * keeps the person's other channels.
 */

const PAGE = "/o/local/w/local/notifications";

async function installSettings(page, initial) {
  const state = { body: initial, puts: [] };
  await page.route("**/notification-settings", async (route) => {
    const request = route.request();
    if (request.method() === "PUT") {
      const body = request.postDataJSON();
      state.puts.push(body);
      state.body = { ...state.body, channels: body.channels };
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(state.body),
    });
  });
  return state;
}

const DECLARED = {
  channels: [
    {
      channel: "email",
      address: "me@example.com",
      push_enabled: true,
      digest_enabled: false,
    },
  ],
  available_channels: ["email", "hosted"],
  channel_options: {
    email: { addressing: "settings", kinds: ["push", "digest"] },
    hosted: {
      addressing: "principal",
      kinds: ["push"],
      label: "Hosted notifications",
      manage_url: "https://app.example.com/account/notifications",
    },
  },
};

for (const viewport of AUDIT_VIEWPORTS.filter(
  (v) => v.name === "phone" || v.name === "desktop",
)) {
  test.describe(`notifications page @ ${viewport.name}`, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height } });

    test("a principal channel is one switch, on by default", async ({
      page,
    }) => {
      test.setTimeout(90_000);
      await installWorkspaceApi(page, {});
      const state = await installSettings(page, DECLARED);
      await page.goto(PAGE);
      const fieldset = page.getByRole("group", {
        name: "Hosted notifications",
      });
      await expect(fieldset).toBeVisible({ timeout: 60_000 });
      const toggle = fieldset.getByRole("checkbox", {
        name: /Send notifications from this workspace/,
      });
      await expect(toggle).toBeChecked();
      await expect(fieldset.getByRole("textbox")).toHaveCount(0);
      await expect(
        fieldset.getByRole("link", {
          name: "Change where notifications reach you",
        }),
      ).toHaveAttribute(
        "href",
        "https://app.example.com/account/notifications",
      );
      await expectCleanLayout(page, "principal channel on");

      await toggle.uncheck();
      await page.getByRole("button", { name: "Save" }).click();
      await expect
        .poll(() => state.puts.length, { timeout: 15_000 })
        .toBeGreaterThan(0);
      expect(state.puts.at(-1).channels).toEqual([
        {
          channel: "email",
          address: "me@example.com",
          push_enabled: true,
          digest_enabled: false,
        },
        {
          channel: "hosted",
          address: "",
          push_enabled: false,
          digest_enabled: false,
        },
      ]);
      await expect(toggle).not.toBeChecked();
      await expectCleanLayout(page, "principal channel off");
    });
  });
}
