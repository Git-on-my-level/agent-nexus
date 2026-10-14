import { expect, test } from "@playwright/test";
import { setup } from "./helpers/durable-discussion.js";

const THREAD = "thread-durable-comment";
const URL = `/o/local/w/local/threads/${THREAD}?tab=messages`;
const TEXT = "Keep this reply through an uncertain send.";
const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

async function open(page) {
  await page.goto(URL);
  await expect(page.locator("#message-text")).toBeVisible();
}
async function post(page) {
  await page.locator("#message-text").fill(TEXT);
  await page.getByRole("button", { name: "Send", exact: true }).click();
}

test("reply draft and its reply target restore in a new tab", async ({
  context,
  page,
}) => {
  await setup(context, (route) =>
    json(route, { event: { id: "unused" } }, 201),
  );
  await open(page);
  await page
    .locator("#message-original-message")
    .getByRole("button", { name: "Reply", exact: true })
    .click({ force: true });
  await page.locator("#message-text").fill(TEXT);
  await page.reload();
  await expect(page.locator("#message-text")).toHaveValue(TEXT);
  await expect(page.getByText("Replying to", { exact: true })).toBeVisible();
  const other = await context.newPage();
  await open(other);
  await expect(other.locator("#message-text")).toHaveValue(TEXT);
  await expect(other.getByText("Replying to", { exact: true })).toBeVisible();
  await other.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.locator("#message-text")).toHaveValue("");
  await expect(page.getByText("Replying to", { exact: true })).toHaveCount(0);
});

for (const failure of ["timeout", "524"])
  test(`${failure} retains original request through reload and retries once across tabs`, async ({
    context,
    page,
  }) => {
    const attempts = [];
    const committed = new Map();
    let healthy = false;
    await setup(context, (route, events) => {
      const body = route.request().postDataJSON();
      attempts.push(body);
      if (!healthy)
        return failure === "timeout"
          ? route.abort("timedout")
          : route.fulfill({
              status: 524,
              contentType: "text/html",
              body: "Cloudflare raw private error",
            });
      if (!committed.has(body.request_key)) {
        const event = {
          ...body.event,
          id: "confirmed-comment",
          actor_id: body.actor_id,
          ts: new Date().toISOString(),
        };
        committed.set(body.request_key, { event });
        events.push(event);
      }
      return json(route, committed.get(body.request_key), 201);
    });
    await open(page);
    await post(page);
    await expect(
      page.locator('.msg-composer [data-mutation-state="retrying"]'),
    ).toBeVisible();
    await expect(page.locator(".msg-composer details[open]")).toHaveCount(0);
    await expect(page.locator(".msg-composer pre")).not.toBeVisible();
    const first = attempts[0];
    await page.reload();
    await expect(
      page.locator('.msg-composer [aria-label="Saved message"]'),
    ).toHaveValue(TEXT);
    const other = await context.newPage();
    await open(other);
    healthy = true;
    await expect.poll(() => committed.size).toBe(1);
    for (const attempt of attempts) expect(attempt).toEqual(first);
    await expect(page.locator("[data-unsent-indicator]")).toHaveCount(0);
  });

test("offline submission survives tab closure and resumes", async ({
  context,
  page,
}) => {
  const attempts = [];
  await setup(context, (route) => {
    attempts.push(route.request().postDataJSON());
    return json(route, { event: { id: "offline-confirmed" } }, 201);
  });
  await open(page);
  await context.setOffline(true);
  await post(page);
  await expect(
    page.locator('.msg-composer [data-mutation-state="pending"]'),
  ).toBeVisible();
  const original = await page.evaluate(async () => {
    const { listLocalWork } = await import("/src/lib/localWorkStore.js");
    return (
      await listLocalWork(
        '["local","local","reader-comment","actor-comment"]',
        "mutation",
      )
    )[0].value.request;
  });
  const next = await context.newPage();
  await page.close();
  await context.setOffline(false);
  await open(next);
  await expect.poll(() => attempts.length).toBe(1);
  expect(attempts[0].request_key).toBe(original.request_key);
  expect(attempts[0].event).toEqual(original.event);
});

