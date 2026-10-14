import { expect, test } from "@playwright/test";
import { ITEM, setup } from "./helpers/durable-inbox.js";

const TEXT = "Use Monday. Keep the second region on Thursday.";
const URL = `/o/local/w/local/inbox/${ITEM.id}`;
const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

async function flush(page) {
  await page.evaluate(async () => {
    const queue = await import("/src/lib/inboxResponseQueue.js");
    await queue.flushInboxResponse();
  });
}

async function records(page, kind = "inbox-send") {
  return page.evaluate(async (type) => {
    const store = await import("/src/lib/localWorkStore.js");
    const scope = await import("/src/lib/readerScope.js");
    return store.listLocalWork(scope.readerScopeKey(), type);
  }, kind);
}

async function begin(page) {
  await page.goto(URL);
  await expect(
    page.getByRole("textbox", { name: "Your response", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("textbox", { name: "Your response", exact: true })
    .fill(TEXT);
}

test("drafts restore after reload, navigation and in another tab", async ({
  page,
  context,
}) => {
  await setup(context, (route) =>
    json(route, { event: { id: "response-one" } }, 201),
  );
  await begin(page);
  await page.reload();
  await expect(
    page.getByRole("textbox", { name: "Your response", exact: true }),
  ).toHaveValue(TEXT);
  await page.goto("/o/local/w/local/inbox");
  await page.getByTestId(`inbox-row-${ITEM.id}`).click();
  await expect(
    page.getByRole("textbox", { name: "Reply", exact: true }),
  ).toHaveValue(TEXT);
  const revised = `${TEXT} Include verification.`;
  await page.getByRole("textbox", { name: "Reply", exact: true }).fill(revised);
  await page.goto(URL);
  await expect(
    page.getByRole("textbox", { name: "Your response", exact: true }),
  ).toHaveValue(revised);
  const other = await context.newPage();
  await other.goto(URL);
  await expect(
    other.getByRole("textbox", { name: "Your response", exact: true }),
  ).toHaveValue(revised);
});

for (const failure of ["timeout", "524"]) {
  test(`${failure} keeps the exact text and retries the same key after reload`, async ({
    page,
    context,
  }) => {
    const bodies = [];
    await setup(context, async (route) => {
      bodies.push(route.request().postDataJSON());
      if (bodies.length === 1) {
        if (failure === "timeout") return route.abort("timedout");
        return json(route, { upstream: "cloudflare-secret-error-body" }, 524);
      }
      return json(route, { event: { id: "response-one" } }, 201);
    });
    await begin(page);
    await page
      .getByRole("button", { name: "Send response", exact: true })
      .click();
    await flush(page);
    const status = page.locator('[data-inbox-send-state="retrying"]').first();
    await expect(status).toBeVisible();
    await expect(
      status.getByRole("textbox", { name: "Saved response" }),
    ).toHaveValue(TEXT);
    await expect(status.locator("p").nth(1)).not.toContainText(
      "cloudflare-secret-error-body",
    );
    expect((await records(page))[0].value.request.response_text).toBe(TEXT);
    await page.reload();
    await expect.poll(() => bodies.length).toBe(2);
    expect(bodies[1]).toEqual(bodies[0]);
    await expect.poll(async () => (await records(page)).length).toBe(0);
  });
}

test("offline work survives closing its tab and resumes in a new tab", async ({
  page,
  context,
}) => {
  const bodies = [];
  await setup(context, (route) => {
    bodies.push(route.request().postDataJSON());
    return json(route, { event: { id: "response-one" } }, 201);
  });
  await begin(page);
  await context.setOffline(true);
  await page
    .getByRole("button", { name: "Send response", exact: true })
    .click();
  await expect(page.locator("[data-unsent-indicator]")).toContainText(
    "1 unsent",
  );
  await page.close();
  await context.setOffline(false);
  const other = await context.newPage();
  await other.goto(URL);
  await expect.poll(() => bodies.length).toBe(1);
  expect(bodies[0].response_text).toBe(TEXT);
  await expect.poll(async () => (await records(other)).length).toBe(0);
});

test("a lost confirmation and reload mid-send replay once across tabs", async ({
  page,
  context,
}) => {
  const committed = new Map();
  const bodies = [];
  let release;
  const waiting = new Promise((resolve) => (release = resolve));
  await setup(context, async (route) => {
    const body = route.request().postDataJSON();
    bodies.push(body);
    committed.set(body.idempotency_key, body.response_text);
    if (bodies.length === 1) {
      await waiting;
      try {
        await route.abort("timedout");
      } catch {
        /* Reload already closed the request. */
      }
      return;
    }
    return json(route, { event: { id: "response-one" }, replayed: true }, 201);
  });
  await begin(page);
  await page
    .getByRole("button", { name: "Send response", exact: true })
    .click();
  // Do not await the deliberately interrupted request.
  const sending = flush(page).catch(() => {});
  await expect.poll(() => bodies.length).toBe(1);
  const other = await context.newPage();
  await other.goto(URL);
  await page.reload();
  release();
  await sending;
  await expect.poll(() => bodies.length).toBe(2);
  expect(committed.size).toBe(1);
  expect(bodies[1]).toEqual(bodies[0]);
  await expect.poll(async () => (await records(other)).length).toBe(0);
  await expect(page.locator("[data-unsent-indicator]")).toHaveCount(0);
});

test("definitive failures keep editable text without automatic retry", async ({
  page,
  context,
}) => {
  let attempts = 0;
  await setup(context, (route) => {
    attempts += 1;
    return json(
      route,
      { error: { code: "conflict", message: "already resolved" } },
      409,
    );
  });
  await begin(page);
  await page
    .getByRole("button", { name: "Send response", exact: true })
    .click();
  await flush(page);
  await page.reload();
  const status = page.locator('[data-inbox-send-state="attention"]').first();
  await expect(status).toBeVisible();
  await expect(
    status.getByRole("textbox", { name: "Saved response" }),
  ).toHaveValue(TEXT);
  await expect(status.getByRole("button", { name: "Copy text" })).toBeVisible();
  await status.getByRole("button", { name: "Edit response" }).click();
  await expect(
    page.getByRole("textbox", { name: "Your response", exact: true }),
  ).toHaveValue(TEXT);
  expect(attempts).toBe(1);
});

test("IndexedDB denial uses the journal and total storage failure prevents sending", async ({
  page,
  context,
}) => {
  let attempts = 0;
  await setup(context, (route) => {
    attempts += 1;
    return json(route, { event: { id: "response-one" } }, 201);
  });
  await context.addInitScript(() => {
    Object.defineProperty(window, "indexedDB", { value: undefined });
  });
  await begin(page);
  await page.reload();
  await expect(
    page.getByRole("textbox", { name: "Your response", exact: true }),
  ).toHaveValue(TEXT);
  await page.evaluate(() => {
    Storage.prototype.setItem = () => {
      throw new Error("quota");
    };
  });
  await page
    .getByRole("textbox", { name: "Your response", exact: true })
    .fill("Keep this additional text.");
  await expect(page.locator("[data-local-save-warning]")).toBeVisible();
  await page
    .locator("[data-local-save-warning]")
    .getByRole("button", { name: "Dismiss" })
    .click();
  await page
    .getByRole("button", { name: "Send response", exact: true })
    .click();
  await flush(page);
  await expect(
    page.getByRole("textbox", { name: "Saved response" }).first(),
  ).toHaveValue("Keep this additional text.");
  expect(attempts).toBe(0);
});

test("principal switches clear other tabs and fence a late failed send", async ({
  page,
  context,
}) => {
  let attempts = 0;
  let release;
  const waiting = new Promise((resolve) => (release = resolve));
  await setup(context, async (route) => {
    attempts += 1;
    await waiting;
    await route.abort("timedout");
  });
  await begin(page);
  const other = await context.newPage();
  await other.goto(URL);
  await expect(
    other.getByRole("textbox", { name: "Your response", exact: true }),
  ).toHaveValue(TEXT);
  await page
    .getByRole("button", { name: "Send response", exact: true })
    .click();
  const flight = flush(page);
  await expect.poll(() => attempts).toBe(1);
  await page.evaluate(async () => {
    const actors = await import("/src/lib/actorSession.js");
    actors.chooseActor("actor-other-reader");
  });
  await expect(
    other.getByRole("textbox", { name: "Your response", exact: true }),
  ).toHaveValue("");
  release();
  await flight;
  expect(await records(other)).toEqual([]);
  expect(await records(other, "draft")).toEqual([]);
  await expect(other.locator("[data-unsent-indicator]")).toHaveCount(0);
});

test("all delivery states are reviewable", async ({
  page,
  context,
}, testInfo) => {
  let attempts = 0;
  await setup(context, (route) => {
    attempts += 1;
    if (attempts === 1) return json(route, { upstream: "cloudflare" }, 524);
    if (attempts === 2)
      return json(route, { error: { code: "conflict" } }, 409);
    return json(route, { event: { id: "response-one" } }, 201);
  });
  await begin(page);
  await page.clock.install();
  await page.clock.pauseAt(new Date());
  await page
    .getByRole("button", { name: "Send response", exact: true })
    .click();
  await page.clock.runFor(10);
  await expect(page.locator("[data-unsent-indicator]")).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          new Promise((resolve, reject) => {
            const opening = indexedDB.open("anx-local-work-v1", 1);
            opening.onerror = () => reject(opening.error);
            opening.onsuccess = () => {
              const db = opening.result;
              const read = db
                .transaction("work", "readonly")
                .objectStore("work")
                .getAll();
              read.onsuccess = () => {
                db.close();
                resolve(
                  read.result.filter(
                    (record) => record.kind === "inbox-send" && !record.deleted,
                  ).length,
                );
              };
              read.onerror = () => reject(read.error);
            };
          }),
      ),
    )
    .toBe(1);
  await page.screenshot({
    path: testInfo.outputPath("pending.png"),
    fullPage: true,
  });
  await flush(page);
  await page.clock.runFor(10);
  await expect(
    page.locator('[data-inbox-send-state="retrying"]').first(),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("retrying.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "Retry send" }).first().click();
  await expect(
    page.locator('[data-inbox-send-state="attention"]').first(),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("needs-attention.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "Retry send" }).first().click();
  await expect(page.locator('[data-inbox-toast="sent"]')).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("sent.png"),
    fullPage: true,
  });
});
