import { expect, test } from "@playwright/test";
import { setup } from "./helpers/durable-discussion.js";
const url = "/o/local/w/local/inbox?mailbox=watching";
const stamp = "2026-10-14T00:00:00Z";
const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
async function prepare(context, send) {
  await setup(context, (route) => json(route, { events: [] }));
  const group = {
    group_ref: "topic:control-update",
    display_name: "Control update",
    unread_count: 1,
    newest_event: { id: "seen-event", ts: stamp, summary: "Seen update" },
    events: [],
  };
  const work = {
    id: "control-card",
    ref: "card:control-card",
    title: "Stale control task",
    version: 7,
    phase: "blocked",
    source: { authority: "nexus" },
    work_summary: {
      status: { state: "blocked", label: "Blocked" },
      age: 127 * 86400,
      last_movement_at: "2025-01-01T00:00:00Z",
    },
  };
  const decision = {
    id: "control-decision",
    instruction: "Control delivery",
    work_ref: work.ref,
    status: "answered",
    scope: "work.annotate",
    action_id: "control-action",
    revision: 3,
  };
  const action = {
    id: "control-action",
    decision_id: decision.id,
    work_ref: work.ref,
    status: "pending_delivery",
    revision: 1,
    deliverable: true,
  };
  await context.route(/\/home\/unread(?:\?.*)?$/, (route) =>
    json(route, { groups: [group] }),
  );
  await context.route(/\/work(?:\?.*)?$/, (route) =>
    json(route, { work: [work] }),
  );
  await context.route(/\/work\/(?:card%3A)?control-card(?:\?.*)?$/, (route) =>
    json(route, { work }),
  );
  await context.route(/\/pm\/decisions(?:\?.*)?$/, (route) =>
    json(route, { items: [decision] }),
  );
  await context.route(/\/pm\/actions(?:\?.*)?$/, (route) =>
    json(route, { items: [action] }),
  );
  await context.route(/\/pm\/decisions\/control-decision$/, (route) =>
    json(route, decision),
  );
  await context.route(/\/pm\/actions\/control-action$/, (route) =>
    json(route, action),
  );
  const writes = [];
  await context.route(
    /\/(?:home\/read|cards\/control-card\/archive|pm\/(?:decisions\/control-decision\/dispatch|actions\/control-action\/(?:acknowledge|reconcile)))$/,
    (route) => {
      const path = new URL(route.request().url()).pathname;
      const body = route.request().postDataJSON();
      writes.push({ path, body });
      return send(route, { path, body, group, work, decision, action, writes });
    },
  );
  await context.route(
    (url) => url.pathname.startsWith("/o/"),
    (route) => route.continue(),
  );
  return { writes, group, work, decision, action };
}
async function records(page) {
  return page.evaluate(async () => {
    const store = await import("/src/lib/localWorkStore.js");
    const scope = await import("/src/lib/readerScope.js");
    return store.listLocalWork(scope.readerScopeKey(), "mutation");
  });
}
async function start(page, kind) {
  await page.goto(
    kind === "read"
      ? url
      : "/o/local/w/local/inbox?item=decision:control-decision&mailbox=watching",
  );
  if (kind === "read") {
    await page
      .locator("[data-inbox-row]")
      .filter({ hasText: "Control update" })
      .click();
    await page.getByRole("button", { name: "Mark read", exact: true }).click();
  } else if (kind === "archive") {
    await page.goto("/o/local/w/local/inbox?mailbox=needs-you");
    await page.getByRole("button", { name: "Stale (1)", exact: true }).click();
    await page
      .getByRole("button", { name: "Archive Stale control task", exact: true })
      .click();
  } else {
    await page
      .getByRole("button", {
        name:
          kind === "acknowledge"
            ? "Acknowledge and clear from Needs you"
            : kind === "reconcile"
              ? "Check source receipt"
              : "Deliver approved instruction",
        exact: true,
      })
      .click();
  }
}
for (const kind of [
  "read",
  "archive",
  "dispatch",
  "acknowledge",
  "reconcile",
]) {
  test(`${kind} recovers an uncertain send with the original key and frozen base`, async ({
    page,
    context,
  }) => {
    const fixture = await prepare(context, (route, { writes, work, action }) =>
      writes.length === 1
        ? json(route, { error: { code: "upstream_timeout" } }, 524)
        : json(
            route,
            kind === "read"
              ? { ok: true }
              : kind === "archive"
                ? { card: work }
                : action,
          ),
    );
    if (kind === "acknowledge") fixture.action.status = "failed";
    if (kind === "reconcile") {
      fixture.action.status = "unknown";
      fixture.action.attempts = [{ sent_at: stamp, status: "unknown" }];
    }
    await start(page, kind);
    await expect(
      page.locator(
        '[data-inbox-control-work] [data-mutation-state="retrying"]',
      ),
    ).toBeVisible();
    expect(fixture.writes[0].body.request_key).toBeTruthy();
    await expect.poll(async () => (await records(page)).length).toBe(1);
    const original = fixture.writes[0].body;
    // New canonical state must not be folded into an uncertain request.
    fixture.group.newest_event = {
      id: "unseen-event",
      ts: "2026-10-14T01:00:00Z",
    };
    fixture.work.version = 8;
    fixture.action.revision = 2;
    await page.reload();
    await expect.poll(() => fixture.writes.length).toBe(2);
    expect(fixture.writes[1].body).toEqual(original);
    await expect.poll(async () => (await records(page)).length).toBe(0);
    await expect(
      page.locator('[data-inbox-control-work] [data-mutation-state="sent"]'),
    ).toBeVisible();
  });
}
test("a conflict keeps the saved control; only explicit latest creates a new base and key", async ({
  page,
  context,
}, testInfo) => {
  const fixture = await prepare(context, (route, { writes, work }) =>
    writes.length === 1
      ? json(
          route,
          { error: { code: "conflict", message: "Work changed" } },
          409,
        )
      : json(route, { card: work }),
  );
  await start(page, "archive");
  await expect(
    page.locator('[data-inbox-control-work] [data-mutation-state="attention"]'),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("durable-inbox-control-conflict.png"),
    fullPage: true,
  });
  fixture.work.version = 8;
  await page.getByRole("button", { name: "Edit request", exact: true }).click();
  await page
    .getByRole("button", { name: "Use latest version", exact: true })
    .click();
  await expect
    .poll(async () =>
      page.evaluate(async () => {
        const store = await import("/src/lib/localWorkStore.js");
        const scope = await import("/src/lib/readerScope.js");
        const draft = await store.loadLocalWork(
          scope.readerScopeKey(),
          "draft",
          "inbox:controls",
        );
        return draft?.items?.[0]?.body?.if_version;
      }),
    )
    .toBe(8);
  await page
    .getByRole("button", { name: "Send saved request", exact: true })
    .click();
  await expect.poll(() => fixture.writes.length).toBe(2);
  expect(fixture.writes[1].body.request_key).not.toBe(
    fixture.writes[0].body.request_key,
  );
  expect(fixture.writes[1].body.if_version).toBe(8);
});
test("bulk controls preserve partial confirmations through tab closure and recovery", async ({
  page,
  context,
}) => {
  const fixture = await prepare(context, (route, { path, writes, work }) =>
    path.endsWith("/read")
      ? json(route, { ok: true })
      : writes.length === 2
        ? json(route, { error: { code: "upstream_timeout" } }, 524)
        : json(route, { card: work }),
  );
  await page.goto(url);
  await expect(
    page.locator("[data-inbox-row]").filter({ hasText: "Control update" }),
  ).toBeVisible();
  await page.evaluate(
    async ({ stamp }) => {
      const { enqueueInboxControls } =
        await import("/src/lib/mutationOutbox.js");
      await enqueueInboxControls({
        target: "inbox:controls",
        href: "/o/local/w/local/inbox",
        draft: {
          items: [
            {
              action: "read",
              id: "topic:control-update",
              title: "Control update",
              body: {
                group_ref: "topic:control-update",
                expected_newest_event_cursor: { id: "seen-event", ts: stamp },
              },
            },
            {
              action: "archive",
              id: "control-card",
              title: "Stale control task",
              ref: "card:control-card",
              body: {
                if_version: 7,
                if_lifecycle: { archived_at: null, trashed_at: null },
                if_latest_observation_id: "",
              },
            },
          ],
        },
      });
    },
    { stamp },
  );
  await expect(
    page.locator('[data-inbox-control-work] [data-mutation-state="retrying"]'),
  ).toContainText("1 of 2 actions confirmed");
  const original = fixture.writes[1].body;
  await page.close();
  const recovered = await context.newPage();
  await recovered.goto(url);
  await expect.poll(() => fixture.writes.length).toBe(3);
  expect(
    fixture.writes.filter((write) => write.path.endsWith("/read")),
  ).toHaveLength(1);
  expect(fixture.writes[2].body).toEqual(original);
  await expect.poll(async () => (await records(recovered)).length).toBe(0);
});
test("offline control requests survive reload and reconnect", async ({
  page,
  context,
}) => {
  const fixture = await prepare(context, (route) => json(route, { ok: true }));
  await page.goto(url);
  await page.evaluate(() =>
    Object.defineProperty(navigator, "onLine", {
      get: () => false,
      configurable: true,
    }),
  );
  await page
    .locator("[data-inbox-row]")
    .filter({ hasText: "Control update" })
    .click();
  await page.getByRole("button", { name: "Mark read", exact: true }).click();
  await expect.poll(async () => (await records(page)).length).toBe(1);
  expect(fixture.writes).toHaveLength(0);
  await page.reload();
  await expect.poll(() => fixture.writes.length).toBe(1);
});

test("denied storage retains the visible inbox intent and sends nothing", async ({
  page,
  context,
}) => {
  const fixture = await prepare(context, (route) => json(route, { ok: true }));
  await context.addInitScript(() => {
    localStorage.setItem("anx_ui_actor_id:local", "actor-comment");
    localStorage.setItem("workspaceTourSeen.local", "1");
    Storage.prototype.setItem = () => {
      throw new Error("Storage denied");
    };
    indexedDB.open = () => {
      throw new Error("Storage denied");
    };
  });
  await start(page, "read");
  await expect(
    page.getByRole("textbox", { name: "Saved inbox request", exact: true }),
  ).toHaveValue(/Control update/);
  await expect(page.locator("[data-inbox-control-work]")).toContainText(
    "Local saving is unavailable",
  );
  expect(fixture.writes).toHaveLength(0);
});
