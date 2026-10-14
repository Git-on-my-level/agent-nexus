import { expect, test } from "@playwright/test";
import { setup } from "./helpers/durable-discussion.js";
const root = "/o/local/w/local/docs";
const stamp = "2026-10-13T00:00:00Z";
const revision = {
  revision_id: "revision-one",
  revision_number: 1,
  content: "Lifecycle content",
  content_type: "text",
  created_at: stamp,
};
const json = (route, body, status = 200) =>
  route.request().isNavigationRequest()
    ? route.continue()
    : route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
async function prepare(context, send, state = "active") {
  await setup(context, () => {});
  const docs = ["one", "two"].map((id) => ({
    id: `lifecycle-${id}`,
    title: `Lifecycle ${id}`,
    handle: `lifecycle-${id}`,
    state,
    updated_at: stamp,
    head_revision_id: revision.revision_id,
    head_revision_number: 1,
    ...(state === "trashed" ? { trashed_at: stamp } : {}),
    ...(state === "archived" ? { archived_at: stamp } : {}),
  }));
  await context.route(/\/docs(?:\?.*)?$/, (route) =>
    json(route, { documents: docs }),
  );
  for (const doc of docs) {
    await context.route(`**/docs/${doc.id}`, (route) =>
      json(route, { document: doc, revision }),
    );
    await context.route(`**/docs/${doc.id}/revisions*`, (route) =>
      json(route, { revisions: [revision] }),
    );
  }
  await context.route(
    /\/docs\/lifecycle-(?:one|two)\/(?:archive|unarchive|trash|restore)$/,
    (route) => send(route, docs),
  );
  return docs;
}
function confirmed(route, docs) {
  const parts = new URL(route.request().url()).pathname.split("/");
  const action = parts.at(-1),
    id = parts.at(-2);
  const doc = docs.find((doc) => doc.id === id);
  doc.updated_at = "2026-10-13T01:00:00Z";
  doc.state =
    action === "archive"
      ? "archived"
      : action === "trash"
        ? "trashed"
        : "active";
  if (action === "archive") doc.archived_at = doc.updated_at;
  if (action === "unarchive") delete doc.archived_at;
  if (action === "trash") doc.trashed_at = doc.updated_at;
  if (action === "restore") delete doc.trashed_at;
  return { document: { ...doc }, revision };
}
async function bulk(page, action = "Archive") {
  await page.goto(root);
  await expect(page.getByText("Lifecycle one", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Select", exact: true }).click();
  await page.getByRole("button", { name: "Select all" }).click();
  await page.getByRole("button", { name: action, exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", {
      name: action === "Move to trash" ? "Trash" : action,
      exact: true,
    })
    .click();
}
const status = (page, state) =>
  page.locator(
    `[data-document-lifecycle-work] [data-mutation-state="${state}"]`,
  );

test("complete bulk checkpoint precedes first send; 524 recovery reuses all keys across tabs", async ({
  page,
  context,
}, info) => {
  let healthy = false;
  const attempts = [],
    committed = new Map();
  await prepare(context, (route, docs) => {
    const body = route.request().postDataJSON();
    attempts.push({ url: route.request().url(), body });
    if (!committed.has(body.request_key))
      committed.set(body.request_key, confirmed(route, docs));
    return json(
      route,
      healthy
        ? committed.get(body.request_key)
        : { error: "Cloudflare 524 PRIVATE-UPSTREAM" },
      healthy ? 200 : 524,
    );
  });
  await bulk(page);
  await expect(status(page, "retrying")).toBeVisible();
  await expect(
    page.locator("[data-document-lifecycle-work] details[open]"),
  ).toHaveCount(0);
  await page.screenshot({
    path: info.outputPath("retrying.png"),
    fullPage: true,
  });
  const request = await page.evaluate(async () => {
    const store = await import("/src/lib/localWorkStore.js");
    const rows = await store.listLocalWork(
      '["local","local","reader-comment","actor-comment"]',
      "mutation",
    );
    return rows.find((row) => row.value.operation === "docs.lifecycle").value
      .request;
  });
  expect(request.items).toHaveLength(2);
  for (const item of request.items) {
    expect(item.body.actor_id).toBe("actor-comment");
    expect(item.body.if_updated_at).toBe(stamp);
  }
  await page.reload();
  const other = await context.newPage();
  await other.goto(root);
  healthy = true;
  await expect.poll(() => committed.size).toBe(2);
  await expect(page.locator("[data-unsent-indicator]")).toHaveCount(0);
  for (const attempt of attempts)
    expect(attempt.body).toEqual(
      request.items.find((item) => attempt.url.includes(item.id)).body,
    );
  await expect(status(page, "sent")).toBeVisible();
  await page.screenshot({ path: info.outputPath("sent.png"), fullPage: true });
});

test("partial confirmation survives reload; attention edit excludes confirmed documents and explicitly refreshes the base", async ({
  page,
  context,
}, info) => {
  const attempts = [];
  let healthy = false;
  const docs = await prepare(context, (route, docs) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-2);
    attempts.push({ id, body: route.request().postDataJSON() });
    if (id.endsWith("two") && !healthy)
      return json(route, { error: { message: "PRIVATE-CONFLICT" } }, 409);
    return json(route, confirmed(route, docs));
  });
  await bulk(page, "Move to trash");
  await expect(status(page, "attention")).toBeVisible();
  await expect(status(page, "attention")).toContainText(
    "1 of 2 documents confirmed",
  );
  await page.screenshot({
    path: info.outputPath("attention.png"),
    fullPage: true,
  });
  const originalTwo = attempts.find((attempt) =>
    attempt.id.endsWith("two"),
  ).body;
  await page.reload();
  await expect(status(page, "attention")).toContainText(
    "1 of 2 documents confirmed",
  );
  await page
    .getByLabel("Document action reason")
    .fill("Newer draft before editing");
  await page.getByRole("button", { name: "Edit request", exact: true }).click();
  await expect(page.getByLabel("Document action reason")).toHaveValue(
    "Newer draft before editing",
  );
  await expect(
    page.getByText("1 earlier documents already confirmed."),
  ).toBeVisible();
  const reason = page.getByLabel("Document action reason");
  await reason.fill("Keep this authored reason");
  docs[1].updated_at = "2026-10-13T02:00:00Z";
  await page.reload();
  await expect(reason).toHaveValue("Keep this authored reason");
  await page.getByRole("button", { name: "Send saved request" }).click();
  await expect(status(page, "attention")).toBeVisible();
  expect(attempts.at(-1).body.if_updated_at).toBe(originalTwo.if_updated_at);
  await page.getByRole("button", { name: "Edit request", exact: true }).click();
  await page.getByRole("button", { name: "Use latest version" }).click();
  await expect
    .poll(async () =>
      page.evaluate(async () => {
        const s = await import("/src/lib/localWorkStore.js");
        return (
          await s.loadLocalWork(
            '["local","local","reader-comment","actor-comment"]',
            "draft",
            "documents:lifecycle",
          )
        )?.documents[0]?.updated_at;
      }),
    )
    .toBe(docs[1].updated_at);
  healthy = true;
  await page.getByRole("button", { name: "Send saved request" }).click();
  await expect(status(page, "sent")).toBeVisible();
  expect(attempts.filter((attempt) => attempt.id.endsWith("one"))).toHaveLength(
    1,
  );
  expect(attempts.at(-1).body.reason).toBe("Keep this authored reason");
  expect(attempts.at(-1).body.request_key).not.toBe(originalTwo.request_key);
});

