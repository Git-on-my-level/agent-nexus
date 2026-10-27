import { expect, test } from "@playwright/test";
import { expectCleanLayout } from "../helpers/layoutAudit.js";
import { setup } from "./helpers/durable-discussion.js";
const root = "/o/local/w/local";
const stamp = "2026-10-13T00:00:00Z";
const base = {
  ref: "card:move",
  handle: "move",
  title: "Durable move",
  board_ref: "board:move",
  version: 7,
  phase: "review",
  source: { authority: "nexus" },
  updated_at: stamp,
  thread_id: "thread-durable-comment",
  definition_of_done: [],
};
const json = (route, value, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(value),
  });
async function prepare(
  context,
  send,
  current = () => base,
  boardStamp = () => stamp,
) {
  await setup(context, () => {});
  await context.route(/\/work(?:\?.*)?$/, (route) =>
    json(route, { work: [current()] }),
  );
  await context.route("**/work/card%3Amove*", (route) =>
    json(route, { work: current() }),
  );
  await context.route("**/cards/card%3Amove/plan*", (route) =>
    json(route, { plan: null }),
  );
  await context.route(/\/boards(?:\?.*)?$/, (route) =>
    json(route, {
      boards: [
        {
          id: "move",
          ref: "board:move",
          title: "Moves",
          updated_at: boardStamp(),
        },
      ],
    }),
  );
  await context.route("**/boards/board%3Amove*", (route) =>
    json(route, { board: { id: "move", updated_at: boardStamp() } }),
  );
  await context.route("**/cards/card%3Amove/move", send);
}
const receipt = () => ({
  card: { id: "move" },
  board: { id: "move", ref: "board:move" },
});
async function openPalette(page) {
  await page.keyboard.press("Escape");
  await page
    .getByRole("heading", { name: "Durable move", exact: true })
    .click();
  await page.keyboard.press("m");
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toBeVisible();
}
async function choose(page) {
  await openPalette(page);
  await page
    .getByRole("option", { name: "Move to In progress", exact: true })
    .click();
  await expect(
    page.getByText("Saved move to In progress. Waiting for confirmation.", {
      exact: true,
    }),
  ).toBeVisible();
}
async function evidence(page) {
  await page.goto(`${root}/initiatives?view=board`);
  const card = page.locator('[data-work-ref="card:move"]');
  await expect(card).toBeVisible();
  await card.focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByLabel(/needs evidence/)).toBeVisible();
}
test("uncertain palette move reloads and retries its exact bases and key", async ({
  page,
  context,
}, info) => {
  let healthy = false;
  const attempts = [];
  await prepare(context, (route) => {
    attempts.push(route.request().postDataJSON());
    return healthy
      ? json(route, receipt())
      : json(route, { error: { message: "Private move diagnostics" } }, 524);
  });
  await page.goto(`${root}/initiatives/card%3Amove`);
  await choose(page);
  await openPalette(page);
  await expect(
    page.locator('[data-mutation-state="retrying"]').last(),
  ).toBeVisible();
  await expect(
    page.getByText("Private move diagnostics", { exact: true }),
  ).not.toBeVisible();
  await expectCleanLayout(page, "move retrying");
  await page.screenshot({
    path: info.outputPath("move-retrying.png"),
    animations: "disabled",
  });
  await page.reload();
  await openPalette(page);
  await expect(
    page.getByRole("dialog").getByLabel("Saved message"),
  ).toHaveValue("Durable move\nDestination: In progress");
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(
    page.getByRole("dialog").locator('[data-mutation-state="sent"]'),
  ).toBeVisible();
  expect(attempts.length).toBeGreaterThan(1);
  expect(
    attempts.every(
      (body) => JSON.stringify(body) === JSON.stringify(attempts[0]),
    ),
  ).toBe(true);
  expect(attempts[0]).toMatchObject({
    actor_id: "actor-comment",
    if_version: 7,
    if_board_updated_at: stamp,
  });
  await expectCleanLayout(page, "move sent");
  await page.screenshot({
    path: info.outputPath("move-sent.png"),
    animations: "disabled",
  });
});
test("completion draft and offline send survive original tab closure", async ({
  page,
  context,
}, info) => {
  const attempts = [];
  await prepare(context, (route) => {
    attempts.push(route.request().postDataJSON());
    return json(route, receipt());
  });
  await evidence(page);
  await page.getByLabel(/needs evidence/).fill("artifact:proof");
  await page.reload();
  await expect(page.getByLabel(/needs evidence/)).toHaveValue("artifact:proof");
  await page.evaluate(() =>
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => false,
    }),
  );
  await page.getByRole("button", { name: "Mark done", exact: true }).click();
  await expect(page.locator('[data-mutation-state="pending"]')).toBeVisible();
  expect(attempts).toHaveLength(0);
  await expectCleanLayout(page, "completion pending");
  await page.screenshot({
    path: info.outputPath("move-pending.png"),
    animations: "disabled",
  });
  await page.close();
  const next = await context.newPage();
  await next.goto(`${root}/initiatives?view=board`);
  await expect(next.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(attempts).toHaveLength(1);
  expect(attempts[0]).toMatchObject({
    if_version: 7,
    if_board_updated_at: stamp,
    resolution: "done",
    resolution_refs: ["artifact:proof"],
  });
});
test("conflicting completion edits preserve evidence and rebase only explicitly", async ({
  page,
  context,
}, info) => {
  let version = 7;
  let token = stamp;
  const attempts = [];
  await prepare(
    context,
    (route) => {
      const body = route.request().postDataJSON();
      attempts.push(body);
      return body.if_version === 7
        ? json(route, { error: { message: "Newer annotations" } }, 409)
        : json(route, receipt());
    },
    () => ({ ...base, version }),
    () => token,
  );
  await evidence(page);
  await page.getByLabel(/needs evidence/).fill("event:proof");
  await page.getByRole("button", { name: "Mark done", exact: true }).click();
  await expect(page.locator('[data-mutation-state="attention"]')).toBeVisible();
  await expectCleanLayout(page, "completion attention");
  await page.screenshot({
    path: info.outputPath("move-attention.png"),
    animations: "disabled",
  });
  version = 8;
  token = "2026-10-13T01:00:00Z";
  await page.reload();
  await page.getByRole("button", { name: "Edit move", exact: true }).click();
  await expect(page.getByLabel(/needs evidence/)).toHaveValue("event:proof");
  await page
    .getByRole("button", { name: "Use latest version", exact: true })
    .click();
  await page.getByRole("button", { name: "Mark done", exact: true }).click();
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(attempts.at(-1)).toMatchObject({
    if_version: 8,
    if_board_updated_at: token,
    resolution_refs: ["event:proof"],
  });
  expect(attempts.at(-1).request_key).not.toBe(attempts[0].request_key);
});

