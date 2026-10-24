import { expect, test } from "@playwright/test";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };
import { visualReportExample } from "../../src/lib/fixtures/visualReportExample.js";
import { setup } from "./helpers/durable-discussion.js";

/**
 * Dashboard pins are a whole-set replacement saved through the outbox: on
 * the device before any send, retried with the original key, kept across a
 * reload, and an older edit never lands over a newer one.
 */
const WATCH = "/o/local/w/local/watch";
const PINS = "workspace.dashboard.pins.set";
const report = (id) => ({
  id,
  ref: `document:${id}`,
  segment: id,
  title: `Dashboard ${id}`,
  updated_at: "2026-10-13T00:00:00Z",
  revision_ref: `document_revision:${id}-1`,
  report: visualReportExample,
});
const json = (route, value, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(value),
  });

/** `pins` is core's GET answer (null: the read fails); `send` answers PUTs. */
async function prepare(context, { pins, send }) {
  await setup(context, () => {});
  await context.route(
    (url) => url.pathname === "/brief",
    (route) =>
      json(route, { ...brief, generated_at: new Date().toISOString() }),
  );
  await context.route(
    (url) => url.pathname === "/workspace/dashboard/reports",
    (route) =>
      json(route, {
        status: "ok",
        pinned_ref: "document:alpha",
        reports: ["alpha", "beta", "gamma"].map(report),
        has_more: false,
      }),
  );
  await context.route(
    (url) => url.pathname === "/workspace/dashboard/pins",
    (route) =>
      route.request().method() === "PUT"
        ? send(route, route.request().postDataJSON())
        : pins()
          ? json(route, pins())
          : json(route, { error: { message: "Private pins detail" } }, 503),
  );
}

// Watch compiles and reads several projections before the first frame; a
// loaded machine needs longer than the default for that first render only.
const FIRST_RENDER = { timeout: 60_000 };
test.describe.configure({ timeout: 120_000 });

const tab = (page, id) => page.locator(`[data-watch-tab="dashboard:${id}"]`);
const dashboard = (page) => page.locator("[data-watch-dashboard]");

async function pin(page, id) {
  await page.locator("[data-overview-report-picker]").click();
  await page.locator(`[data-overview-report-choice="${id}"]`).click();
  await expect(page).toHaveURL(new RegExp(`dashboard=${id}(?:&|$)`));
  await dashboard(page).getByRole("button", { name: "Pin as tab" }).click();
  await expect(
    dashboard(page).getByRole("button", { name: "Unpin tab" }),
  ).toBeVisible();
}

/** Every saved pin edit on this device, as the outbox persisted it. */
function savedPins(page) {
  return page.evaluate(async (operation) => {
    const scope = (await import("/src/lib/readerScope.js")).readerScopeKey();
    const store = await import("/src/lib/localWorkStore.js");
    return (await store.listLocalWork(scope, "mutation"))
      .map((record) => record.value)
      .filter((row) => row?.operation === operation)
      .sort((a, b) => a.createdAt - b.createdAt)
      .map((row) => ({
        state: row.state,
        refs: row.request.body.document_refs,
        key: row.request.body.request_key,
      }));
  }, PINS);
}

const offline = (page) =>
  page.evaluate(() =>
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => false,
    }),
  );

test("the workspace default shows as a tab but is not the reader's pin", async ({
  page,
  context,
}) => {
  const attempts = [];
  await prepare(context, {
    pins: () => ({
      document_refs: ["document:alpha"],
      source: "workspace_default",
    }),
    send: (route, body) => {
      attempts.push(body);
      return json(route, {
        document_refs: body.document_refs,
        source: "personal",
      });
    },
  });
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(tab(page, "alpha")).toHaveAttribute(
    "aria-selected",
    "true",
    FIRST_RENDER,
  );
  await expect(tab(page, "alpha").getByLabel("Pinned")).toHaveCount(0);
  // Nothing to unpin: the default is the workspace's, not the reader's.
  await expect(
    dashboard(page).getByRole("button", { name: "Unpin tab" }),
  ).toHaveCount(0);
  await pin(page, "beta");
  await expect.poll(() => attempts.length).toBe(1);
  // Pinning saves only the reader's own refs, never the default.
  expect(attempts[0].document_refs).toEqual(["document:beta"]);
  await expect(tab(page, "beta").getByLabel("Pinned")).toHaveCount(1);
  await expect(tab(page, "alpha")).toHaveCount(0);
});