test("offline complete batch survives tab closure", async ({
  page,
  context,
}, info) => {
  const attempts = [];
  await prepare(context, (route, docs) => {
    attempts.push(route.request().postDataJSON());
    return json(route, confirmed(route, docs));
  });
  await page.goto(root);
  await page.getByRole("button", { name: "Select", exact: true }).click();
  await page.getByRole("button", { name: "Select all" }).click();
  await page.getByRole("button", { name: "Archive", exact: true }).click();
  await context.setOffline(true);
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Archive", exact: true })
    .click();
  await expect(status(page, "pending")).toBeVisible();
  await page.screenshot({
    path: info.outputPath("pending.png"),
    fullPage: true,
  });
  const other = await context.newPage();
  await page.close();
  await context.setOffline(false);
  await other.goto(root);
  await expect(status(other, "sent")).toBeVisible();
  expect(attempts).toHaveLength(2);
});

test("detail restore timeout replays exact request and preserves newer reason in another tab", async ({
  page,
  context,
}) => {
  let healthy = false;
  const attempts = [],
    committed = new Map();
  await prepare(
    context,
    (route, docs) => {
      const body = route.request().postDataJSON();
      attempts.push(body);
      if (!committed.has(body.request_key))
        committed.set(body.request_key, confirmed(route, docs));
      return healthy
        ? json(route, committed.get(body.request_key))
        : route.abort("timedout");
    },
    "trashed",
  );
  await page.goto(`${root}/lifecycle-one`);
  await page.getByRole("button", { name: "Restore", exact: true }).click();
  await expect(status(page, "retrying")).toBeVisible();
  const other = await context.newPage();
  await other.goto(`${root}/lifecycle-one`);
  await other
    .getByLabel("Document action reason")
    .fill("Newer reason survives");
  healthy = true;
  await expect(page.locator("[data-unsent-indicator]")).toHaveCount(0);
  await expect(other.getByLabel("Document action reason")).toHaveValue(
    "Newer reason survives",
  );
  for (const body of attempts) expect(body).toEqual(attempts[0]);
  expect(committed.size).toBe(1);
  await other.reload();
  await expect(other.getByLabel("Document action reason")).toHaveValue(
    "Newer reason survives",
  );
});

