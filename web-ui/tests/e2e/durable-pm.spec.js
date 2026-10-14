import { expect, test } from "@playwright/test";
import { setup } from "./helpers/pm-work.js";

const root = "/o/local/w/local";
const text = "Keep this PM question through uncertain delivery.";
const composer = (page) => page.getByLabel("Message PM", { exact: true });
const state = (page, value) =>
  page.locator(`.pm-composer [data-mutation-state="${value}"]`);
async function open(page) {
  await page.goto(`${root}/pm?work_ref=card%3Arelease`);
  await expect(composer(page)).toBeVisible();
}
async function send(page) {
  await composer(page).fill(text);
  await page.getByRole("button", { name: "Send message", exact: true }).click();
}

test("PM draft survives reload and closing its original tab", async ({
  page,
  context,
}) => {
  await setup(page);
  await open(page);
  await composer(page).fill(text);
  await page.reload();
  await expect(composer(page)).toHaveValue(text);
  const other = await context.newPage();
  await setup(other);
  await page.close();
  await open(other);
  await expect(composer(other)).toHaveValue(text);
});

test("offline PM submission survives tab closure and sends on reconnect", async ({
  page,
  context,
}, testInfo) => {
  const { calls } = await setup(page);
  await open(page);
  await page.evaluate(() =>
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => false,
    }),
  );
  await send(page);
  await expect(state(page, "pending")).toBeVisible();
  expect(calls.filter((row) => row.method === "POST")).toHaveLength(0);
  await page.screenshot({
    path: testInfo.outputPath("pm-pending.png"),
    fullPage: true,
  });
  await page.close();
  const other = await context.newPage();
  const recovered = await setup(other);
  await open(other);
  // The drain may finish before this page mounts its confirmation listener,
  // so the recovered turn never renders here; wait on the drained POST
  // instead of a transient status line.
  await expect
    .poll(
      () =>
        recovered.calls.filter(
          (row) => row.method === "POST" && row.path.endsWith("/messages"),
        ).length,
    )
    .toBe(1);
  const messages = recovered.calls.filter((row) =>
    row.path.endsWith("/messages"),
  );
  expect(messages).toHaveLength(1);
  expect(messages[0].body.text).toBe(text);
});

test("PM confirmation preserves newer composer text", async ({ page }) => {
  let finish;
  const gate = new Promise((resolve) => {
    finish = resolve;
  });
  await setup(page, {
    handle: async ({ path }) => {
      if (path.endsWith("/messages")) await gate;
    },
  });
  await open(page);
  await send(page);
  await expect(state(page, "sending")).toBeVisible();
  await composer(page).fill("My newer unsent question");
  finish();
  await expect(state(page, "sent")).toBeVisible();
  await expect(composer(page)).toHaveValue("My newer unsent question");
  await page.reload();
  await expect(composer(page)).toHaveValue("My newer unsent question");
});

for (const failure of ["creation", "message"])
  for (const fault of ["524", "timeout"])
    test(`lost ${failure} confirmation (${fault}) resumes exact keys after reload`, async ({
      page,
      context,
    }, testInfo) => {
      let healthy = false;
      const attempts = [];
      const creations = new Map();
      const messages = new Map();
      const handle = async ({
        path,
        method,
        body,
        reply,
        route,
        conversations,
        turns,
      }) => {
        if (method === "GET" && path === "/pm/conversations") {
          await reply({ items: [...creations.values()], has_more: false });
          return true;
        }
        if (method === "GET" && path.startsWith("/pm/conversations/")) {
          await reply({
            conversation: [...creations.values()][0],
            turns: [...messages.values()],
          });
          return true;
        }
        if (method !== "POST") return;
        const creation = path === "/pm/conversations";
        if (!creation && !path.endsWith("/messages")) return;
        attempts.push({ path, body });
        const receipts = creation ? creations : messages;
        const replayKey = body.replay_key || body.request_key;
        if (!receipts.has(replayKey))
          receipts.set(
            replayKey,
            creation
              ? {
                  id: "conversation-sample",
                  title: body.title,
                  work_ref: body.work_ref,
                  created_at: new Date().toISOString(),
                }
              : {
                  id: "turn-sample",
                  text: body.text,
                  status: "sending",
                  created_at: new Date().toISOString(),
                },
          );
        const result = receipts.get(replayKey);
        const rows = creation ? conversations : turns;
        if (!rows.some((row) => row.id === result.id)) rows.push(result);
        if (!healthy && (failure === "creation") === creation) {
          if (fault === "timeout") await route.abort("timedout");
          else
            await reply(
              { error: { message: "Private upstream failure" } },
              524,
            );
        } else await reply(result, creation ? 201 : 202);
        return true;
      };
      await setup(page, { handle });
      await open(page);
      await send(page);
      await expect(state(page, "retrying")).toBeVisible();
      await expect(page.locator(".pm-composer details[open]")).toHaveCount(0);
      await page.screenshot({
        path: testInfo.outputPath(`pm-${failure}-${fault}-retrying.png`),
        fullPage: true,
      });
      const original = attempts.find((row) =>
        failure === "creation"
          ? row.path === "/pm/conversations"
          : row.path.endsWith("/messages"),
      );
      expect(original.body.actor_id).toBe("actor-sample");
      expect(original.body.replay_key).toBeTruthy();
      expect(original.body.replay_key).not.toBe(original.body.request_key);
      await page.reload();
      await expect(state(page, "retrying")).toBeVisible();
      const other = await context.newPage();
      await setup(other, { handle });
      await open(other);
      healthy = true;
      await page.evaluate(() => window.dispatchEvent(new Event("online")));
      await other.evaluate(() => window.dispatchEvent(new Event("online")));
      await expect(page.getByText(/^Working/)).toBeVisible({ timeout: 15000 });
      expect(creations.size).toBe(1);
      expect(messages.size).toBe(1);
      const sameStep = attempts.filter((row) => row.path === original.path);
      expect(sameStep.length).toBeGreaterThan(1);
      for (const attempt of sameStep)
        expect(attempt.body).toEqual(original.body);
      await page.screenshot({
        path: testInfo.outputPath(`pm-${failure}-${fault}-confirmed.png`),
        fullPage: true,
      });
    });