test("a pin is saved before any send, survives reload and retries its key", async ({
  page,
  context,
}) => {
  let healthy = false;
  const attempts = [];
  await prepare(context, {
    pins: () => ({ document_refs: [], source: "personal" }),
    send: (route, body) => {
      attempts.push(body);
      return healthy
        ? json(route, { document_refs: body.document_refs, source: "personal" })
        : json(route, { error: { message: "Private pin diagnostics" } }, 503);
    },
  });
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await offline(page);
  await pin(page, "beta");
  await expect
    .poll(() => savedPins(page))
    .toEqual([
      { state: "pending", refs: ["document:beta"], key: expect.any(String) },
    ]);
  expect(attempts).toHaveLength(0);
  const [{ key }] = await savedPins(page);

  // Back online after a reload: core still has no pins, the outbox does.
  await page.reload();
  await expect(tab(page, "beta").getByLabel("Pinned")).toHaveCount(
    1,
    FIRST_RENDER,
  );
  await expect.poll(() => attempts.length).toBeGreaterThan(0);
  await expect(
    page.getByText("Private pin diagnostics", { exact: true }),
  ).toHaveCount(0);
  await expect(tab(page, "beta").getByLabel("Pinned")).toHaveCount(1);

  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect.poll(() => savedPins(page)).toEqual([]);
  await expect(tab(page, "beta").getByLabel("Pinned")).toHaveCount(1);
  expect(attempts.length).toBeGreaterThan(1);
  for (const body of attempts)
    expect(body).toEqual({
      request_key: key,
      document_refs: ["document:beta"],
    });
});

test("a newer pin edit supersedes the older pending one", async ({
  page,
  context,
}) => {
  const attempts = [];
  await prepare(context, {
    pins: () => ({ document_refs: [], source: "personal" }),
    send: (route, body) => {
      attempts.push(body);
      return json(route, {
        document_refs: body.document_refs,
        source: "personal",
      });
    },
  });
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await offline(page);
  await pin(page, "beta");
  await pin(page, "gamma");
  await expect
    .poll(async () =>
      (await savedPins(page)).map(({ state, refs }) => [state, refs]),
    )
    .toEqual([
      ["superseded", ["document:beta"]],
      ["pending", ["document:beta", "document:gamma"]],
    ]);
  expect(attempts).toHaveLength(0);
  await page.evaluate(() => {
    delete navigator.onLine;
    window.dispatchEvent(new Event("online"));
    window.dispatchEvent(new Event("focus"));
  });
  await expect
    .poll(async () =>
      (await savedPins(page)).filter((r) => r.state !== "superseded"),
    )
    .toEqual([]);
  expect(attempts.map((body) => body.document_refs)).toEqual([
    ["document:beta", "document:gamma"],
  ]);
});

test("an older edit that fails after being superseded is never resent", async ({
  page,
  context,
}) => {
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  let server = [];
  const attempts = [];
  await prepare(context, {
    pins: () => ({ document_refs: server, source: "personal" }),
    send: async (route, body) => {
      attempts.push(body);
      if (attempts.length === 1) {
        // The first edit is in flight when the reader makes a second one,
        // and then its connection drops.
        await gate;
        return route.abort("timedout");
      }
      server = body.document_refs;
      return json(route, { document_refs: server, source: "personal" });
    },
  });
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await pin(page, "beta");
  await expect.poll(() => attempts.length).toBe(1);
  await pin(page, "gamma");
  await expect
    .poll(async () =>
      (await savedPins(page)).map(({ state, refs }) => [state, refs]),
    )
    .toContainEqual(["superseded", ["document:beta"]]);
  release();
  // The newer set lands, and the failed older one stays retired instead of
  // being rescheduled to replay over it.
  await expect.poll(() => server).toEqual(["document:beta", "document:gamma"]);
  await expect
    .poll(async () =>
      (await savedPins(page)).filter((r) => r.state !== "superseded"),
    )
    .toEqual([]);
  expect(attempts.map((body) => body.document_refs)).toEqual([
    ["document:beta"],
    ["document:beta", "document:gamma"],
  ]);
  expect(server).toEqual(["document:beta", "document:gamma"]);
  await expect(tab(page, "gamma")).toBeVisible();
  await expect(tab(page, "beta")).toBeVisible();
});

