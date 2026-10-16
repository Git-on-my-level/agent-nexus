import { expect, test } from "@playwright/test";
import { setup as discussionSetup } from "./helpers/durable-discussion.js";
import {
  ITEM,
  ROW,
  URL,
  setup as inboxSetup,
} from "./helpers/durable-decide.js";
import { setup as pmSetup } from "./helpers/pm-work.js";

const discussionURL =
  "/o/local/w/local/threads/thread-durable-comment?tab=messages";
const inboxURL = URL;
const replyBox = (page) => page.locator(`${ROW} [data-decision-reply]`);
const sendReply = (page) =>
  page.locator(ROW).getByRole("button", { name: "Send reply" }).click();
const text = "One submission shared by two tabs.";
const json = (route, body, status = 201) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

async function saved(page, kind, target) {
  return page.evaluate(
    async ({ kind, target }) => {
      const store = await import("/src/lib/localWorkStore.js");
      const scope = await import("/src/lib/readerScope.js");
      return target
        ? store.loadLocalWork(scope.readerScopeKey(), kind, target)
        : store.listLocalWork(scope.readerScopeKey(), kind);
    },
    { kind, target },
  );
}

test("simultaneous identical composer submissions create one entity even with cross-tab notifications delayed", async ({
  page,
  context,
}) => {
  // Notification loss/delay is permitted. Correctness must come from reading
  // persisted records under the scope lock, not a BroadcastChannel race.
  await context.addInitScript(() => {
    window.BroadcastChannel = class {
      postMessage() {}
      close() {}
    };
    const add = window.addEventListener.bind(window);
    window.addEventListener = (type, ...args) => {
      if (type !== "storage") add(type, ...args);
    };
  });
  const bodies = [];
  await discussionSetup(context, (route, events) => {
    const body = route.request().postDataJSON();
    bodies.push(body);
    const event = {
      ...body.event,
      id: `posted-${bodies.length}`,
      ts: new Date().toISOString(),
    };
    events.push(event);
    return json(route, { event });
  });
  const other = await context.newPage();
  await Promise.all([page.goto(discussionURL), other.goto(discussionURL)]);
  await Promise.all([
    page.locator("#message-text").fill(text),
    other.locator("#message-text").fill(text),
  ]);
  await page.evaluate(async () => {
    const { readerScopeKey } = await import("/src/lib/readerScope.js");
    let acquired;
    const ready = new Promise((resolve) => (acquired = resolve));
    window.heldDrain = navigator.locks.request(
      `anx:mutation-send:${readerScopeKey()}`,
      async () => {
        acquired();
        await new Promise((resolve) => (window.releaseDrain = resolve));
      },
    );
    await ready;
  });
  await Promise.all([
    page.getByRole("button", { name: "Send", exact: true }).click(),
    other.getByRole("button", { name: "Send", exact: true }).click(),
  ]);
  await expect.poll(async () => (await saved(page, "mutation")).length).toBe(2);
  await page.evaluate(() => window.releaseDrain());
  await expect.poll(() => bodies.length).toBe(1);
  await expect
    .poll(
      async () =>
        (await saved(page, "mutation")).filter(
          (row) => row.value.state !== "superseded",
        ).length,
    )
    .toBe(0);
  await page.reload();
  await other.reload();
  await expect(page.locator("[data-unsent-indicator]")).toHaveCount(0);
  expect(bodies).toHaveLength(1);
});