test("definitive message failure keeps text and edits in its already-created conversation", async ({
  page,
}, testInfo) => {
  let reject = true;
  const { calls } = await setup(page, {
    handle: async ({ path, reply }) => {
      if (path.endsWith("/messages") && reject) {
        await reply({ error: { message: "Private policy detail" } }, 403);
        return true;
      }
    },
  });
  await open(page);
  await send(page);
  await expect(state(page, "attention")).toBeVisible();
  await expect(page.getByLabel("Saved message", { exact: true })).toHaveValue(
    text,
  );
  await expect(page.locator(".pm-composer details[open]")).toHaveCount(0);
  await page.screenshot({
    path: testInfo.outputPath("pm-attention.png"),
    fullPage: true,
  });
  await page.reload();
  await expect(state(page, "attention")).toBeVisible();
  await page.getByRole("button", { name: "Edit message", exact: true }).click();
  await expect(composer(page)).toHaveValue(text);
  await composer(page).fill("A corrected question");
  reject = false;
  await page.getByRole("button", { name: "Send message", exact: true }).click();
  await expect(page.getByText(/^Working/)).toBeVisible();
  expect(
    calls.filter(
      (row) => row.method === "POST" && row.path === "/pm/conversations",
    ),
  ).toHaveLength(1);
  const sends = calls.filter((row) => row.path.endsWith("/messages"));
  expect(sends[0].body.request_key).not.toBe(sends[1].body.request_key);
  expect(sends[0].body.replay_key).not.toBe(sends[1].body.replay_key);
  expect(sends[1].body.text).toBe("A corrected question");
});

test("decision note and exact answer survive reload and uncertain acknowledgement", async ({
  page,
}, testInfo) => {
  let healthy = false;
  const attempts = [];
  await setup(page, {
    handle: async ({ path, body, reply, decisions }) => {
      if (!path.endsWith("/answer")) return;
      attempts.push(body);
      if (!healthy)
        await reply({ error: { message: "Private answer error" } }, 503);
      else {
        decisions[0] = {
          ...decisions[0],
          status: "declined",
          answer: body.text,
          revision: 3,
        };
        await reply(decisions[0]);
      }
      return true;
    },
  });
  await page.goto(`${root}/inbox?item=decision:decision-sample`);
  const note = page.getByLabel("Your note (recorded with the decision)");
  await note.fill("Wait for the acceptance evidence.");
  await page.reload();
  await expect(note).toHaveValue("Wait for the acceptance evidence.");
  await page.getByRole("button", { name: "Decline", exact: true }).click();
  await expect(page.locator('[data-mutation-state="retrying"]')).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("pm-decision-retrying.png"),
    fullPage: true,
  });
  await page.reload();
  await expect(page.getByLabel("Saved message", { exact: true })).toHaveValue(
    "Wait for the acceptance evidence.",
  );
  healthy = true;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible({
    timeout: 15000,
  });
  expect(attempts.length).toBeGreaterThan(1);
  for (const attempt of attempts) expect(attempt).toEqual(attempts[0]);
  expect(attempts[0]).toMatchObject({
    actor_id: "actor-sample",
    revision: 2,
    approve: false,
  });
  await page.screenshot({
    path: testInfo.outputPath("pm-decision-sent.png"),
    fullPage: true,
  });
});