test("pins wait for a successful read before any whole-set edit", async ({
  page,
  context,
}) => {
  let readable = false;
  let healthy = false;
  const attempts = [];
  await prepare(context, {
    pins: () =>
      readable
        ? { document_refs: ["document:alpha"], source: "personal" }
        : null,
    send: (route, body) => {
      attempts.push(body);
      return healthy
        ? json(route, { document_refs: body.document_refs, source: "personal" })
        : json(route, { error: { message: "Unavailable" } }, 503);
    },
  });
  // The read fails: alpha is pinned in core, but this page cannot know it.
  await page.goto(`${WATCH}?tab=dashboard&dashboard=beta`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await expect(page.getByText(/pinning is paused/)).toBeVisible();
  await expect(page.getByText("Private pins detail")).toHaveCount(0);
  const pinButton = dashboard(page).getByRole("button", { name: "Pin as tab" });
  await expect(pinButton).toBeDisabled();
  await pinButton.click({ force: true });
  await page.locator("[data-overview-report-picker]").click();
  await expect(page.getByRole("menuitem", { name: "Pin as tab" })).toHaveCount(
    0,
  );
  await page.keyboard.press("Escape");
  expect(attempts).toHaveLength(0);
  expect(await savedPins(page)).toEqual([]);

  // Once a read lands, the edit keeps what core already had.
  readable = true;
  await page.locator("[data-watch-pins-retry]").click();
  await expect(tab(page, "alpha").getByLabel("Pinned")).toHaveCount(1);
  await expect(pinButton).toBeEnabled();
  await pinButton.click();
  await expect.poll(() => attempts.length).toBeGreaterThan(0);
  expect(attempts[0].document_refs).toEqual([
    "document:alpha",
    "document:beta",
  ]);

  // A saved edit still on its way is known local intent, even when the
  // next read fails.
  readable = false;
  await page.reload();
  await expect(tab(page, "beta").getByLabel("Pinned")).toHaveCount(
    1,
    FIRST_RENDER,
  );
  await expect(page.getByText(/pinning is paused/)).toHaveCount(0);
  await expect(
    dashboard(page).getByRole("button", { name: "Unpin tab" }),
  ).toBeEnabled();
  healthy = true;
});

test("an online pin is on the device before its request goes out", async ({
  page,
  context,
}) => {
  const atSend = [];
  await prepare(context, {
    pins: () => ({ document_refs: [], source: "personal" }),
    send: async (route, body) => {
      // Read the device's outbox while core is still answering.
      atSend.push({ body, saved: await savedPins(page) });
      return json(route, {
        document_refs: body.document_refs,
        source: "personal",
      });
    },
  });
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await pin(page, "beta");
  await expect.poll(() => atSend.length).toBe(1);
  expect(atSend[0].saved).toEqual([
    expect.objectContaining({
      refs: ["document:beta"],
      key: atSend[0].body.request_key,
    }),
  ]);
  await expect.poll(() => savedPins(page)).toEqual([]);
});

test("a pin that cannot be saved on the device is never sent", async ({
  page,
  context,
}) => {
  const attempts = [];
  await prepare(context, {
    pins: () => ({ document_refs: [], source: "personal" }),
    send: (route, body) => {
      attempts.push(body);
      return json(route, {
        document_refs: body.document_refs,
        source: "personal",
      });
    },
  });
  await context.addInitScript(() =>
    Object.defineProperty(window, "indexedDB", { value: undefined }),
  );
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await page.evaluate(() => {
    Storage.prototype.setItem = () => {
      throw new Error("Storage denied");
    };
  });
  await page.locator("[data-overview-report-picker]").click();
  await page.locator('[data-overview-report-choice="beta"]').click();
  await dashboard(page).getByRole("button", { name: "Pin as tab" }).click();
  await expect(page.locator("[data-watch-pin-error]")).toBeVisible();
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(tab(page, "beta").getByLabel("Pinned")).toHaveCount(0);
  expect(attempts).toHaveLength(0);
});