test("two plain-HTTP tabs entering the send path for the same outbox item send the same request_key and produce one server effect", async ({
  page,
  context,
}) => {
  await context.addInitScript(() => {
    Object.defineProperty(navigator, "locks", {
      configurable: true,
      get: () => undefined,
    });
    // Isolate lease storage per tab so both plain-HTTP tabs acquire their own
    // lease and enter the send callback concurrently for the same persisted item.
    const tabLeaseStore = new Map();
    const rawGetItem = Storage.prototype.getItem;
    const rawSetItem = Storage.prototype.setItem;
    const rawRemoveItem = Storage.prototype.removeItem;
    Storage.prototype.getItem = function (key) {
      if (typeof key === "string" && key.startsWith("anx:mutation-lock:")) {
        return tabLeaseStore.has(key) ? tabLeaseStore.get(key) : null;
      }
      return rawGetItem.call(this, key);
    };
    Storage.prototype.setItem = function (key, value) {
      if (typeof key === "string" && key.startsWith("anx:mutation-lock:")) {
        tabLeaseStore.set(key, String(value));
        return;
      }
      return rawSetItem.call(this, key, value);
    };
    Storage.prototype.removeItem = function (key) {
      if (typeof key === "string" && key.startsWith("anx:mutation-lock:")) {
        tabLeaseStore.delete(key);
        return;
      }
      return rawRemoveItem.call(this, key);
    };
  });

  const seenKeys = [];
  const byKey = new Map();
  let releaseBoth;
  const bothEntered = new Promise((resolve) => {
    releaseBoth = resolve;
  });

  await discussionSetup(context, async (route, events) => {
    const body = route.request().postDataJSON();
    seenKeys.push(body.request_key);
    if (seenKeys.length >= 2) releaseBoth();
    await bothEntered;
    if (!byKey.has(body.request_key)) {
      const event = {
        ...body.event,
        id: `posted-${events.length + 1}`,
        ts: new Date().toISOString(),
      };
      events.push(event);
      byKey.set(body.request_key, { event });
    }
    return json(route, byKey.get(body.request_key));
  });

  const other = await context.newPage();
  await Promise.all([page.goto(discussionURL), other.goto(discussionURL)]);
  await page.evaluate(() =>
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => false,
    }),
  );
  await page.locator("#message-text").fill(text);
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect.poll(async () => (await saved(page, "mutation")).length).toBe(1);

  await Promise.all([
    page.evaluate(async () => {
      Object.defineProperty(navigator, "onLine", {
        configurable: true,
        get: () => true,
      });
      const outbox = await import("/src/lib/mutationOutbox.js");
      await outbox.recoverMutations();
    }),
    other.evaluate(async () => {
      const outbox = await import("/src/lib/mutationOutbox.js");
      await outbox.recoverMutations();
    }),
  ]);

  expect(seenKeys.length).toBeGreaterThanOrEqual(2);
  expect(new Set(seenKeys).size).toBe(1);
  expect(byKey.size).toBe(1);
  await expect.poll(async () => (await saved(page, "mutation")).length).toBe(0);
});

test("distinct persisted bodies for the same composer remain separate intents", async ({
  page,
  context,
}) => {
  const bodies = [];
  await discussionSetup(context, (route) => {
    bodies.push(route.request().postDataJSON());
    return json(route, { event: { id: `posted-${bodies.length}` } });
  });
  await page.goto(discussionURL);
  await expect(page.locator("#message-text")).toBeVisible();
  await page.evaluate(async () => {
    const { enqueueDiscussionMessage } =
      await import("/src/lib/mutationOutbox.js");
    await Promise.all(
      ["First distinct message", "Second distinct message"].map((text) =>
        enqueueDiscussionMessage({
          target: "test:distinct",
          draft: { text },
          event: {
            type: "message_posted",
            thread_id: "thread-durable-comment",
            refs: ["thread:thread-durable-comment"],
            summary: text,
            payload: { text },
          },
        }),
      ),
    );
  });
  await expect.poll(() => bodies.length).toBe(2);
  expect(bodies[0].event.payload.text).not.toBe(bodies[1].event.payload.text);
});

test("storage denial during an expired new-key retry keeps the request and prevents a send", async ({
  page,
  context,
}) => {
  await context.addInitScript(() =>
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => false,
    }),
  );
  let attempts = 0;
  await discussionSetup(context, (route) => {
    attempts++;
    return json(route, { event: { id: "unexpected" } });
  });
  await page.goto(discussionURL);
  await page.locator("#message-text").fill(text);
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect.poll(async () => (await saved(page, "mutation")).length).toBe(1);
  await page.evaluate(async () => {
    const store = await import("/src/lib/localWorkStore.js");
    const { readerScopeKey } = await import("/src/lib/readerScope.js");
    const { autoRetryMaxAgeMs } = await import("/src/lib/replayPolicy.js");
    const [record] = await store.listLocalWork(readerScopeKey(), "mutation");
    await store.saveLocalWork(readerScopeKey(), "mutation", record.target, {
      ...record.value,
      createdAt: Date.now() - autoRetryMaxAgeMs - 1,
    });
  });
  await page.reload();
  await expect(
    page.getByRole("button", { name: "Retry as new", exact: true }),
  ).toBeVisible();
  await page.evaluate(() => {
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => true,
    });
    const set = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key.startsWith("anx:local-work:v1:"))
        throw new DOMException("Full", "QuotaExceededError");
      return set.call(this, key, value);
    };
    const transaction = IDBDatabase.prototype.transaction;
    IDBDatabase.prototype.transaction = function (...args) {
      if (args[1] === "readwrite")
        throw new DOMException("Full", "QuotaExceededError");
      return transaction.apply(this, args);
    };
  });
  await page.getByRole("button", { name: "Retry as new", exact: true }).click();
  await expect(
    page.locator('.msg-composer [data-mutation-state="attention"]').first(),
  ).toContainText("Local saving is unavailable");
  await expect(page.getByLabel("Saved message").first()).toHaveValue(text);
  expect(attempts).toBe(0);
});

