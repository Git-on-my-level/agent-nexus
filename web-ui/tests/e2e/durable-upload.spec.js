import { expect, test } from "@playwright/test";
import { setup } from "./helpers/durable-discussion.js";
import { setup as setupInbox, ITEM } from "./helpers/durable-inbox.js";

const url = "/o/local/w/local/threads/thread-durable-comment?tab=messages";
const file = {
  name: "evidence.txt",
  mimeType: "text/plain",
  buffer: Buffer.from("Keep these original file bytes."),
};

test("principal replacement erases private file bytes and fences late upload receipts", async ({
  page,
  context,
}) => {
  let finish;
  const gate = new Promise((resolve) => {
    finish = resolve;
  });
  let attempts = 0;
  await setup(context, (route) =>
    json(route, { event: { id: "unused" } }, 201),
  );
  await context.route("**/artifacts/attachments", async (route) => {
    attempts++;
    await gate;
    await json(route, { artifact }, 201);
  });
  await open(page);
  await attach(page);
  await expect.poll(() => attempts).toBe(1);
  await page.evaluate(async () => {
    const auth = await import("/src/lib/authSession.js");
    auth.authenticatedAgent.set({
      agent_id: "replacement-owner",
      actor_id: "replacement-actor",
    });
  });
  await expect(page.locator("[data-unsent-indicator]")).toHaveCount(0);
  finish();
  await expect
    .poll(async () =>
      page.evaluate(async () => {
        const local = await import("/src/lib/localWorkStore.js");
        const old = JSON.stringify([
          "local",
          "local",
          "reader-comment",
          "actor-comment",
        ]);
        return (
          (await local.listLocalWork(old, "mutation")).length +
          (await local.listLocalWork(old, "attachment")).length
        );
      }),
    )
    .toBe(0);
  await expect(
    page.getByRole("button", { name: `Remove artifact:${artifact.id}` }),
  ).toHaveCount(0);
});
const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
async function open(page) {
  await page.goto(url);
  await expect(page.locator("#message-text")).toBeVisible();
}
async function attach(page) {
  await page.locator('.msg-composer input[type="file"]').setInputFiles(file);
}
const status = (page, value) =>
  page.locator(`.msg-composer [data-mutation-state="${value}"]`);
const artifact = {
  id: "uploaded-evidence",
  kind: "attachment",
  original_filename: file.name,
  mime_type: file.mimeType,
  refs: ["thread:thread-durable-comment"],
};

test("Inbox upload survives reload and its ref is sent with the answer", async ({
  page,
  context,
}) => {
  let healthy = false;
  const responses = [];
  await setupInbox(context, (route) => {
    responses.push(route.request().postDataJSON());
    return json(route, { event: { id: "response-upload" } }, 201);
  });
  await context.route("**/artifacts/attachments", (route) =>
    healthy
      ? json(route, { artifact }, 201)
      : json(route, { error: { message: "Private upload failure" } }, 503),
  );
  await page.goto(`/o/local/w/local/inbox/${ITEM.id}`);
  await page
    .getByRole("textbox", { name: "Your response", exact: true })
    .fill("The evidence is attached.");
  await page.getByLabel("Attach file", { exact: true }).setInputFiles(file);
  await expect(page.locator('[data-mutation-state="retrying"]')).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Send response", exact: true }),
  ).toBeDisabled();
  await page.reload();
  await expect(
    page.getByRole("textbox", { name: "Your response", exact: true }),
  ).toHaveValue("The evidence is attached.");
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(
    page.getByRole("button", { name: `Remove artifact:${artifact.id}` }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Send response", exact: true })
    .click();
  await page.evaluate(async () => {
    const queue = await import("/src/lib/inboxResponseQueue.js");
    await queue.flushInboxResponse();
  });
  await expect.poll(() => responses.length).toBe(1);
  expect(responses[0].related_refs).toContain(`artifact:${artifact.id}`);
});

test("offline file selection survives tab close and is included in the recovered post", async ({
  page,
  context,
}, testInfo) => {
  const uploads = [],
    posts = [];
  await setup(context, (route) => {
    posts.push(route.request().postDataJSON());
    return json(route, { event: { id: "posted" } }, 201);
  });
  await context.route("**/artifacts/attachments", (route) => {
    uploads.push(route.request().postData());
    return json(route, { artifact }, 201);
  });
  await open(page);
  await page.locator("#message-text").fill("Read the attached evidence.");
  await page.evaluate(() =>
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => false,
    }),
  );
  await attach(page);
  await expect(status(page, "pending")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Send", exact: true }),
  ).toBeDisabled();
  expect(uploads).toHaveLength(0);
  await page.screenshot({
    path: testInfo.outputPath("upload-pending.png"),
    fullPage: true,
  });
  await page.close();
  const other = await context.newPage();
  await open(other);
  await expect(
    other.getByRole("button", { name: "Send", exact: true }),
  ).toBeEnabled();
  expect(uploads).toHaveLength(1);
  expect(uploads[0]).toContain(file.buffer.toString());
  await other.getByRole("button", { name: "Send", exact: true }).click();
  await expect.poll(() => posts.length).toBe(1);
  await expect(status(other, "sent")).toBeVisible();
  expect(posts[0].event.refs).toContain(`artifact:${artifact.id}`);
  await other.reload();
  await expect(other.locator("#message-text")).toHaveValue("");
  await expect(
    other.getByRole("button", { name: `Remove artifact:${artifact.id}` }),
  ).toHaveCount(0);
});