test("total storage denial prevents sends and retains the complete visible intent", async ({
  page,
  context,
}) => {
  const attempts = [];
  await prepare(context, (route, docs) => {
    attempts.push(route.request().postDataJSON());
    return json(route, confirmed(route, docs));
  });
  await context.addInitScript(() =>
    Object.defineProperty(window, "indexedDB", { value: undefined }),
  );
  await page.goto(root);
  await page.evaluate(() => {
    Storage.prototype.setItem = () => {
      throw new Error("Storage denied");
    };
  });
  await page.getByRole("button", { name: "Select", exact: true }).click();
  await page.getByRole("button", { name: "Select all" }).click();
  await page
    .getByRole("button", { name: "Move to trash", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Trash", exact: true })
    .click();
  await expect(
    page.getByText("Saved document request", { exact: true }),
  ).toBeVisible();
  await page
    .getByLabel("Document action reason")
    .fill("Keep despite denied storage");
  await expect(
    page.getByRole("button", { name: "Copy request" }),
  ).toBeVisible();
  expect(attempts).toHaveLength(0);
});

test("principal replacement clears private lifecycle work across tabs and fences late sends", async ({
  page,
  context,
}) => {
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const attempts = [];
  await prepare(
    context,
    async (route) => {
      attempts.push(route.request().postDataJSON());
      await gate;
      await route.abort("timedout");
    },
    "trashed",
  );
  await page.goto(`${root}/lifecycle-one`);
  await page.getByRole("button", { name: "Restore", exact: true }).click();
  await expect.poll(() => attempts.length).toBe(1);
  const other = await context.newPage();
  await other.goto(`${root}/lifecycle-one`);
  await other
    .getByLabel("Document action reason")
    .fill("PRIVATE-LIFECYCLE-REASON");
  await page.evaluate(async () => {
    const auth = await import("/src/lib/authSession.js");
    auth.authenticatedAgent.set({
      agent_id: "replacement-owner",
      actor_id: "replacement-actor",
    });
  });
  await expect(other.getByLabel("Document action reason")).toHaveCount(0);
  release();
  await expect(other.locator("[data-unsent-indicator]")).toHaveCount(0);
  expect(attempts).toHaveLength(1);
});