test("confirmation preserves a newer Decide draft differing only by whitespace", async ({
  page,
  context,
}) => {
  let release;
  const gate = new Promise((resolve) => (release = resolve));
  let attempts = 0;
  await inboxSetup(context, async (route) => {
    attempts++;
    await gate;
    return json(route, { event: { id: "confirmed" } });
  });
  await page.goto(inboxURL);
  await replyBox(page).fill(text);
  await sendReply(page);
  const flight = page.evaluate(async () => {
    const queue = await import("/src/lib/inboxResponseQueue.js");
    await queue.flushInboxResponse();
  });
  await expect.poll(() => attempts).toBe(1);
  const other = await context.newPage();
  await other.goto(inboxURL);
  await expect(replyBox(other)).toBeAttached();
  const newer = ` ${text}\n`;
  await other.evaluate(
    async ({ id, newer }) => {
      const { saveLocalWork } = await import("/src/lib/localWorkStore.js");
      const { readerScopeKey } = await import("/src/lib/readerScope.js");
      await saveLocalWork(
        readerScopeKey(),
        "draft",
        `inbox:${id}:reply`,
        newer,
      );
    },
    { id: ITEM.id, newer },
  );
  release();
  await flight;
  expect(await saved(other, "draft", `inbox:${ITEM.id}:reply`)).toBe(newer);
  await other.reload();
  await expect(replyBox(other)).toHaveValue(newer);
});