for (const checkpoint of ["creation", "message"])
  test(`legacy pending ${checkpoint} body recovers without receipt-key upgrades`, async ({
    page,
  }) => {
    const { calls, conversations } = await setup(page);
    await open(page);
    await page.evaluate(() =>
      Object.defineProperty(navigator, "onLine", {
        configurable: true,
        get: () => false,
      }),
    );
    await send(page);
    await expect(state(page, "pending")).toBeVisible();
    if (checkpoint === "message")
      conversations.push({
        id: "legacy-conversation",
        title: text,
        created_at: new Date().toISOString(),
      });
    const request = await page.evaluate(async (checkpoint) => {
      const queue = await import("/src/lib/mutationOutbox.js");
      const store = await import("/src/lib/localWorkStore.js");
      let entry;
      const unsubscribe = queue.mutationOutbox.subscribe((rows) => {
        entry = structuredClone(rows[0]);
      });
      unsubscribe();
      delete entry.request.creation.replay_key;
      delete entry.request.message.replay_key;
      if (checkpoint === "message")
        entry.receipt = { conversationId: "legacy-conversation" };
      await store.saveLocalWork(entry.scope, "mutation", entry.key, entry, {
        epoch: entry.epoch,
      });
      return entry.request;
    }, checkpoint);
    await page.reload();
    // Recovery drains during boot and may confirm before this page mounts,
    // so no "Working" turn is guaranteed; the drained POSTs are the
    // deterministic signal, and the assertions below check their bodies.
    await expect
      .poll(
        () =>
          calls.filter(
            (call) => call.method === "POST" && call.path.endsWith("/messages"),
          ).length,
      )
      .toBe(1);
    const posts = calls.filter((call) => call.method === "POST");
    const creates = posts.filter((call) => call.path === "/pm/conversations");
    expect(creates).toHaveLength(checkpoint === "creation" ? 1 : 0);
    if (creates.length) expect(creates[0].body).toEqual(request.creation);
    const messages = posts.filter((call) => call.path.endsWith("/messages"));
    expect(messages).toHaveLength(1);
    expect(messages[0].body).toEqual(request.message);
    if (checkpoint === "message")
      expect(messages[0].path).toContain("legacy-conversation");
  });

test("original receipt confirmation refreshes current turn state", async ({
  page,
}, testInfo) => {
  let finish;
  const gate = new Promise((resolve) => {
    finish = resolve;
  });
  const { calls } = await setup(page, {
    handle: async ({ path, method, body, reply, conversations, turns }) => {
      if (method !== "POST" || !path.endsWith("/messages")) return;
      const current = {
        id: "finished-turn",
        text: body.text,
        status: "delivered",
        response: "The PM finished while confirmation was delayed.",
        created_at: new Date().toISOString(),
      };
      turns.push(current);
      await gate;
      await reply({ ...current, status: "sending", response: "" }, 202);
      expect(conversations).toHaveLength(1);
      return true;
    },
  });
  await open(page);
  await send(page);
  await expect(state(page, "sending")).toBeVisible();
  await composer(page).fill("My next unsent question");
  finish();
  await expect(
    page.getByText("The PM finished while confirmation was delayed.", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(state(page, "sent")).toBeVisible();
  await expect(composer(page)).toHaveValue("My next unsent question");
  expect(
    calls.some(
      (call) =>
        call.method === "GET" && call.path.startsWith("/pm/conversations/"),
    ),
  ).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("pm-receipt-sent.png"),
    fullPage: true,
  });
});

test("editing a rejected creation uses fresh creation and message keys", async ({
  page,
}) => {
  let reject = true;
  const { calls } = await setup(page, {
    handle: async ({ path, method, reply }) => {
      if (reject && method === "POST" && path === "/pm/conversations") {
        await reply(
          {
            error: {
              code: "conflict",
              message: "The original context changed.",
            },
          },
          409,
        );
        return true;
      }
    },
  });
  await open(page);
  await send(page);
  await expect(state(page, "attention")).toBeVisible();
  await page.reload();
  await page.getByRole("button", { name: "Edit message", exact: true }).click();
  await composer(page).fill("A corrected first question");
  reject = false;
  await page.getByRole("button", { name: "Send message", exact: true }).click();
  await expect(page.getByText(/^Working/)).toBeVisible();
  const creates = calls.filter(
    (call) => call.method === "POST" && call.path === "/pm/conversations",
  );
  expect(creates).toHaveLength(2);
  expect(creates[1].body.request_key).not.toBe(creates[0].body.request_key);
  expect(creates[1].body.replay_key).not.toBe(creates[0].body.replay_key);
  expect(creates[1].body.title).toBe("A corrected first question");
});

