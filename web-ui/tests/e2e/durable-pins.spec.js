import { expect, test } from "@playwright/test";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };
import { visualReportExample } from "../../src/lib/fixtures/visualReportExample.js";
import { setup } from "./helpers/durable-discussion.js";

/**
 * A pin edit is one saved operation (add or remove one dashboard), on the
 * device before any send. The outbox applies it to a fresh read when it
 * sends, so no edit drops a pin it never saw: not another tab's, not a
 * confirmed one that an older read missed, not one core had while this page
 * could not read it.
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
const doc = (id) => `document:${id}`;

/**
 * A small stand-in for core's pins: the reader's personal set (the workspace
 * default `alpha` when empty) and keyed replay, which answers 409 when a key
 * comes back with a different body.
 *
 * `read(request)` may return "fail" (503), a Promise to hold the read, or
 * nothing to answer now. `write(body)` may return "fail" (503), "abort", a
 * Promise to hold the write, or nothing to apply it.
 */
async function prepare(context, { pins = [], read, write } = {}) {
  const core = { pins: [...pins], reads: 0, writes: [], keys: new Map() };
  const answer = () =>
    core.pins.length
      ? { document_refs: core.pins, source: "personal" }
      : { document_refs: [doc("alpha")], source: "workspace_default" };
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
        pinned_ref: doc("alpha"),
        reports: ["alpha", "beta", "gamma", "delta"].map(report),
        has_more: false,
      }),
  );
  await context.route(
    (url) => url.pathname === "/workspace/dashboard/pins",
    async (route) => {
      if (route.request().method() !== "PUT") {
        core.reads += 1;
        // What core held when the read arrived, even if it answers late.
        const value = answer();
        const hold = await read?.(core);
        if (hold === "fail")
          return json(
            route,
            { error: { message: "Private pins detail" } },
            503,
          );
        return json(route, value);
      }
      const body = route.request().postDataJSON();
      core.writes.push(body);
      const hold = await write?.(body, core);
      if (hold === "fail")
        return json(route, { error: { message: "Private pin detail" } }, 503);
      if (hold === "abort") return route.abort("timedout");
      const sent = JSON.stringify(body.document_refs);
      const earlier = core.keys.get(body.request_key);
      if (earlier && earlier.sent !== sent)
        return json(route, { error: { message: "Key reused" } }, 409);
      if (!earlier) {
        core.pins = [...body.document_refs];
        core.keys.set(body.request_key, { sent, answer: answer() });
      }
      return json(route, core.keys.get(body.request_key).answer);
    },
  );
  return core;
}

// Watch compiles and reads several projections before the first frame; a
// loaded machine needs longer than the default for that first render only.
const FIRST_RENDER = { timeout: 60_000 };
test.describe.configure({ timeout: 120_000 });

const tab = (page, id) => page.locator(`[data-watch-tab="dashboard:${id}"]`);
const pinned = (page, id) => tab(page, id).getByLabel("Pinned");
const dashboard = (page) => page.locator("[data-watch-dashboard]");

