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
  await expect(other.getByText(/^Working/)).toBeVisible();
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
  test(`lost ${failure} confirmation resumes exact keys after reload`, async ({
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
      if (!receipts.has(body.request_key))
        receipts.set(
          body.request_key,
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
      const result = receipts.get(body.request_key);
      const rows = creation ? conversations : turns;
      if (!rows.some((row) => row.id === result.id)) rows.push(result);
      if (!healthy && (failure === "creation") === creation) {
        await reply({ error: { message: "Private upstream failure" } }, 524);
      } else await reply(result, creation ? 201 : 202);
      return true;
    };
    await setup(page, { handle });
    await open(page);
    await send(page);
    await expect(state(page, "retrying")).toBeVisible();
    await expect(page.locator(".pm-composer details[open]")).toHaveCount(0);
    await page.screenshot({
      path: testInfo.outputPath(`pm-${failure}-retrying.png`),
      fullPage: true,
    });
    const original = attempts.find((row) =>
      failure === "creation"
        ? row.path === "/pm/conversations"
        : row.path.endsWith("/messages"),
    );
    expect(original.body.actor_id).toBe("actor-sample");
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
    for (const attempt of sameStep) expect(attempt.body).toEqual(original.body);
    await page.screenshot({
      path: testInfo.outputPath(`pm-${failure}-confirmed.png`),
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