test("reload during lost confirmation replays the committed message without a second post", async ({
  context,
  page,
}) => {
  const attempts = [];
  const committed = new Map();
  let release;
  const interrupted = new Promise((resolve) => {
    release = resolve;
  });
  await setup(context, async (route, events) => {
    const body = route.request().postDataJSON();
    attempts.push(body);
    if (!committed.has(body.request_key)) {
      const event = {
        ...body.event,
        id: "lost-confirmation",
        actor_id: body.actor_id,
        ts: new Date().toISOString(),
      };
      committed.set(body.request_key, { event });
      events.push(event);
      await interrupted;
      await route.abort("failed").catch(() => {});
      return;
    }
    await json(route, committed.get(body.request_key), 201);
  });
  await open(page);
  await post(page);
  await expect.poll(() => committed.size).toBe(1);
  const original = attempts[0];
  await page.reload();
  release();
  const other = await context.newPage();
  await open(other);
  await expect.poll(() => attempts.length >= 2).toBe(true);
  await expect(page.locator("[data-unsent-indicator]")).toHaveCount(0);
  expect(committed.size).toBe(1);
  for (const attempt of attempts) expect(attempt).toEqual(original);
});

test("storage failure prevents posting and retains text for correction", async ({
  context,
  page,
}) => {
  let posts = 0;
  await setup(context, (route) => {
    posts++;
    return json(route, { event: { id: "should-not-post" } }, 201);
  });
  await open(page);
  await page.evaluate(() => {
    Storage.prototype.setItem = () => {
      throw new Error("quota");
    };
    IDBDatabase.prototype.transaction = () => {
      throw new Error("device store unavailable");
    };
  });
  await page.locator("#message-text").fill(TEXT);
  await page.locator("#message-text").press("Control+Enter");
  await expect(
    page.locator('.msg-composer [data-mutation-state="attention"]'),
  ).toBeVisible();
  await expect(page.locator("#message-text")).toHaveValue(TEXT);
  await expect(
    page.locator('.msg-composer [aria-label="Saved message"]'),
  ).toHaveValue(TEXT);
  expect(posts).toBe(0);
});

test("confirmation preserves newer composer work in another tab", async ({
  context,
  page,
}) => {
  let release;
  let started = false;
  const hold = new Promise((resolve) => {
    release = resolve;
  });
  await setup(context, async (route) => {
    started = true;
    await hold;
    await json(route, { event: { id: "confirmed-original" } }, 201);
  });
  await open(page);
  await page.locator("#message-text").fill(TEXT);
  const other = await context.newPage();
  await open(other);
  await expect(other.locator("#message-text")).toHaveValue(TEXT);
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect.poll(() => started).toBe(true);
  const newer = "New work written while the earlier reply is sending.";
  await other.locator("#message-text").fill(newer);
  release();
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  await expect(other.locator("#message-text")).toHaveValue(newer);
  await other.reload();
  await expect(other.locator("#message-text")).toHaveValue(newer);
});

test("validation keeps copy and edit actions, with all four states reviewable", async ({
  context,
  page,
}, testInfo) => {
  let status = 409;
  await setup(context, (route) =>
    status === 201
      ? json(route, { event: { id: "edited-confirmed" } }, 201)
      : json(
          route,
          { error: { message: "Internal validation detail" } },
          status,
        ),
  );
  await open(page);
  await context.setOffline(true);
  await post(page);
  await expect(
    page.locator('.msg-composer [data-mutation-state="pending"]'),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("discussion-pending.png"),
    fullPage: true,
  });
  status = 524;
  await context.setOffline(false);
  await expect(
    page.locator('.msg-composer [data-mutation-state="retrying"]'),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("discussion-retrying.png"),
    fullPage: true,
  });
  status = 409;
  await expect(
    page.locator('.msg-composer [data-mutation-state="attention"]'),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("discussion-attention.png"),
    fullPage: true,
  });
  await page
    .locator(".msg-composer")
    .getByRole("button", { name: "Edit message", exact: true })
    .click();
  await expect(page.locator("#message-text")).toHaveValue(TEXT);
  status = 201;
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(
    page.locator('.msg-composer [data-mutation-state="sent"]'),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("discussion-sent.png"),
    fullPage: true,
  });
});