test("source-owned completion recovers the original proposal without another approval prompt", async ({
  page,
  context,
}) => {
  let healthy = false;
  let confirmations = 0;
  const attempts = [];
  const external = {
    ...base,
    source: { authority: "github", revision: "original-source" },
    decision_revision: "original-source",
  };
  await prepare(
    context,
    () => {
      throw new Error("Source work must not move natively");
    },
    () => external,
  );
  await context.route("**/pm/presence", (route) =>
    json(route, { state: "connected", configured: true, connected: true }),
  );
  await context.route(/\/pm\/decisions(?:\?.*)?$/, (route) => {
    if (route.request().method() !== "POST")
      return json(route, {
        items: [
          {
            id: "proposal-move",
            work_ref: base.ref,
            scope: "work.phase",
            status: "declined",
          },
        ],
      });
    const body = route.request().postDataJSON();
    attempts.push(body);
    return healthy
      ? json(
          route,
          { ...body, id: "proposal-move", status: "awaiting_answer" },
          201,
        )
      : json(route, { error: { message: "Disconnected after commit" } }, 503);
  });
  page.on("dialog", async (dialog) => {
    confirmations += 1;
    await dialog.accept();
  });
  await evidence(page);
  await page.getByLabel(/needs evidence/).fill("artifact:source-proof");
  await page.getByRole("button", { name: "Mark done", exact: true }).click();
  await expect(page.locator('[data-mutation-state="retrying"]')).toBeVisible();
  await page.reload();
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(confirmations).toBe(1);
  expect(attempts.length).toBeGreaterThan(1);
  expect(
    attempts.every(
      (body) => JSON.stringify(body) === JSON.stringify(attempts[0]),
    ),
  ).toBe(true);
  expect(attempts[0]).toMatchObject({
    actor_id: "actor-comment",
    replay_key: expect.any(String),
    request_key: "task-phase:card:move:done:original-source",
    target_revision: "original-source",
    payload: { phase: "done", resolution_refs: ["artifact:source-proof"] },
  });
  // The original awaiting receipt is delivery evidence. Current list reads,
  // rather than that old receipt, decide whether the board shows Requested.
  await expect(page.locator('[data-work-ref="card:move"]')).not.toContainText(
    "Requested",
  );
});