test("storage denial blocks upload and keeps the original file downloadable", async ({
  page,
  context,
}) => {
  let uploads = 0;
  await setup(context, (route) =>
    json(route, { event: { id: "unused" } }, 201),
  );
  await context.route("**/artifacts/attachments", (route) => {
    uploads++;
    return json(route, { artifact }, 201);
  });
  await open(page);
  await page.evaluate(() => {
    Object.defineProperty(window, "indexedDB", {
      value: undefined,
      configurable: true,
    });
    const set = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key.startsWith("anx:local-work:v1:"))
        throw new DOMException("Denied", "QuotaExceededError");
      return set.call(this, key, value);
    };
  });
  await attach(page);
  await expect(status(page, "attention")).toBeVisible();
  expect(uploads).toBe(0);
  const downloadEvent = page.waitForEvent("download");
  await page.getByRole("button", { name: "Save file", exact: true }).click();
  const download = await downloadEvent;
  const chunks = [];
  for await (const chunk of await download.createReadStream())
    chunks.push(chunk);
  expect(Buffer.concat(chunks)).toEqual(file.buffer);
});

test("lost upload acknowledgement retries identical bytes and key across reload and tabs", async ({
  page,
  context,
}, testInfo) => {
  let healthy = false;
  const attempts = [];
  const committed = new Map();
  await setup(context, (route) =>
    json(route, { event: { id: "unused" } }, 201),
  );
  await context.route("**/artifacts/attachments", (route) => {
    const body = route.request().postData();
    const key = body.match(/name="request_key"\r\n\r\n([^\r]+)/)?.[1];
    attempts.push({ key, body });
    if (!committed.has(key)) committed.set(key, { artifact });
    return healthy
      ? json(route, committed.get(key), 201)
      : route.fulfill({
          status: 524,
          contentType: "text/html",
          body: "Private upstream diagnostics",
        });
  });
  await open(page);
  await attach(page);
  await expect(status(page, "retrying")).toBeVisible();
  await expect(page.locator(".msg-composer details[open]")).toHaveCount(0);
  await page.screenshot({
    path: testInfo.outputPath("upload-retrying.png"),
    fullPage: true,
  });
  await page.reload();
  await expect(status(page, "retrying")).toBeVisible();
  const other = await context.newPage();
  await open(other);
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await other.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(
    page.getByRole("button", { name: `Remove artifact:${artifact.id}` }),
  ).toBeVisible();
  await expect(
    other.getByRole("button", { name: `Remove artifact:${artifact.id}` }),
  ).toBeVisible();
  expect(committed.size).toBe(1);
  expect(attempts.length).toBeGreaterThan(1);
  for (const attempt of attempts) {
    expect(attempt.key).toBe(attempts[0].key);
    expect(attempt.body).toContain(file.buffer.toString());
    expect(attempt.body).toContain('filename="evidence.txt"');
  }
  await page.screenshot({
    path: testInfo.outputPath("upload-confirmed.png"),
    fullPage: true,
  });
});

test("definitive upload failure preserves a downloadable file and allows replacement", async ({
  page,
  context,
}, testInfo) => {
  await setup(context, (route) =>
    json(route, { event: { id: "unused" } }, 201),
  );
  await context.route("**/artifacts/attachments", (route) =>
    json(route, { error: { message: "Private validation details" } }, 409),
  );
  await open(page);
  await page.locator("#message-text").fill("My untouched note");
  await attach(page);
  await expect(status(page, "attention")).toBeVisible();
  await page.reload();
  await expect(status(page, "attention")).toBeVisible();
  await expect(page.locator("#message-text")).toHaveValue("My untouched note");
  await page.screenshot({
    path: testInfo.outputPath("upload-attention.png"),
    fullPage: true,
  });
  const downloaded = page.waitForEvent("download");
  await page.getByRole("button", { name: "Save file", exact: true }).click();
  const download = await downloaded;
  expect(download.suggestedFilename()).toBe(file.name);
  const stream = await download.createReadStream();
  const chunks = [];
  for await (const chunk of stream) chunks.push(chunk);
  expect(Buffer.concat(chunks)).toEqual(file.buffer);
  await page
    .getByRole("button", { name: "Choose replacement", exact: true })
    .click();
  await expect(status(page, "attention")).toHaveCount(0);
  await expect(page.locator("#message-text")).toHaveValue("My untouched note");
});
