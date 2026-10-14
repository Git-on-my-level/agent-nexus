import { expect, test } from "@playwright/test";
import { setup } from "./helpers/durable-discussion.js";

const thread = "thread-durable-comment";
const href = `/o/local/w/local/threads/${thread}?tab=timeline`;
const stamp = "2026-10-14T00:00:00Z";
const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
async function prepare(context, send, replies = false) {
  await setup(context, (route) => json(route, { events: [] }));
  const events = ["parent", "second"].map((id, index) => ({
    id,
    handle: `${id}-handle`,
    ref: `event:${id}-handle`,
    type: "message_posted",
    actor_id: "actor-comment",
    thread_id: thread,
    refs: [
      `thread:${thread}`,
      ...(replies && index ? ["event:parent-handle"] : []),
    ],
    summary: `Message ${id}`,
    payload: { text: `Message ${id}` },
    ts: `2026-10-13T00:0${index}:00Z`,
    provenance: { sources: ["inferred"] },
  }));
  const writes = [];
  await context.route(`**/${thread}/timeline`, (route) =>
    json(route, { events }),
  );
  await context.route(
    /\/events\/(parent|second)(?:\/(archive|unarchive|trash|restore))?$/,
    (route) => {
      const [, id, action] = new URL(route.request().url()).pathname.match(
        /\/events\/([^/]+)(?:\/([^/]+))?$/,
      );
      const event = events.find((row) => row.id === id);
      if (!action) return json(route, { event });
      const body = route.request().postDataJSON();
      writes.push({ id, action, body });
      return send(route, { id, action, body, event, events });
    },
  );
  return { writes, events };
}
async function archive(page, ids = ["parent"]) {
  if (ids.length === 1)
    await page
      .locator(`#event-${ids[0]}`)
      .getByRole("button", { name: "Archive", exact: true })
      .click();
  else {
    await page.getByRole("button", { name: "Select", exact: true }).click();
    for (const id of ids)
      await page
        .locator(`#event-${id}`)
        .getByRole("button", { name: "Select event", exact: true })
        .click();
    await page.getByRole("button", { name: "Archive", exact: true }).click();
  }
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Archive", exact: true })
    .click();
}

test("lost acknowledgement preserves the exact request through reload and Messages tab", async ({
  page,
  context,
}, info) => {
  let healthy = false;
  const receipts = new Map();
  const { writes } = await prepare(context, (route, { id, body, event }) => {
    if (!receipts.has(body.request_key))
      receipts.set(body.request_key, {
        event: { ...event, archived_at: stamp },
        affected_event_ids: [id],
      });
    return healthy
      ? json(route, receipts.get(body.request_key))
      : json(route, {}, 524);
  });
  await page.goto(href);
  await archive(page);
  await expect(page.locator('[data-mutation-state="retrying"]')).toBeVisible();
  await page.screenshot({
    path: info.outputPath("event-retrying.png"),
    fullPage: true,
  });
  await page.reload();
  await page.getByRole("tab", { name: "Messages", exact: true }).click();
  await expect(page.locator("[data-event-lifecycle-work]")).toContainText(
    "Message parent",
  );
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(receipts.size).toBe(1);
  expect(writes.length).toBeGreaterThan(1);
  for (const write of writes) expect(write).toEqual(writes[0]);
  expect(writes[0].body.if_lifecycle).toEqual({
    archived_at: null,
    trashed_at: null,
  });
  expect(writes[0].body.actor_id).toBe("actor-comment");
});

test("bulk progress survives reload and never resends the confirmed first item", async ({
  page,
  context,
}, info) => {
  let healthy = false;
  let first;
  const { writes } = await prepare(context, (route, { id, event }) => {
    first ||= id;
    if (id !== first && !healthy) return json(route, {}, 524);
    event.archived_at = stamp;
    return json(route, { event, affected_event_ids: [id] });
  });
  await page.goto(href);
  await archive(page, ["parent", "second"]);
  await expect(page.locator('[data-mutation-state="retrying"]')).toContainText(
    "1 of 2",
  );
  await page.screenshot({
    path: info.outputPath("event-bulk-partial.png"),
    fullPage: true,
  });
  await page.reload();
  await expect(page.locator('[data-mutation-state="retrying"]')).toContainText(
    "1 of 2",
  );
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(writes.filter((write) => write.id === first)).toHaveLength(1);
  const second = writes.filter((write) => write.id !== first);
  expect(second.length).toBeGreaterThan(1);
  for (const write of second) expect(write).toEqual(second[0]);
});

test("a parent receipt confirms its selected reply without a stale second send", async ({
  page,
  context,
}) => {
  const { writes } = await prepare(
    context,
    (route, { event, events }) => {
      for (const row of events) row.archived_at = stamp;
      return json(route, { event, affected_event_ids: ["parent", "second"] });
    },
    true,
  );
  await page.goto(href);
  await archive(page, ["parent", "second"]);
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(writes).toHaveLength(1);
  expect(writes[0].id).toBe("parent");
});

