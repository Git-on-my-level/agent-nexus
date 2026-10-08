import { expect as baseExpect, test } from "@playwright/test";

import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";
import { expectCleanLayout } from "../helpers/layoutAudit.js";
import { waitForAppReady } from "../helpers/pageReady.js";

// The dev server compiles routes on demand; first paint can take seconds.
const expect = baseExpect.configure({ timeout: 20_000 });
test.describe.configure({ timeout: 120_000 });

/**
 * A PM agent runs on the reader's own computer, never on the server, so a
 * workspace can have none. These specs cover the three states core reports:
 * every PM surface absent until one is onboarded, the setup flow flipping to
 * connected in place, and an onboarded PM that is not running.
 */

const ROOT = "/o/local/w/local";
const PHONE = { width: 390, height: 844 };

const minutesAgo = (minutes) =>
  new Date(Date.now() - minutes * 60_000).toISOString();

/* `GET /pm/presence` answers, as core shapes them. */
const NOT_ONBOARDED = {
  state: "not_onboarded",
  last_seen: null,
  runner: null,
  host: null,
  configured: false,
  connected: false,
};
const CONNECTED = {
  state: "connected",
  runner: "Hermes",
  host: "studio",
  configured: true,
  connected: true,
  last_seen: null,
};
const OFFLINE = {
  state: "offline",
  runner: "Hermes",
  host: "studio",
  configured: true,
  connected: false,
  last_seen: minutesAgo(12),
};
/*
 * A core that predates the computed `state`: registered, with no heartbeat
 * recorded yet. Its PM surfaces must survive the upgrade.
 */
const LEGACY_REGISTERED = { configured: true, connected: false };

const pmSlot = (page) => page.locator("[data-pm-nav]").first();

test.describe("no PM agent onboarded", () => {
  test("every PM surface is absent, and only setup is offered @states", async ({
    page,
  }) => {
    const api = await installWorkspaceApi(page, {
      pm: NOT_ONBOARDED,
      work: [
        {
          ref: "card:release",
          handle: "release",
          title: "Release the sample workspace",
          source: { authority: "nexus", native_status: "Ready" },
          phase: "review",
          freshness: { status: "unknown" },
        },
      ],
    });

    await page.goto(`${ROOT}/tasks`);
    await waitForAppReady(page);

    // The one entry point, in the slot Ask PM normally occupies.
    await expect(pmSlot(page)).toHaveAttribute("data-pm-nav", "setup");
    await expect(
      page.getByRole("link", { name: "Set up your PM" }),
    ).toBeVisible();
    await expect(page.getByRole("link", { name: "Ask PM" })).toHaveCount(0);
    await expectCleanLayout(page, "tasks with no PM");

    /*
     * The PM lists are still read, deliberately: a proposal filed earlier
     * still waits for a yes whether or not a PM is running, and the Inbox is
     * the only place to answer it. What is gated is the affordances.
     */
    expect(
      api.calls.filter((call) => call.path === "/pm/decisions"),
    ).not.toEqual([]);

    await page.goto(`${ROOT}/inbox`);
    await waitForAppReady(page);
    await expect(page.getByRole("link", { name: "Ask PM" })).toHaveCount(0);
    // The reader's own rows still load; the Inbox is not broken by a missing PM.
    await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
    await expectCleanLayout(page, "inbox with no PM");

    // The palette offers setup, and never a conversation nothing can answer.
    await page.keyboard.press("ControlOrMeta+k");
    const palette = page.getByRole("dialog", { name: "Command palette" });
    await expect(
      palette.getByRole("option", { name: "Set up your PM" }),
    ).toBeVisible();
    await expect(palette.getByRole("option", { name: /Ask PM/ })).toHaveCount(
      0,
    );
    await page.keyboard.press("Escape");
    await expect(palette).toHaveCount(0);

    // A bookmarked conversation lands on setup rather than on an error.
    await page.goto(`${ROOT}/pm`);
    await waitForAppReady(page);
    await expect(page).toHaveURL(new RegExp(`${ROOT}/pm/setup$`));
  });

  test("the setup flow explains, offers the command, and waits @states", async ({
    page,
  }) => {
    const api = await installWorkspaceApi(page, { pm: NOT_ONBOARDED });

    await page.goto(`${ROOT}/pm/setup`);
    await waitForAppReady(page);

    await expect(
      page.getByRole("heading", { name: "Set up your PM", level: 1 }),
    ).toBeVisible();
    await expect(page.getByText(/runs on your own computer/i)).toBeVisible();
    await expect(page.locator("[data-pm-install-command]")).toContainText(
      "pm install",
    );
    await expect(page.locator("[data-pm-setup-state]")).toHaveAttribute(
      "data-pm-setup-state",
      "waiting",
    );
    await expect(
      page.getByText(/Waiting for your PM to connect/i),
    ).toBeVisible();
    await expectCleanLayout(page, "pm setup waiting");

    // The first heartbeat arrives: the page flips in place, and the shell
    // grows its PM surfaces without a reload.
    api.pm = CONNECTED;
    await expect(page.locator("[data-pm-setup-state]")).toHaveAttribute(
      "data-pm-setup-state",
      "connected",
    );
    await expect(page.getByText(/Your PM is running/i)).toBeVisible();
    await expect(pmSlot(page)).toHaveAttribute("data-pm-nav", "ask");
    await expect(page.getByRole("link", { name: "Ask PM" })).toBeVisible();
    await expectCleanLayout(page, "pm setup connected");

    // The shell carries the new state across a client-side navigation, so
    // other surfaces grow their PM affordances without a reload.
    await page.getByRole("link", { name: "Tasks" }).first().click();
    await expect(page.getByRole("heading", { name: "Tasks" })).toBeVisible();
    // The page's own header action, not the shell's slot.
    await expect(
      page
        .getByRole("link", { name: "Ask PM", exact: true })
        .locator(":scope:not([data-pm-nav])"),
    ).toHaveCount(1);
  });

  test("setup reads at phone width @states", async ({ page }) => {
    await page.setViewportSize(PHONE);
    await installWorkspaceApi(page, { pm: NOT_ONBOARDED });

    await page.goto(`${ROOT}/pm/setup`);
    await waitForAppReady(page);
    await expect(page.locator("[data-pm-install-command]")).toBeVisible();
    // The bottom bar keeps the PM slot, with copy that fits a tab.
    await expect(page.getByRole("link", { name: "Set up PM" })).toBeVisible();
    await expectCleanLayout(page, "pm setup phone");
  });
});