async function open(page, id) {
  await page.locator("[data-overview-report-picker]").click();
  await page.locator(`[data-overview-report-choice="${id}"]`).click();
  await expect(page).toHaveURL(new RegExp(`dashboard=${id}(?:&|$)`));
}
async function pin(page, id) {
  await open(page, id);
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
        op: row.request.op,
        ref: row.request.ref,
        key: row.request.request_key,
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
const online = (page) =>
  page.evaluate(() => {
    delete navigator.onLine;
    window.dispatchEvent(new Event("online"));
    window.dispatchEvent(new Event("focus"));
  });

test("the workspace default shows as a tab but is not the reader's pin", async ({
  page,
  context,
}) => {
  const core = await prepare(context);
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(tab(page, "alpha")).toHaveAttribute(
    "aria-selected",
    "true",
    FIRST_RENDER,
  );
  await expect(pinned(page, "alpha")).toHaveCount(0);
  // Nothing to unpin: the default is the workspace's, not the reader's.
  await expect(
    dashboard(page).getByRole("button", { name: "Unpin tab" }),
  ).toHaveCount(0);
  await pin(page, "beta");
  await expect.poll(() => core.pins).toEqual([doc("beta")]);
  // Pinning saves only the reader's own refs, never the default.
  expect(core.writes.map((body) => body.document_refs)).toEqual([
    [doc("beta")],
  ]);
  await expect(pinned(page, "beta")).toHaveCount(1);
  await expect(tab(page, "alpha")).toHaveCount(0);
});

test("an online pin is on the device before its request goes out", async ({
  page,
  context,
}) => {
  const atSend = [];
  const core = await prepare(context, {
    // Read the device's outbox while core is still answering.
    write: async (body) => {
      atSend.push({ body, saved: await savedPins(page) });
    },
  });
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await pin(page, "beta");
  await expect.poll(() => atSend.length).toBe(1);
  expect(atSend[0].saved).toEqual([
    expect.objectContaining({
      op: "add",
      ref: doc("beta"),
      key: atSend[0].body.request_key,
    }),
  ]);
  await expect.poll(() => savedPins(page)).toEqual([]);
  expect(core.pins).toEqual([doc("beta")]);
});

test("a pin that cannot be saved on the device is never sent", async ({
  page,
  context,
}) => {
  const core = await prepare(context);
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
  await open(page, "beta");
  await dashboard(page).getByRole("button", { name: "Pin as tab" }).click();
  await expect(page.locator("[data-watch-pin-error]")).toBeVisible();
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(pinned(page, "beta")).toHaveCount(0);
  expect(core.writes).toHaveLength(0);
});

test("a saved pin survives reload and retries with its original key", async ({
  page,
  context,
}) => {
  let healthy = false;
  const core = await prepare(context, {
    write: () => (healthy ? undefined : "fail"),
  });
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await offline(page);
  await pin(page, "beta");
  await expect
    .poll(() => savedPins(page))
    .toEqual([
      {
        state: "pending",
        op: "add",
        ref: doc("beta"),
        key: expect.any(String),
      },
    ]);
  expect(core.writes).toHaveLength(0);
  const [{ key }] = await savedPins(page);

  // Back online after a reload: core still has no pins, the outbox does.
  await page.reload();
  await expect(pinned(page, "beta")).toHaveCount(1, FIRST_RENDER);
  await expect.poll(() => core.writes.length).toBeGreaterThan(0);
  await expect(page.getByText("Private pin detail")).toHaveCount(0);
  await expect(pinned(page, "beta")).toHaveCount(1);

  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect.poll(() => savedPins(page)).toEqual([]);
  await expect(pinned(page, "beta")).toHaveCount(1);
  expect(core.writes.length).toBeGreaterThan(1);
  for (const body of core.writes)
    expect(body).toEqual({ request_key: key, document_refs: [doc("beta")] });
});

test("a read that started before a pin confirmed cannot roll it back", async ({
  page,
  context,
}) => {
  let releaseRead;
  let holdNextRead = false;
  const core = await prepare(context, {
    pins: [doc("alpha")],
    read: () => {
      if (!holdNextRead) return;
      holdNextRead = false;
      return new Promise((resolve) => {
        releaseRead = resolve;
      });
    },
  });
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(pinned(page, "alpha")).toHaveCount(1, FIRST_RENDER);
  await offline(page);
  await pin(page, "beta");
  await expect.poll(async () => (await savedPins(page)).length).toBe(1);

  // Reload still offline: the page's own read goes out (and is held, with
  // core's set as of now) while the saved edit waits for the network.
  await page.addInitScript(() =>
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => false,
    }),
  );
  holdNextRead = true;
  await page.reload();
  await expect.poll(() => typeof releaseRead, FIRST_RENDER).toBe("function");
  await expect(pinned(page, "beta")).toHaveCount(1, FIRST_RENDER);
  await online(page);
  await expect.poll(() => core.pins).toEqual([doc("alpha"), doc("beta")]);
  await expect.poll(() => savedPins(page)).toEqual([]);

  // The held read now answers with the set from before beta landed.
  const late = page.waitForResponse(
    (response) =>
      response.url().includes("/workspace/dashboard/pins") &&
      response.request().method() === "GET",
  );
  releaseRead();
  await late;
  await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 200)));
  await expect(pinned(page, "beta")).toHaveCount(1);
  await pin(page, "gamma");
  await expect
    .poll(() => core.pins)
    .toEqual([doc("alpha"), doc("beta"), doc("gamma")]);
  await expect(pinned(page, "beta")).toHaveCount(1);
});

