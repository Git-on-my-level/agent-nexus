import { expect as baseExpect, test } from "@playwright/test";

import {
  deferred,
  expectNoClippedContent,
  installWorkspaceApi,
} from "../helpers/workspaceApiMock.js";
import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

// The dev server compiles routes on demand; first paint of a route can take
// several seconds under parallel runs.
const expect = baseExpect.configure({ timeout: 20_000 });
test.describe.configure({ timeout: 120_000 });

/**
 * Layout audit for the workspace settings surfaces: Secrets, Audit (events),
 * Integrations and the More hub. Same method as the Ask PM / threads spec:
 * drive every reachable state with deliberately long content and audit the
 * geometry after each transition.
 */

const ROOT = "/o/local/w/local";
const LONG_TOKEN =
  "unbroken_0123456789abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOP";
const LONG_SENTENCE =
  "The reader asked for the full reconciliation history of the vendor handoff, including every source revision we could not verify. ";

const minutesAgo = (minutes) =>
  new Date(Date.now() - minutes * 60_000).toISOString();

const bothEnds = { scrollPositions: ["top", "bottom"] };

const SECRETS = [
  {
    id: "secret-openai",
    name: "OPENAI_API_KEY",
    description: "API key for the summarizer agent",
    updated_at: minutesAgo(600),
  },
  {
    id: "secret-long",
    name: `VENDOR_${LONG_TOKEN}`,
    description: `${LONG_SENTENCE}${LONG_TOKEN}`,
    updated_at: minutesAgo(30),
  },
  {
    id: "secret-bare",
    name: "GITHUB_TOKEN",
    description: "",
    updated_at: minutesAgo(5),
  },
];

const EVENTS = [
  {
    id: "evt-message",
    ts: minutesAgo(12),
    type: "message_posted",
    actor_id: "actor-operator",
    thread_id: "thread-onboarding",
    refs: ["thread:thread-onboarding", `card:${LONG_TOKEN}`],
    summary: `Message: ${LONG_SENTENCE}${LONG_TOKEN}`,
    payload: { text: `${LONG_SENTENCE}${LONG_TOKEN}` },
  },
  {
    id: "evt-card",
    ts: minutesAgo(45),
    type: "card_created",
    actor_id: "actor-hermes",
    refs: [`card:${LONG_TOKEN}`],
    summary: `Card created: ${LONG_TOKEN}`,
    payload: {},
  },
  {
    id: "evt-unknown",
    ts: minutesAgo(90),
    type: "some_future_event_type",
    actor_id: "actor-operator",
    refs: [],
    summary: "",
    payload: {},
  },
];

function workRecord(overrides) {
  return {
    id: "release",
    ref: "card:release",
    handle: "release",
    title: "Release the sample workspace",
    source: {
      authority: "github",
      connection_id: "sample-github",
      native_status: "Awaiting reviewer",
    },
    freshness: {
      status: "fresh",
      last_observed_at: minutesAgo(5),
      stale_after_seconds: 3600,
    },
    refresh: { state: "idle", next_due_at: minutesAgo(-30) },
    ...overrides,
  };
}