test.describe("an onboarded PM that is not running", () => {
  test("keeps PM features and says it is offline @states", async ({ page }) => {
    await installWorkspaceApi(page, { pm: OFFLINE, conversations: [] });

    await page.goto(`${ROOT}/pm`);
    await waitForAppReady(page);

    await expect(page.getByRole("heading", { name: "Ask PM" })).toBeVisible();
    await expect(pmSlot(page)).toHaveAttribute("data-pm-nav", "ask");
    await expect(page.locator("[data-pm-status]")).toHaveAttribute(
      "data-pm-status",
      "offline",
    );
    await expect(page.locator("[data-pm-offline-note]")).toContainText(
      "anx pm status",
    );
    // The runner and host core reported, in the unobtrusive status line.
    await expect(page.locator("[data-pm-status]")).toContainText("Hermes");
    // Asking stays possible: the answer waits for the machine to come back.
    await expect(page.locator("#pm-message")).toBeEnabled();
    await expectCleanLayout(page, "ask pm offline");

    // Manage shows the state and the commands that change it.
    await page.getByRole("link", { name: "Manage" }).click();
    await waitForAppReady(page);
    await expect(page.locator("[data-pm-manage-state]")).toHaveAttribute(
      "data-pm-manage-state",
      "offline",
    );
    await expect(
      page.locator('[data-pm-command="pm-status-command"]'),
    ).toContainText("pm status");
    await expect(
      page.locator('[data-pm-command="pm-uninstall-command"]'),
    ).toContainText("pm uninstall");
    await expect(page.getByText("Hermes · studio")).toBeVisible();
    await expectCleanLayout(page, "pm manage offline");
  });
});

test.describe("a core that predates the computed PM state", () => {
  /*
   * It reports only `configured` / `connected`. A workspace that has used its
   * PM for months reports no recorded heartbeat the first time it runs such a
   * core; reading that as "no PM" would hide its surfaces on every upgrade.
   */
  test("keeps a registered PM's surfaces from the legacy fields @states", async ({
    page,
  }) => {
    await installWorkspaceApi(page, {
      pm: LEGACY_REGISTERED,
      conversations: [],
    });

    await page.goto(`${ROOT}/pm`);
    await waitForAppReady(page);
    await expect(page.getByRole("heading", { name: "Ask PM" })).toBeVisible();
    await expect(pmSlot(page)).toHaveAttribute("data-pm-nav", "ask");
    await expect(page.locator("[data-pm-status]")).toHaveAttribute(
      "data-pm-status",
      "offline",
    );
  });
});

test.describe("a core that does not report PM state", () => {
  /*
   * Back-compatibility: an older core carries no PM state. Hiding the
   * product's primary verb on that evidence would break a workspace whose PM
   * is running fine, so PM surfaces stay and no setup is offered.
   */
  test("leaves PM features alone @states", async ({ page }) => {
    await installWorkspaceApi(page, { pm: null, conversations: [] });

    await page.goto(`${ROOT}/pm`);
    await waitForAppReady(page);
    await expect(page.getByRole("heading", { name: "Ask PM" })).toBeVisible();
    await expect(pmSlot(page)).toHaveAttribute("data-pm-nav", "ask");
    await expect(page.locator("[data-pm-status]")).toHaveCount(0);
    await expect(page.locator("[data-pm-offline-note]")).toHaveCount(0);
  });
});