for (const surface of ["discussion", "inbox", "inbox-unknown-age"])
  test(`${surface} recovery stops at the shared age cutoff and only an explicit new-key retry sends`, async ({
    page,
    context,
  }, testInfo) => {
    const kind = surface.startsWith("inbox") ? "inbox" : "discussion";
    await context.addInitScript(() =>
      Object.defineProperty(navigator, "onLine", {
        configurable: true,
        get: () => false,
      }),
    );
    const bodies = [];
    const handle = (route) => {
      bodies.push(route.request().postDataJSON());
      return json(
        route,
        kind === "inbox"
          ? { event: { id: "new-inbox" } }
          : { event: { id: "new-discussion" } },
      );
    };
    if (kind === "inbox") await inboxSetup(context, handle);
    else await discussionSetup(context, handle);
    await page.goto(kind === "inbox" ? inboxURL : discussionURL);
    if (kind === "inbox") {
      await replyBox(page).fill(text);
      await sendReply(page);
    } else {
      await page.locator("#message-text").fill(text);
      await page.getByRole("button", { name: "Send", exact: true }).click();
    }
    const storeKind = kind === "inbox" ? "inbox-send" : "mutation";
    await expect
      .poll(async () => (await saved(page, storeKind)).length)
      .toBe(1);
    const original = await page.evaluate(
      async ({ storeKind, surface }) => {
        const store = await import("/src/lib/localWorkStore.js");
        const { readerScopeKey } = await import("/src/lib/readerScope.js");
        const { autoRetryMaxAgeMs } = await import("/src/lib/replayPolicy.js");
        const [record] = await store.listLocalWork(readerScopeKey(), storeKind);
        const value = {
          ...record.value,
          createdAt: Date.now() - autoRetryMaxAgeMs - 1,
          nextAt: 0,
        };
        if (surface === "inbox-unknown-age") delete value.createdAt;
        await store.saveLocalWork(
          readerScopeKey(),
          storeKind,
          record.target,
          value,
        );
        return value.request;
      },
      { storeKind, surface },
    );
    await page.reload();
    // Decide's row offers Retry; the recovery panel offers "Retry as new".
    if (kind === "inbox")
      await page.locator("[data-unsent-indicator] summary").click();
    await expect(
      page.getByRole("button", { name: "Retry as new", exact: true }),
    ).toBeVisible();
    await page.evaluate(() => {
      Object.defineProperty(navigator, "onLine", {
        configurable: true,
        get: () => true,
      });
      window.dispatchEvent(new Event("online"));
    });
    expect(bodies).toHaveLength(0);
    await page.evaluate(async (kind) => {
      if (kind === "inbox") {
        const queue = await import("/src/lib/inboxResponseQueue.js");
        await queue.retryInboxResponse("inbox-durable");
      } else {
        const queue = await import("/src/lib/mutationOutbox.js");
        let entry;
        const off = queue.mutationOutbox.subscribe((rows) => (entry = rows[0]));
        off();
        await queue.retryMutation(entry.key);
      }
    }, kind);
    expect(bodies).toHaveLength(0);
    await page.screenshot({
      path: testInfo.outputPath(`${surface}-expired-attention.png`),
      fullPage: true,
    });
    await page
      .getByRole("button", { name: "Retry as new", exact: true })
      .click();
    if (kind === "inbox")
      await page.evaluate(async () => {
        const queue = await import("/src/lib/inboxResponseQueue.js");
        await queue.flushInboxResponse();
      });
    await expect.poll(() => bodies.length).toBe(1);
    const key = kind === "inbox" ? "idempotency_key" : "request_key";
    expect(bodies[0][key]).not.toBe(original[key]);
    const oldBody = {
      ...original,
      actor_id: kind === "inbox" ? "actor-durable-e2e" : "actor-comment",
    };
    const newBody = { ...bodies[0] };
    delete oldBody[key];
    delete newBody[key];
    expect(newBody).toEqual(oldBody);
  });

test("an expired first PM message retries with fresh message keys in its confirmed conversation", async ({
  page,
}) => {
  let reject = true;
  const { calls, conversations } = await pmSetup(page, {
    handle: async ({ path, reply }) => {
      if (path.endsWith("/messages") && reject) {
        await reply({ error: { code: "conflict" } }, 409);
        return true;
      }
    },
  });
  await page.goto("/o/local/w/local/pm?work_ref=card%3Arelease");
  await page.getByLabel("Message PM", { exact: true }).fill(text);
  await page.getByRole("button", { name: "Send message", exact: true }).click();
  await expect(page.locator('[data-mutation-state="attention"]')).toBeVisible();
  const original = calls.find((call) => call.path.endsWith("/messages")).body;
  await page.evaluate(async () => {
    const store = await import("/src/lib/localWorkStore.js");
    const { readerScopeKey } = await import("/src/lib/readerScope.js");
    const { autoRetryMaxAgeMs } = await import("/src/lib/replayPolicy.js");
    const [record] = await store.listLocalWork(readerScopeKey(), "mutation");
    // A deliberate new retry upgrades a legacy message to receipt mode;
    // automatic recovery must continue to leave such old bodies untouched.
    delete record.value.request.message.replay_key;
    delete record.value.request.creation.replay_key;
    await store.saveLocalWork(readerScopeKey(), "mutation", record.target, {
      ...record.value,
      createdAt: Date.now() - autoRetryMaxAgeMs - 1,
    });
  });
  await page.reload();
  await expect(
    page.getByRole("button", { name: "Retry as new", exact: true }),
  ).toBeVisible();
  reject = false;
  await page.getByRole("button", { name: "Retry as new", exact: true }).click();
  await expect(page.getByText(/^Working/)).toBeVisible();
  const messages = calls.filter((call) => call.path.endsWith("/messages"));
  expect(messages).toHaveLength(2);
  expect(messages[1].path).toBe(messages[0].path);
  expect(messages[1].body.request_key).not.toBe(original.request_key);
  expect(messages[1].body.replay_key).not.toBe(original.replay_key);
  expect(messages[1].body.replay_key).toEqual(expect.any(String));
  expect(messages[1].body.text).toBe(original.text);
  expect(conversations).toHaveLength(1);
});