test("conflict keeps the original base until explicit edit and latest version", async ({
  page,
  context,
}, info) => {
  let healthy = false;
  const { writes, events } = await prepare(
    context,
    (route, { event, body }) => {
      if (!healthy)
        return json(route, { error: { message: "Private diagnostic" } }, 409);
      expect(body.if_lifecycle.archived_at).toBe(stamp);
      return json(route, { event, affected_event_ids: [] });
    },
  );
  await page.goto(href);
  await archive(page);
  await expect(page.locator('[data-mutation-state="attention"]')).toBeVisible();
  await page.screenshot({
    path: info.outputPath("event-conflict.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "Edit request", exact: true }).click();
  events[0].archived_at = stamp;
  await page
    .getByRole("button", { name: "Use latest version", exact: true })
    .click();
  healthy = true;
  await page
    .getByRole("button", { name: "Send saved request", exact: true })
    .click();
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(writes).toHaveLength(2);
  expect(writes[0].body.if_lifecycle.archived_at).toBeNull();
  expect(writes[1].body.request_key).not.toBe(writes[0].body.request_key);
});

test("denied local persistence keeps the selected request visible and sends nothing", async ({
  page,
  context,
}) => {
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
  const { writes } = await prepare(context, (route) => json(route, {}));
  await page.goto(href);
  await archive(page);
  await expect(page.locator("[data-event-lifecycle-work]")).toContainText(
    "Message parent",
  );
  await expect(page.locator("[data-event-lifecycle-work]")).toContainText(
    /copy/i,
  );
  expect(writes).toHaveLength(0);
});

test("offline bulk request survives closing the tab before any send", async ({
  context,
}) => {
  const { writes } = await prepare(context, (route, { event, id }) =>
    json(route, {
      event: { ...event, archived_at: stamp },
      affected_event_ids: [id],
    }),
  );
  const page = await context.newPage();
  await page.goto(href);
  await expect(
    page.getByRole("button", { name: "Select", exact: true }),
  ).toBeVisible();
  await context.setOffline(true);
  await archive(page, ["parent", "second"]);
  await expect(page.locator('[data-mutation-state="pending"]')).toBeVisible();
  expect(writes).toHaveLength(0);
  await page.close();
  await context.setOffline(false);
  const recovered = await context.newPage();
  await recovered.goto(href);
  await expect(recovered.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(writes).toHaveLength(2);
});

test("partial definitive failure edits only unfinished items and retains the complete reason", async ({
  page,
  context,
}) => {
  let healthy = false;
  const { writes } = await prepare(
    context,
    (route, { id, event }) => {
      if (id === "second" && !healthy) return json(route, {}, 403);
      return json(route, {
        event: { ...event, trashed_at: stamp },
        affected_event_ids: [id],
      });
    },
    true,
  );
  await page.goto(href);
  await page.getByRole("button", { name: "Select", exact: true }).click();
  for (const id of ["parent", "second"])
    await page
      .locator(`#event-${id}`)
      .getByRole("button", { name: "Select event", exact: true })
      .click();
  await page
    .getByRole("button", { name: "Move to trash", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Trash", exact: true })
    .click();
  await expect(page.locator('[data-mutation-state="attention"]')).toContainText(
    "1 of 2",
  );
  await page.getByRole("button", { name: "Edit request", exact: true }).click();
  await page
    .getByLabel("Event action reason")
    .fill("Complete authored reason after a partial result");
  await page.reload();
  await expect(page.getByLabel("Event action reason")).toHaveValue(
    "Complete authored reason after a partial result",
  );
  await expect(page.locator("[data-event-lifecycle-work]")).toContainText(
    "1 earlier events already confirmed",
  );
  healthy = true;
  await page
    .getByRole("button", { name: "Send saved request", exact: true })
    .click();
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(writes.filter((write) => write.id === "parent")).toHaveLength(1);
  expect(writes.at(-1).body.reason).toBe(
    "Complete authored reason after a partial result",
  );
  expect(writes.at(-1).body.request_key).not.toBe(writes[1].body.request_key);
});

test("principal replacement clears saved event requests in both tabs and fences a late response", async ({
  page,
  context,
}) => {
  let finish;
  const gate = new Promise((resolve) => {
    finish = resolve;
  });
  const { writes } = await prepare(context, async (route) => {
    await gate;
    return route.abort("timedout");
  });
  await page.goto(href);
  await archive(page);
  await expect.poll(() => writes.length).toBe(1);
  const other = await context.newPage();
  await other.goto(href);
  await expect(other.locator("[data-event-lifecycle-work]")).toContainText(
    "Message parent",
  );
  await page.evaluate(async () => {
    const auth = await import("/src/lib/authSession.js");
    auth.authenticatedAgent.set({
      agent_id: "replacement-owner",
      actor_id: "replacement-actor",
    });
  });
  await expect(other.locator("[data-event-lifecycle-work]")).not.toContainText(
    "Message parent",
  );
  finish();
  await expect(other.locator("[data-unsent-indicator]")).toHaveCount(0);
  expect(writes).toHaveLength(1);
});