test("confirmation in one tab preserves newer completion evidence in another", async ({
  page,
  context,
}) => {
  let finish;
  const gate = new Promise((resolve) => {
    finish = resolve;
  });
  const attempts = [];
  await prepare(context, async (route) => {
    attempts.push(route.request().postDataJSON());
    await gate;
    await json(route, receipt());
  });
  await evidence(page);
  await page.getByLabel(/needs evidence/).fill("artifact:original");
  await page.getByRole("button", { name: "Mark done", exact: true }).click();
  await expect(page.locator('[data-mutation-state="sending"]')).toBeVisible();
  const other = await context.newPage();
  await other.goto(`${root}/initiatives?view=board`);
  await expect(other.getByLabel(/needs evidence/)).toHaveValue(
    "artifact:original",
  );
  await other.getByLabel(/needs evidence/).fill("artifact:newer");
  finish();
  await expect(other.locator('[data-mutation-state="sent"]')).toBeVisible();
  await expect(other.getByLabel(/needs evidence/)).toHaveValue(
    "artifact:newer",
  );
  await other.reload();
  await expect(other.getByLabel(/needs evidence/)).toHaveValue(
    "artifact:newer",
  );
  expect(attempts).toHaveLength(1);
  expect(attempts[0].resolution_refs).toEqual(["artifact:original"]);
});

test("editing a failed board move from task detail returns to its durable composer", async ({
  page,
  context,
}) => {
  await prepare(context, (route) =>
    json(route, { error: { message: "Conflicting base" } }, 409),
  );
  await evidence(page);
  await page.getByLabel(/needs evidence/).fill("event:keep-through-navigation");
  await page.getByRole("button", { name: "Mark done", exact: true }).click();
  await expect(page.locator('[data-mutation-state="attention"]')).toBeVisible();
  await page.goto(`${root}/initiatives/card%3Amove`);
  await page.getByRole("button", { name: "Edit move", exact: true }).click();
  await expect(page).toHaveURL(/\/initiatives\?view=board$/);
  await expect(page.getByLabel(/needs evidence/)).toHaveValue(
    "event:keep-through-navigation",
  );
  await expect(
    page.getByRole("button", { name: "Mark done", exact: true }),
  ).toBeEnabled();
});

test("another board drop cannot replace unfinished completion evidence", async ({
  page,
  context,
}) => {
  await prepare(context, (route) => json(route, receipt()));
  const second = {
    ...base,
    ref: "card:second",
    handle: "second",
    title: "Another task",
  };
  await context.route(/\/work(?:\?.*)?$/, (route) =>
    json(route, { work: [base, second] }),
  );
  await evidence(page);
  await page.getByLabel(/needs evidence/).fill("event:unfinished-proof");
  const card = page.locator('[data-work-ref="card:second"][tabindex="0"]');
  const from = await card.boundingBox();
  const to = await page.locator('[data-work-phase="done"]').boundingBox();
  await page.mouse.move(from.x + from.width / 2, from.y + 12);
  await page.mouse.down();
  await page.mouse.move(from.x + from.width / 2 + 12, from.y + 20, {
    steps: 3,
  });
  await page.mouse.move(to.x + to.width / 2, to.y + 65, { steps: 8 });
  await page.mouse.up();
  await expect(
    page.getByText(/Finish or discard the saved move/),
  ).toBeVisible();
  await expect(page.getByLabel(/needs evidence/)).toHaveValue(
    "event:unfinished-proof",
  );
  await page.reload();
  await expect(page.getByLabel(/needs evidence/)).toHaveValue(
    "event:unfinished-proof",
  );
});

