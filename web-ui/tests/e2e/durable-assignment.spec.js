import { expect, test } from "@playwright/test";
import { setup } from "./helpers/durable-discussion.js";

const root = "/o/local/w/local";
const stamp = "2026-10-13T00:00:00Z";
const work = {
  ref: "card:assignment",
  handle: "assignment",
  title: "Durable task",
  phase: "backlog",
  source: { authority: "nexus" },
  updated_at: stamp,
  thread_id: "thread-durable-comment",
  assignee_refs: [],
  definition_of_done: [],
};
const json = (route, value, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(value),
  });
async function prepare(context, send, current = () => work) {
  await setup(context, () => {});
  await context.route(/\/actors(?:\?.*)?$/, (route) =>
    json(route, {
      actors: [
        { id: "actor-comment", display_name: "Reader", tags: ["human"] },
        { id: "actor-bob", display_name: "Bob", tags: ["human"] },
        { id: "actor-alice", display_name: "Alice", tags: ["human"] },
      ],
    }),
  );
  await context.route("**/work/card%3Aassignment*", (route) =>
    json(route, { work: current() }),
  );
  await context.route("**/cards/card%3Aassignment/plan*", (route) =>
    json(route, { plan: null }),
  );
  await context.route("**/cards/card%3Aassignment", (route) =>
    route.request().method() === "PATCH"
      ? send(route)
      : json(route, { card: current() }),
  );
}
async function openAssignment(page) {
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("heading", { name: "Durable task", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("heading", { name: "Durable task", exact: true })
    .click();
  await page.keyboard.press("a");
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toBeVisible();
}
async function choose(page, name = "Bob") {
  await openAssignment(page);
  await page
    .getByRole("option", { name: `Assign to ${name}`, exact: true })
    .click();
  await expect(
    page.getByText("Assignment saved. Waiting for confirmation.", {
      exact: true,
    }),
  ).toBeVisible();
}

test("uncertain assignment survives reload and replays its exact request", async ({
  page,
  context,
}, testInfo) => {
  let healthy = false;
  const attempts = [];
  await prepare(context, (route) => {
    const body = route.request().postDataJSON();
    attempts.push(body);
    return healthy
      ? json(route, {
          card: {
            ...work,
            id: "assignment-id",
            assignee_refs: body.patch.assignee_refs,
            updated_at: "2026-10-13T01:00:00Z",
          },
        })
      : json(
          route,
          { error: { message: "Private upstream diagnostics" } },
          524,
        );
  });
  await page.goto(`${root}/initiatives/card%3Aassignment`);
  await choose(page);
  await expect.poll(() => attempts.length).toBeGreaterThan(0);
  await openAssignment(page);
  await expect(
    page
      .getByRole("dialog", { name: "Command palette" })
      .locator('[data-mutation-state="retrying"]'),
  ).toBeVisible();
  await expect(
    page
      .getByRole("dialog", { name: "Command palette" })
      .getByText("Private upstream diagnostics", { exact: true }),
  ).not.toBeVisible();
  await page.screenshot({
    animations: "disabled",
    path: testInfo.outputPath("assignment-retrying.png"),
  });
  await page.reload();
  await openAssignment(page);
  await expect(
    page
      .getByRole("dialog", { name: "Command palette" })
      .getByLabel("Saved message"),
  ).toHaveValue("Assign “Durable task” to Bob");
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(
    page
      .getByRole("dialog", { name: "Command palette" })
      .locator('[data-mutation-state="sent"]'),
  ).toBeVisible();
  expect(
    attempts.every(
      (body) => JSON.stringify(body) === JSON.stringify(attempts[0]),
    ),
  ).toBe(true);
  await page.screenshot({
    animations: "disabled",
    path: testInfo.outputPath("assignment-sent.png"),
  });
});

test("offline assignment survives tab closure", async ({
  page,
  context,
}, testInfo) => {
  let healthy = false;
  await prepare(context, (route) =>
    healthy
      ? json(route, { card: { ...work, id: "assignment-id" } })
      : route.abort("internetdisconnected"),
  );
  await page.goto(`${root}/initiatives/card%3Aassignment`);
  await openAssignment(page);
  await context.setOffline(true);
  await choose(page);
  await openAssignment(page);
  await expect(
    page
      .getByRole("dialog", { name: "Command palette" })
      .getByLabel("Saved message"),
  ).toHaveValue("Assign “Durable task” to Bob");
  await page.screenshot({
    animations: "disabled",
    path: testInfo.outputPath("assignment-pending.png"),
  });
  await page.close();
  healthy = true;
  await context.setOffline(false);
  const next = await context.newPage();
  await next.goto(`${root}/initiatives/card%3Aassignment`);
  await openAssignment(next);
  await expect(
    next
      .getByRole("dialog", { name: "Command palette" })
      .locator('[data-mutation-state="sent"]'),
  ).toBeVisible();
});

test("conflicting assignment keeps its selected person and rebases only explicitly", async ({
  page,
  context,
}, testInfo) => {
  let version = stamp;
  const attempts = [];
  await prepare(
    context,
    (route) => {
      const body = route.request().postDataJSON();
      attempts.push(body);
      return body.if_updated_at === stamp
        ? json(route, { error: { message: "Conflicting changes" } }, 409)
        : json(route, {
            card: { ...work, id: "assignment-id", updated_at: version },
          });
    },
    () => ({ ...work, updated_at: version }),
  );
  await page.goto(`${root}/initiatives/card%3Aassignment`);
  await choose(page);
  await openAssignment(page);
  await expect(
    page
      .getByRole("dialog", { name: "Command palette" })
      .locator('[data-mutation-state="attention"]'),
  ).toBeVisible();
  await page.screenshot({
    animations: "disabled",
    path: testInfo.outputPath("assignment-attention.png"),
  });
  version = "2026-10-13T02:00:00Z";
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Durable task", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Edit assignment", exact: true })
    .click();
  await expect(
    page.getByText("Saved assignment: Bob for “Durable task”"),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Use latest version", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Send assignment", exact: true })
    .click();
  await openAssignment(page);
  await expect(
    page
      .getByRole("dialog", { name: "Command palette" })
      .locator('[data-mutation-state="sent"]'),
  ).toBeVisible();
  expect(attempts.at(-1).patch).toEqual(attempts[0].patch);
  expect(attempts.at(-1).if_updated_at).toBe(version);
  expect(attempts.at(-1).request_key).not.toBe(attempts[0].request_key);
});