const WORK = [
  workRecord({}),
  workRecord({
    id: "vendor",
    ref: `card:${LONG_TOKEN}`,
    handle: LONG_TOKEN,
    title: `Vendor sample delivery ${LONG_TOKEN}`,
    freshness: {
      status: "stale",
      last_observed_at: minutesAgo(400),
      stale_after_seconds: 3600,
    },
    refresh: {
      state: "failed",
      next_due_at: minutesAgo(-5),
      last_error: {
        code: "permission_denied",
        detail: `${LONG_SENTENCE}${LONG_TOKEN}`,
        connection_id: "sample-github",
      },
    },
  }),
  workRecord({
    id: "multica",
    ref: "card:multica",
    handle: "multica",
    title: "Multica delivery",
    source: {
      authority: "multica",
      connection_id: `connection-${LONG_TOKEN}`,
      native_status: "Custom waiting state",
    },
    freshness: { status: "unknown" },
    refresh: { state: "queued" },
  }),
];

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`settings states @ ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      permissions: ["clipboard-read", "clipboard-write"],
    });

    test("secrets: loading, empty, populated, reveal and delete", async ({
      page,
    }) => {
      const api = await installWorkspaceApi(page, {
        secrets: [],
        revealValue: `sk-${LONG_TOKEN}`,
      });
      api.hold.secrets = deferred();
      await page.goto(`${ROOT}/secrets`);
      await expect(
        page.getByRole("heading", { name: "Secrets" }),
      ).toBeVisible();
      await expectCleanLayout(page, "secrets loading");

      api.hold.secrets.resolve();
      api.hold = {};
      await expect(page.getByText("No secrets configured.")).toBeVisible();
      await expectCleanLayout(page, "secrets empty", bothEnds);

      await page.getByRole("button", { name: "New secret" }).click();
      await expect(page.getByLabel("Name")).toBeVisible();
      await expectCleanLayout(page, "create form open", bothEnds);

      await page.getByLabel("Name").fill(`VENDOR_${LONG_TOKEN}`);
      await page.getByLabel("Value").fill(`sk-${LONG_TOKEN}`);
      await page
        .getByLabel("Description (optional)")
        .fill(`${LONG_SENTENCE}${LONG_TOKEN}`);
      api.fail.createSecret = {
        message: `secret name already exists in this workspace. ${LONG_SENTENCE}`,
      };
      await page.getByRole("button", { name: "Create" }).click();
      await expect(page.getByText(/secret name already exists/)).toBeVisible();
      await expectCleanLayout(page, "create secret failed", bothEnds);

      api.fail = {};
      api.hold.createSecret = deferred();
      await page.getByRole("button", { name: "Create" }).click();
      await expect(
        page.getByRole("button", { name: "Creating..." }),
      ).toBeVisible();
      await expectCleanLayout(page, "creating secret");
      api.secrets = SECRETS;
      api.hold.createSecret.resolve();
      api.hold = {};
      await expect(page.getByText("OPENAI_API_KEY")).toBeVisible();
      await expectCleanLayout(page, "secrets populated", bothEnds);
      await expectNoClippedContent(page, "secrets populated");

      // The second row carries the very long name and a very long value.
      await page.getByRole("button", { name: "Reveal" }).nth(1).click();
      await expect(page.getByText(`sk-${LONG_TOKEN}`)).toBeVisible();
      await expectCleanLayout(page, "secret revealed", bothEnds);
      await expectNoClippedContent(page, "secret revealed");

      await page.getByRole("button", { name: "Delete" }).first().click();
      await expect(
        page.getByRole("dialog", { name: "Delete secret" }),
      ).toBeVisible();
      await expectCleanLayout(page, "delete secret modal", bothEnds);

      api.fail.deleteSecret = {
        message: `secret is referenced by an active agent binding. ${LONG_SENTENCE}`,
      };
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Delete" })
        .click();
      await expect(
        page.getByText(/referenced by an active agent/),
      ).toBeVisible();
      await expectCleanLayout(page, "delete secret failed", bothEnds);
    });

    test("secrets: agent principal sees a read-only page", async ({ page }) => {
      await installWorkspaceApi(page, {
        self: {
          agent_id: "agent-hermes",
          actor_id: "actor-hermes",
          username: "m4-hermes",
          principal_kind: "agent",
          auth_method: "public_key",
        },
        secrets: SECRETS,
      });
      await page.goto(`${ROOT}/secrets`);
      await expect(
        page.getByRole("heading", { name: "Secrets" }),
      ).toBeVisible();
      await expect(page.getByText("No secrets configured.")).toBeVisible();
      await expectCleanLayout(page, "secrets as agent", bothEnds);
    });

    test("audit: loading, rows, filters, empty and failure", async ({
      page,
    }) => {
      const api = await installWorkspaceApi(page, {
        events: EVENTS,
        eventsPageInfo: { has_more: true, next_cursor: "cursor-2" },
      });
      api.hold.events = deferred();
      await page.goto(`${ROOT}/events`);
      await expect(page.getByRole("heading", { name: "Audit" })).toBeVisible();
      await expectCleanLayout(page, "audit loading");

      api.hold.events.resolve();
      api.hold = {};
      await expect(page.getByText(/Card created:/)).toBeVisible();
      await expectCleanLayout(page, "audit rows", bothEnds);
      await expectNoClippedContent(page, "audit rows");

      await page.getByRole("button", { name: /^Filter/ }).click();
      await expect(page.getByTestId("events-filter-panel")).toBeVisible();
      await expectCleanLayout(page, "audit filters open", bothEnds);

      await page.getByPlaceholder("Type", { exact: true }).fill(LONG_TOKEN);
      api.events = [];
      api.eventsPageInfo = { has_more: false, next_cursor: "" };
      await page.getByRole("button", { name: "Apply" }).click();
      await expect(
        page.getByText("No events match these filters."),
      ).toBeVisible();
      await expectCleanLayout(page, "audit empty", bothEnds);

      api.fail.events = {
        message: `event index unavailable. ${LONG_SENTENCE}${LONG_TOKEN}`,
      };
      await page.getByRole("button", { name: "Clear filters" }).first().click();
      await expect(page.getByText(/event index unavailable/)).toBeVisible();
      await expectCleanLayout(page, "audit failed", bothEnds);
    });

    test("integrations: loading, coverage, source errors and capabilities", async ({
      page,
    }) => {
      const api = await installWorkspaceApi(page, {
        work: WORK,
        workCursor: "cursor-2",
        capabilities: {
          refresh_executor_configured: false,
          observation_submission: true,
          readers: [`reader-${LONG_TOKEN}`],
        },
      });
      api.hold.work = deferred();
      await page.goto(`${ROOT}/integrations`);
      await expect(
        page.getByRole("heading", { name: "Integrations" }),
      ).toBeVisible();
      await expectCleanLayout(page, "integrations loading");

      api.hold.work.resolve();
      api.hold = {};
      await expect(page.getByText(/Vendor sample delivery/)).toBeVisible();
      await expectCleanLayout(page, "integrations populated", bothEnds);
      await expectNoClippedContent(page, "integrations populated");

      await page
        .getByRole("group")
        .filter({ hasText: "Details" })
        .first()
        .locator("summary")
        .click();
      await expectCleanLayout(page, "source error details open", bothEnds);

      await page
        .getByRole("group")
        .filter({ hasText: "Reported capabilities" })
        .locator("summary")
        .click();
      await expectCleanLayout(page, "capabilities open", bothEnds);

      api.fail.work = {
        message: `coverage projection unavailable. ${LONG_SENTENCE}${LONG_TOKEN}`,
      };
      await page.getByRole("button", { name: "Reload health" }).click();
      await expect(
        page.getByText(/coverage projection unavailable/i),
      ).toBeVisible();
      await expectCleanLayout(page, "integrations failed", bothEnds);
    });

    test("integrations: no external sources", async ({ page }) => {
      await installWorkspaceApi(page, {
        work: [
          workRecord({
            id: "internal",
            ref: "card:internal",
            source: { authority: "nexus", native_status: "Ready" },
          }),
        ],
      });
      await page.goto(`${ROOT}/integrations`);
      await expect(page.getByText("No external sources yet")).toBeVisible();
      await expectCleanLayout(page, "integrations empty", bothEnds);
    });

    test("more hub and the settings redirect", async ({ page }) => {
      await installWorkspaceApi(page);
      await page.goto(`${ROOT}/more`);
      await expect(
        page.getByRole("link", { name: /Access/ }).first(),
      ).toBeVisible();
      await expectCleanLayout(page, "more hub", bothEnds);

      await page.goto(`${ROOT}/settings`);
      await expect(page).toHaveURL(/\/more$/);
      await expectCleanLayout(page, "settings redirect", bothEnds);
    });
  });
}