test("timed-out move retries the exact actor, body and original key", async ({
  page,
  context,
}) => {
  let healthy = false;
  const attempts = [];
  await prepare(context, (route) => {
    attempts.push(route.request().postDataJSON());
    return healthy ? json(route, receipt()) : route.abort("timedout");
  });
  await page.goto(`${root}/initiatives/card%3Amove`);
  await choose(page);
  await expect(page.locator('[data-mutation-state="retrying"]')).toBeVisible();
  await page.reload();
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(attempts.length).toBeGreaterThan(1);
  expect(
    attempts.every(
      (body) => JSON.stringify(body) === JSON.stringify(attempts[0]),
    ),
  ).toBe(true);
});

test("storage denial retains completion evidence and prevents any send", async ({
  page,
  context,
}) => {
  const attempts = [];
  await prepare(context, (route) => {
    attempts.push(route.request().postDataJSON());
    return json(route, receipt());
  });
  await context.addInitScript(() =>
    Object.defineProperty(window, "indexedDB", { value: undefined }),
  );
  await evidence(page);
  await page.getByLabel(/needs evidence/).fill("event:journal-proof");
  await page.reload();
  await expect(page.getByLabel(/needs evidence/)).toHaveValue(
    "event:journal-proof",
  );
  await page.evaluate(() => {
    Storage.prototype.setItem = () => {
      throw new Error("Storage denied");
    };
  });
  await page.getByLabel(/needs evidence/).fill("event:keep-when-storage-fails");
  await expect(page.locator("[data-local-save-warning]")).toBeVisible();
  await page
    .locator("[data-local-save-warning]")
    .getByRole("button", { name: "Dismiss" })
    .click();
  await page.getByRole("button", { name: "Mark done", exact: true }).click();
  await expect(page.locator('[data-mutation-state="attention"]')).toBeVisible();
  await expect(page.getByLabel("Saved message")).toHaveValue(
    /event:keep-when-storage-fails/,
  );
  expect(attempts).toHaveLength(0);
});

test("principal changes purge move work in other tabs and fence a late failure", async ({
  page,
  context,
}) => {
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const attempts = [];
  await prepare(context, async (route) => {
    attempts.push(route.request().postDataJSON());
    await gate;
    await route.abort("timedout");
  });
  await evidence(page);
  await page.getByLabel(/needs evidence/).fill("artifact:private-owner-proof");
  const oldScope = await page.evaluate(async () => {
    const store = await import("/src/lib/readerScope.js");
    return store.readerScopeKey();
  });
  const other = await context.newPage();
  await other.goto(`${root}/initiatives?view=board`);
  await expect(other.getByLabel(/needs evidence/)).toHaveValue(
    "artifact:private-owner-proof",
  );
  await page.getByRole("button", { name: "Mark done", exact: true }).click();
  await expect.poll(() => attempts.length).toBe(1);
  await page.evaluate(async () => {
    const auth = await import("/src/lib/authSession.js");
    auth.authenticatedAgent.set({
      agent_id: "replacement-owner",
      actor_id: "replacement-actor",
    });
  });
  await expect(other.getByLabel(/needs evidence/)).toHaveCount(0);
  release();
  await expect(other.locator("[data-unsent-indicator]")).toHaveCount(0);
  await expect
    .poll(() =>
      other.evaluate(async (scope) => {
        const store = await import("/src/lib/localWorkStore.js");
        return [
          await store.listLocalWork(scope, "mutation"),
          await store.listLocalWork(scope, "draft"),
        ];
      }, oldScope),
    )
    .toEqual([[], []]);
  await expect(
    other.getByText("artifact:private-owner-proof", { exact: true }),
  ).toHaveCount(0);
  expect(attempts).toHaveLength(1);
});