test("storage denial blocks chat sends and keeps the complete question visible", async ({
  page,
  context,
}, testInfo) => {
  await context.addInitScript(() =>
    Object.defineProperty(window, "indexedDB", { value: undefined }),
  );
  const { calls } = await setup(page);
  await open(page);
  await page.evaluate(() => {
    const set = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key.startsWith("anx:local-work:v1:"))
        throw new DOMException("Denied", "QuotaExceededError");
      return set.call(this, key, value);
    };
  });
  await composer(page).fill(text);
  const warning = page.locator("[data-local-save-warning]");
  await expect(warning).toBeVisible();
  await warning.getByRole("button", { name: "Dismiss" }).click();
  await page.getByRole("button", { name: "Send message", exact: true }).click();
  await expect(state(page, "attention")).toBeVisible();
  await expect(page.getByLabel("Saved message", { exact: true })).toHaveValue(
    text,
  );
  expect(calls.filter((call) => call.method === "POST")).toHaveLength(0);
  await page.screenshot({
    path: testInfo.outputPath("pm-receipt-attention.png"),
    fullPage: true,
  });
});

test("retrying a failed turn creates a fresh intent each time", async ({
  page,
}) => {
  const { calls } = await setup(page, {
    handle: async ({ path, method, body, reply, turns }) => {
      if (method !== "POST" || !path.endsWith("/messages")) return;
      const turn = {
        id: `failed-${turns.length}`,
        text: body.text,
        status: "failed",
        failure: "The PM did not finish.",
        created_at: new Date().toISOString(),
      };
      turns.push(turn);
      await reply(turn, 202);
      return true;
    },
  });
  await open(page);
  await send(page);
  const retry = page
    .getByRole("button", { name: "Retry", exact: true })
    .first();
  await expect(retry).toBeVisible();
  await retry.click();
  await expect
    .poll(() => calls.filter((call) => call.path.endsWith("/messages")).length)
    .toBe(2);
  await expect(page.locator("[data-unsent-indicator]")).toHaveCount(0);
  await retry.click();
  await expect
    .poll(() => calls.filter((call) => call.path.endsWith("/messages")).length)
    .toBe(3);
  const bodies = calls
    .filter((call) => call.path.endsWith("/messages"))
    .map((call) => call.body);
  expect(new Set(bodies.map((body) => body.request_key)).size).toBe(3);
  expect(new Set(bodies.map((body) => body.replay_key)).size).toBe(3);
  expect(bodies.every((body) => body.text === text)).toBe(true);
});

for (const step of ["creation", "message"])
  test(`principal replacement clears both tabs and fences a late ${step} result`, async ({
    page,
    context,
  }) => {
    let release;
    const gate = new Promise((resolve) => {
      release = resolve;
    });
    const attempts = [];
    const handle = async ({ path, method, body, reply }) => {
      if (method !== "POST") return;
      attempts.push({ path, body });
      if (
        (step === "creation" && path === "/pm/conversations") ||
        (step === "message" && path.endsWith("/messages"))
      ) {
        await gate;
        if (step === "creation")
          await reply({ id: "private-late-conversation" }, 201);
        else await reply({ error: { message: "Private late failure" } }, 503);
        return true;
      }
    };
    await setup(page, { handle });
    await open(page);
    await composer(page).fill(text);
    const oldScope = await page.evaluate(async () =>
      (await import("/src/lib/readerScope.js")).readerScopeKey(),
    );
    const other = await context.newPage();
    await setup(other, { handle });
    await open(other);
    await expect(composer(other)).toHaveValue(text);
    try {
      await page
        .getByRole("button", { name: "Send message", exact: true })
        .click();
      await expect
        .poll(() =>
          attempts.some((call) =>
            step === "creation"
              ? call.path === "/pm/conversations"
              : call.path.endsWith("/messages"),
          ),
        )
        .toBe(true);
      await expect(
        other.getByLabel("Saved message", { exact: true }),
      ).toHaveValue(text);
      await page.evaluate(async () => {
        const auth = await import("/src/lib/authSession.js");
        auth.authenticatedAgent.set({
          agent_id: "replacement-owner",
          actor_id: "replacement-actor",
        });
      });
      await expect(
        other.getByLabel("Saved message", { exact: true }),
      ).toHaveCount(0);
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
      await expect(composer(other)).toHaveValue("");
      expect(
        attempts.filter((call) => call.path.endsWith("/messages")),
      ).toHaveLength(step === "creation" ? 0 : 1);
    } finally {
      release();
    }
  });