test("two tabs pinning different dashboards both keep their pins", async ({
  page,
  context,
}) => {
  const core = await prepare(context, { pins: [doc("alpha")] });
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(pinned(page, "alpha")).toHaveCount(1, FIRST_RENDER);
  const other = await context.newPage();
  await other.goto(`${WATCH}?tab=dashboard`);
  await expect(pinned(other, "alpha")).toHaveCount(1, FIRST_RENDER);

  // The second tab is paused while the first pins beta and core confirms it,
  // so it never sees that edit pending: it has only read [alpha].
  const debug = await context.newCDPSession(other);
  await debug.send("Debugger.enable");
  await debug.send("Debugger.pause");
  await pin(page, "beta");
  await expect.poll(() => core.pins).toEqual([doc("alpha"), doc("beta")]);
  await expect.poll(() => savedPins(page)).toEqual([]);
  await debug.send("Debugger.resume");
  await debug.send("Debugger.disable");
  await pin(other, "gamma");
  await expect
    .poll(() => core.pins)
    .toEqual([doc("alpha"), doc("beta"), doc("gamma")]);
  // Both tabs end on core's set, without a reload.
  for (const tabPage of [page, other])
    for (const id of ["alpha", "beta", "gamma"])
      await expect(pinned(tabPage, id)).toHaveCount(1);
});

test("a pin made while the pins cannot be read keeps core's other pins", async ({
  page,
  context,
}) => {
  const core = await prepare(context, {
    pins: [doc("alpha")],
    // The page's first read fails; the outbox's own read later succeeds.
    read: (state) => (state.reads === 1 ? "fail" : undefined),
  });
  await page.goto(`${WATCH}?tab=dashboard&dashboard=beta`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await expect(page.getByText(/Couldn't read your pinned tabs/)).toBeVisible();
  await expect(page.getByText("Private pins detail")).toHaveCount(0);
  await dashboard(page).getByRole("button", { name: "Pin as tab" }).click();
  await expect.poll(() => core.pins).toEqual([doc("alpha"), doc("beta")]);
  await expect(pinned(page, "alpha")).toHaveCount(1);
  await expect(pinned(page, "beta")).toHaveCount(1);
  await expect(page.getByText(/Couldn't read your pinned tabs/)).toHaveCount(0);
});

test("a pin past the limit is refused, before saving or when sending", async ({
  page,
  context,
}) => {
  const twelve = Array.from({ length: 12 }, (_, i) => doc(`full${i}`));
  let readable = true;
  const core = await prepare(context, {
    pins: twelve,
    read: () => (readable ? undefined : "fail"),
  });
  // Known to be full: nothing is saved or sent.
  await page.goto(`${WATCH}?tab=dashboard&dashboard=beta`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await expect.poll(() => core.reads).toBeGreaterThan(0);
  await dashboard(page).getByRole("button", { name: "Pin as tab" }).click();
  await expect(page.locator("[data-watch-pin-error]")).toContainText(
    "You can pin up to 12 dashboards",
  );
  expect(await savedPins(page)).toEqual([]);

  // Unread here, so the page cannot tell; the send-time read can, and the
  // edit stops for the reader instead of replacing the set.
  readable = false;
  await page.reload();
  await expect(page.getByText(/Couldn't read your pinned tabs/)).toBeVisible(
    FIRST_RENDER,
  );
  readable = true;
  await dashboard(page).getByRole("button", { name: "Pin as tab" }).click();
  await expect
    .poll(async () => (await savedPins(page)).map((row) => row.state))
    .toEqual(["attention"]);
  await expect(page.locator("[data-watch-pin-error]")).toContainText(
    "You already have 12 pinned dashboards",
  );
  expect(core.writes).toHaveLength(0);
  expect(core.pins).toEqual(twelve);
});

test("pinning then unpinning one dashboard reaches core in that order", async ({
  page,
  context,
}) => {
  let failFirst = true;
  const core = await prepare(context, {
    // The add's first send fails, so the remove becomes due first.
    write: (body) => {
      if (failFirst && body.document_refs.includes(doc("beta"))) {
        failFirst = false;
        return "fail";
      }
    },
  });
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(dashboard(page)).toBeVisible(FIRST_RENDER);
  await offline(page);
  await pin(page, "beta");
  await dashboard(page).getByRole("button", { name: "Unpin tab" }).click();
  await expect
    .poll(async () => (await savedPins(page)).map((row) => row.op))
    .toEqual(["add", "remove"]);
  await online(page);
  await expect.poll(() => savedPins(page), { timeout: 20_000 }).toEqual([]);
  expect(core.pins).toEqual([]);
  expect(core.writes.map((body) => body.document_refs)).toEqual([
    [doc("beta")],
    [doc("beta")],
    [],
  ]);
  await expect(pinned(page, "beta")).toHaveCount(0);
});
