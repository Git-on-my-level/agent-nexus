import { expect, test } from "@playwright/test";
import { setup } from "./helpers/durable-discussion.js";
const root = "/o/local/w/local";
const json = (route, body, status = 200) =>
  route.request().isNavigationRequest()
    ? route.continue()
    : route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
const stamp = "2026-10-13T00:00:00Z";
const doc = {
  id: "author-doc",
  title: "Author document",
  summary: "Original summary",
  state: "active",
  thread_id: "thread-durable-comment",
  head_revision_id: "head-one",
  head_revision_number: 1,
  updated_at: stamp,
};
const revision = {
  revision_id: "head-one",
  revision_number: 1,
  content: "Original content",
  content_type: "text",
  created_at: stamp,
};

test("confirmation preserves a newer empty description draft", async ({
  page,
  context,
}) => {
  await prepare(context);
  let finish;
  const gate = new Promise((resolve) => {
    finish = resolve;
  });
  await context.route("**/docs/author-doc", async (route) => {
    if (route.request().method() !== "PATCH")
      return json(route, { document: doc, revision });
    const body = route.request().postDataJSON();
    await gate;
    return json(route, {
      document: {
        ...doc,
        summary: body.patch.summary,
        updated_at: "2026-10-13T01:00:00Z",
      },
      revision,
    });
  });
  await page.goto(`${root}/docs/author-doc/edit`);
  const input = page.getByLabel("Short description", { exact: true });
  await input.fill("Submitted description");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.locator('[data-mutation-state="sending"]')).toBeVisible();
  await input.fill("");
  finish();
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  await expect(input).toHaveValue("");
});
async function prepare(context) {
  await setup(context, (route) =>
    json(route, { event: { id: "unused" } }, 201),
  );
  await context.route("**/boards", (route) => json(route, { boards: [] }));
  await context.route("**/docs/author-doc", (route) =>
    json(route, { document: doc, revision }),
  );
  await context.route("**/docs/author-doc/revisions", (route) =>
    json(route, { revisions: [revision] }),
  );
}
test("task fields restore and uncertain creation replays the original body across reload", async ({
  page,
  context,
}, testInfo) => {
  await prepare(context);
  let healthy = false;
  const attempts = [],
    committed = new Map();
  await context.route("**/work", (route) => {
    if (route.request().method() !== "POST") return json(route, { work: [] });
    const body = route.request().postDataJSON();
    attempts.push(body);
    if (!committed.has(body.request_key))
      committed.set(body.request_key, {
        work: { ref: "card:authored", title: body.title },
      });
    return healthy
      ? json(route, committed.get(body.request_key), 201)
      : json(route, { error: { message: "Private timeout diagnostics" } }, 524);
  });
  await context.route("**/work/card%3Aauthored", (route) =>
    json(route, {
      work: {
        ref: "card:authored",
        title: "Durable outcome",
        phase: "backlog",
        source: { authority: "nexus" },
      },
    }),
  );
  await page.goto(`${root}/initiatives/new`);
  await page.getByLabel("Outcome", { exact: true }).fill("Durable outcome");
  await page.getByLabel("Context", { exact: true }).fill("Keep the scope note");
  await page
    .getByLabel("Acceptance criteria", { exact: true })
    .fill("Evidence reviewed\nOwner confirms");
  await page.getByLabel("Accountable owner", { exact: true }).fill("Reader");
  await page.reload();
  await expect(page.getByLabel("Context", { exact: true })).toHaveValue(
    "Keep the scope note",
  );
  await expect(
    page.getByLabel("Acceptance criteria", { exact: true }),
  ).toHaveValue("Evidence reviewed\nOwner confirms");
  await page.getByRole("button", { name: "Create task", exact: true }).click();
  await expect(page.locator('[data-mutation-state="retrying"]')).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("author-retrying.png"),
    fullPage: true,
  });
  await page.reload();
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect.poll(() => attempts.length).toBeGreaterThan(1);
  await expect(page).toHaveURL(/\/initiatives\/card%3Aauthored/);
  expect(committed.size).toBe(1);
  for (const body of attempts) expect(body).toEqual(attempts[0]);
});
test("document creation draft survives closing its original offline tab", async ({
  page,
  context,
}, testInfo) => {
  await prepare(context);
  const posts = [];
  await context.route("**/docs", (route) => {
    if (route.request().method() !== "POST")
      return json(route, { documents: [doc] });
    posts.push(route.request().postDataJSON());
    return json(route, { document: doc, revision }, 201);
  });
  await page.goto(`${root}/docs`);
  await page.getByRole("button", { name: "New doc", exact: true }).click();
  await page
    .getByPlaceholder("Document title", { exact: true })
    .fill("My offline document");
  await page
    .getByRole("textbox", { name: "Head content (Markdown) *" })
    .fill("Keep this complete Markdown body.");
  await page.evaluate(() =>
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => false,
    }),
  );
  await page.getByRole("button", { name: "Create doc", exact: true }).click();
  await expect(page.locator('[data-mutation-state="pending"]')).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("author-pending.png"),
    fullPage: true,
  });
  await page.close();
  const other = await context.newPage();
  await other.goto(`${root}/docs`);
  await expect.poll(() => posts.length).toBe(1);
  expect(posts[0].content).toBe("Keep this complete Markdown body.");
});
test("document settings conflict retains the note and uses a fresh version only on explicit rebase", async ({
  page,
  context,
}, testInfo) => {
  await prepare(context);
  let latest = stamp;
  const writes = [];
  await context.route("**/docs/author-doc", (route) => {
    if (route.request().method() !== "PATCH")
      return json(route, {
        document: { ...doc, updated_at: latest },
        revision,
      });
    const body = route.request().postDataJSON();
    writes.push(body);
    if (body.if_updated_at !== latest)
      return json(
        route,
        { error: { message: "Private conflict details" } },
        409,
      );
    return json(route, {
      document: { ...doc, summary: body.patch.summary, updated_at: latest },
      revision,
    });
  });
  await page.goto(`${root}/docs/author-doc/edit`);
  await page
    .getByLabel("Short description", { exact: true })
    .fill("My authored description");
  await page.reload();
  await expect(
    page.getByLabel("Short description", { exact: true }),
  ).toHaveValue("My authored description");
  latest = "2026-10-13T01:00:00Z";
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.locator('[data-mutation-state="attention"]')).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("author-attention.png"),
    fullPage: true,
  });
  await page.reload();
  await expect(
    page.getByLabel("Short description", { exact: true }),
  ).toHaveValue("My authored description");
  await page
    .getByRole("button", { name: "Use latest version", exact: true })
    .click();
  await expect(page.locator('[data-mutation-state="attention"]')).toHaveCount(
    0,
  );
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  await expect(
    page.getByLabel("Short description", { exact: true }),
  ).toHaveValue("My authored description");
  expect(writes).toHaveLength(2);
  expect(writes[0].if_updated_at).toBe(stamp);
  expect(writes[1].if_updated_at).toBe(latest);
  expect(writes[1].request_key).not.toBe(writes[0].request_key);
  expect(writes[1].patch.summary).toBe("My authored description");
  await page.screenshot({
    path: testInfo.outputPath("author-sent.png"),
    fullPage: true,
  });
});
test("revision draft restores its original base after reload and an uncertain send", async ({
  page,
  context,
}) => {
  await prepare(context);
  let healthy = false;
  const attempts = [];
  await context.route("**/docs/author-doc/revisions", (route) => {
    if (route.request().method() !== "POST")
      return json(route, { revisions: [revision] });
    const body = route.request().postDataJSON();
    attempts.push(body);
    return healthy
      ? json(
          route,
          {
            document: { ...doc, head_revision_id: "head-two" },
            revision: {
              ...revision,
              revision_id: "head-two",
              content: body.content,
            },
          },
          201,
        )
      : json(route, { error: { message: "Private busy diagnostics" } }, 503);
  });
  await page.goto(`${root}/docs/author-doc`);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Content (Markdown)", exact: true })
    .fill("My new revision body");
  await page.reload();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "Content (Markdown)", exact: true }),
  ).toHaveValue("My new revision body");
  await page
    .getByRole("button", { name: "Save revision", exact: true })
    .click();
  await expect(page.locator('[data-mutation-state="retrying"]')).toBeVisible();
  await page.reload();
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect.poll(() => attempts.length).toBeGreaterThan(1);
  for (const body of attempts) expect(body).toEqual(attempts[0]);
  expect(attempts[0].if_base_revision).toBe("head-one");
});
