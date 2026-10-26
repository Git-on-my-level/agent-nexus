import { expect, test } from "@playwright/test";
import { LIST, ROW, URL, setup } from "./helpers/durable-decide.js";

/**
 * Durability of a Decide reply (the former inbox durability specs): the
 * draft, the persisted outbox request and its idempotency key survive
 * reload, tab closure, offline periods and lost confirmations.
 */
const TEXT = "Use Monday. Keep the second region on Thursday.";
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

/** Open the permalink (its composer is always open) and type the reply. */
async function begin(page) {
  await page.goto(URL);
  const reply = page.locator(`${ROW} [data-decision-reply]`);
  await expect(reply).toBeVisible();
  await reply.fill(TEXT);
}

const sendReply = (page) =>
  page.locator(ROW).getByRole("button", { name: "Send reply" }).click();

test("drafts restore after reload, navigation and in another tab", async ({
  page,
  context,
}) => {
  await setup(context, (route) =>
    json(route, { event: { id: "response-one" } }, 201),
  );
  await begin(page);
  await page.reload();
  await expect(page.locator(`${ROW} [data-decision-reply]`)).toHaveValue(TEXT);
  await page.goto(LIST);
  // Decide opens on the first decision, its composer already open.
  const row = page.locator(ROW);
  await expect(row.locator("[data-decision-reply]")).toHaveValue(TEXT);
  const revised = `${TEXT} Include verification.`;
  await row.locator("[data-decision-reply]").fill(revised);
  await page.goto(URL);
  await expect(page.locator(`${ROW} [data-decision-reply]`)).toHaveValue(
    revised,
  );
  const other = await context.newPage();
  await other.goto(URL);
  await expect(other.locator(`${ROW} [data-decision-reply]`)).toHaveValue(
    revised,
  );
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
    await sendReply(page);
    await flush(page);
    await expect.poll(() => bodies.length).toBe(1);
    await expect(page.locator("body")).not.toContainText(
      "cloudflare-secret-error-body",
    );
    await expect
      .poll(async () => (await records(page))[0]?.value.request.response_text)
      .toBe(TEXT);
    await page.reload();
    await expect.poll(() => bodies.length).toBe(2);
    expect(bodies[1]).toEqual(bodies[0]);
    expect(bodies[0].idempotency_key).toBeTruthy();
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
  await sendReply(page);
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
  await sendReply(page);
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

test("a tab waiting on the send lock honours a failed retry's backoff", async ({
  page,
  context,
}) => {
  // Waits out two real backoffs (4s, then 8s).
  test.slow();
  const sends = [];
  let failedAt = 0;
  let release;
  const waiting = new Promise((resolve) => (release = resolve));
  await setup(context, async (route) => {
    sends.push({ at: Date.now(), body: route.request().postDataJSON() });
    if (sends.length === 1)
      return json(route, { upstream: "private upstream failure" }, 524);
    if (sends.length === 2) {
      await waiting;
      failedAt = Date.now();
      return json(route, { upstream: "private upstream failure" }, 524);
    }
    return json(route, { event: { id: "response-one" } }, 201);
  });
  await begin(page);
  await sendReply(page);
  await flush(page);
  // The scheduled retry is in flight, so its persisted row reads "sending"
  // and is already due.
  await expect.poll(() => sends.length, { timeout: 10000 }).toBe(2);
  const other = await context.newPage();
  await other.goto(URL);
  // The second tab lists that row as due and queues on the send lock the
  // first tab holds until the uncertain response arrives.
  await expect(other.locator("[data-unsent-indicator]")).toBeVisible();
  // A busy or background sending tab handles its failure slowly; the backoff
  // must not depend on that tab winning the race to storage.
  const cdp = await context.newCDPSession(page);
  await cdp.send("Emulation.setCPUThrottlingRate", { rate: 20 });
  release();
  await expect.poll(() => sends.length, { timeout: 15000 }).toBe(3);
  await cdp.send("Emulation.setCPUThrottlingRate", { rate: 1 });
  // The failed retry's full backoff (attempt 2: 2s * 2^2 = 8s) must hold
  // across tabs. It starts when the sending tab checkpoints the failure,
  // which is after the reply below was fulfilled, so no resend may come
  // earlier than failedAt + 8s (less clock rounding). A waiting tab that
  // resends once the lock is released, however late, cannot meet this.
  expect(sends[2].at - failedAt).toBeGreaterThanOrEqual(8000 - 100);
  for (const send of sends) expect(send.body).toEqual(sends[0].body);
  await expect.poll(async () => (await records(other)).length).toBe(0);
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
  await sendReply(page);
  await flush(page);
  const failed = page.locator(`${ROW} [data-decision-failed]`);
  await expect(failed).toBeVisible();
  await expect(failed.getByRole("button", { name: "Retry" })).toBeVisible();
  await failed.getByRole("button", { name: "Edit" }).click();
  await expect(page.locator(`${ROW} [data-decision-reply]`)).toHaveValue(TEXT);
  expect(attempts).toBe(1);
});

test("an explicitly resolved ask offers Copy and Discard, not Retry", async ({
  page,
  context,
}) => {
  let attempts = 0;
  await setup(context, (route) => {
    attempts += 1;
    return json(
      route,
      { error: { code: "conflict", message: "Ask is no longer open" } },
      409,
    );
  });
  await begin(page);
  await sendReply(page);
  await flush(page);
  const failed = page.locator(`${ROW} [data-decision-failed]`);
  await expect(failed.locator("[data-decision-gone]")).toBeVisible();
  await expect(failed.getByRole("button", { name: "Retry" })).toHaveCount(0);
  await expect(failed.getByRole("button", { name: "Edit" })).toHaveCount(0);
  await expect(
    failed.getByRole("button", { name: "Copy answer" }),
  ).toBeVisible();
  const toast = page.locator('[data-inbox-toast="failed"]');
  await expect(toast).toContainText("Not sent.");
  await expect(toast.getByRole("button", { name: "Retry" })).toHaveCount(0);
  expect((await records(page)).length).toBe(1);
  const chip = page.locator("[data-unsent-indicator]");
  await chip.locator("summary").click();
  await expect(chip.getByRole("button", { name: "Retry send" })).toHaveCount(0);
  await expect(chip.locator("[data-inbox-discard]")).toBeVisible();
  await failed.getByRole("button", { name: "Discard" }).click();
  await expect(page.locator(`${ROW} [data-decision-failed]`)).toHaveCount(0);
  await expect(toast).toHaveCount(0);
  await expect.poll(async () => (await records(page)).length).toBe(0);
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
  const reply = page.locator(`${ROW} [data-decision-reply]`);
  await expect(reply).toHaveValue(TEXT);
  await page.evaluate(() => {
    Storage.prototype.setItem = () => {
      throw new Error("quota");
    };
  });
  await reply.fill("Keep this additional text.");
  await expect(page.locator("[data-local-save-warning]")).toBeVisible();
  await page
    .locator("[data-local-save-warning]")
    .getByRole("button", { name: "Dismiss" })
    .click();
  await sendReply(page);
  await flush(page);
  expect(attempts).toBe(0);
  await expect(reply).toHaveValue("Keep this additional text.");
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
  await expect(other.locator(`${ROW} [data-decision-reply]`)).toHaveValue(TEXT);
  await sendReply(page);
  const flight = flush(page);
  await expect.poll(() => attempts).toBe(1);
  await page.evaluate(async () => {
    const actors = await import("/src/lib/actorSession.js");
    actors.chooseActor("actor-other-reader");
  });
  await expect(other.locator(`${ROW} [data-decision-reply]`)).toHaveValue("");
  release();
  await flight;
  expect(await records(other)).toEqual([]);
  expect(await records(other, "draft")).toEqual([]);
  await expect(other.locator("[data-unsent-indicator]")).toHaveCount(0);
});

test("a proposal tap is persisted before it is sent and reuses its key", async ({
  page,
  context,
}) => {
  const bodies = [];
  await setup(context, (route) => {
    bodies.push(route.request().postDataJSON());
    return bodies.length === 1
      ? json(route, { error: { code: "upstream_timeout" } }, 524)
      : json(route, { event: { id: "response-one" } }, 201);
  });
  await page.goto(LIST);
  await page.locator(`${ROW} [data-decision-proposal="1"]`).click();
  await flush(page);
  await expect.poll(() => bodies.length).toBe(1);
  await page.reload();
  await expect.poll(() => bodies.length).toBe(2);
  expect(bodies[1]).toEqual(bodies[0]);
  expect(bodies[0]).toMatchObject({ response_text: "Use the Monday window." });
  await expect.poll(async () => (await records(page)).length).toBe(0);
});

test("a saved 404 answer retries after reload without retyping or changing its key", async ({
  page,
  context,
}) => {
  const bodies = [];
  await setup(context, (route) => {
    bodies.push(route.request().postDataJSON());
    if (bodies.length === 1)
      return json(
        route,
        { error: { code: "not_found", message: "inbox item not found" } },
        404,
      );
    return json(route, { event: { id: "saved-answer" } }, 201);
  });
  await begin(page);
  await sendReply(page);
  await flush(page);
  await expect(page.locator(`${ROW} [data-decision-gone]`)).toHaveCount(0);
  await expect(
    page
      .locator(`${ROW} [data-decision-failed]`)
      .getByRole("button", { name: "Retry", exact: true }),
  ).toBeVisible();
  expect((await records(page))[0].value.request.response_text).toBe(TEXT);
  await page.reload();
  const chip = page.locator("[data-unsent-indicator]");
  await chip.locator("summary").click();
  await chip.getByRole("button", { name: "Retry send", exact: true }).click();
  await expect.poll(() => bodies.length).toBe(2);
  expect(bodies[1]).toEqual(bodies[0]);
  expect(bodies[1].response_text).toBe(TEXT);
  await expect.poll(async () => (await records(page)).length).toBe(0);
});
