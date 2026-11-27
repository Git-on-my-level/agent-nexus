import { expect, test } from "@playwright/test";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };

/**
 * Single skips an older open tab marked "Unknown saved operation" (it lacked
 * Skip) all go out after the reader reloads a current tab, through the real
 * browser outbox: IndexedDB, the journal and the send lock. Some asks have
 * closed meanwhile; core answers 409 for those and they clear too.
 */
const ACTOR = "actor-stranded-skip-e2e";
const LIST = "/o/local/w/local/decide";
const N = 12;

const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

test.use({ viewport: { width: 1280, height: 900 } });

test("every stranded skip is sent after a reload", async ({
  page,
  context,
}) => {
  test.setTimeout(120_000);
  await context.addInitScript((actor) => {
    localStorage.setItem("anx_ui_actor_id:local", actor);
    localStorage.setItem("workspaceTourSeen.local", "1");
  }, ACTOR);
  await context.route(/\/actors(?:\?.*)?$/, (route) =>
    json(route, {
      actors: [{ id: ACTOR, display_name: "Reader", tags: ["human"] }],
    }),
  );
  await context.route(
    /\/(?:stream\/(?:events|inbox)|inbox\/stream)(?:\?.*)?$/,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "",
      }),
  );
  await context.route(
    (url) => url.pathname === "/brief",
    (route) =>
      json(route, { ...brief, generated_at: new Date().toISOString() }),
  );
  let phase = 1;
  const sent = [];
  const batches = [];
  await context.route(/\/inbox\/responses$/, (route) => {
    if (phase === 1)
      return json(route, { error: { code: "bad", message: "bad" } }, 400);
    const { items } = route.request().postDataJSON();
    batches.push(items.length);
    return json(route, {
      results: items.map((_, index) => ({ index, status: "responded" })),
    });
  });
  await context.route(/\/inbox\/[^/]+\/respond$/, async (route) => {
    const id = decodeURIComponent(
      new URL(route.request().url()).pathname.split("/").at(-2),
    );
    if (phase === 1)
      return json(route, { error: { code: "bad", message: "bad" } }, 400);
    const n = sent.length;
    sent.push({ id, at: Date.now() });
    await new Promise((r) => setTimeout(r, 50 + ((n * 397) % 600)));
    if (n % 3 === 0)
      return json(
        route,
        { error: { code: "conflict", message: "Ask is no longer open" } },
        409,
      ).catch(() => {});
    return json(route, { event: { id: `response-${id}` } }, 201).catch(
      () => {},
    );
  });

  await page.goto(LIST);
  await page.locator('[data-decide-pick="ask-93"]').check();
  await page.locator("[data-decide-skip-selected]").click();
  // Wait for the batch to fail into attention.
  await expect
    .poll(
      () =>
        page.evaluate(async () => {
          const db = await new Promise((r) => {
            const q = indexedDB.open("anx-local-work-v1");
            q.onsuccess = () => r(q.result);
          });
          const all = await new Promise((r) => {
            const q = db.transaction("work").objectStore("work").getAll();
            q.onsuccess = () => r(q.result);
          });
          db.close();
          return all.filter(
            (x) =>
              x.kind === "mutation" &&
              !x.deleted &&
              x.value?.state === "attention",
          ).length;
        }),
      { timeout: 30_000 },
    )
    .toBe(1);

  // Clone it into N single skips an older tab gave up on.
  await page.evaluate(async (n) => {
    const db = await new Promise((r) => {
      const q = indexedDB.open("anx-local-work-v1");
      q.onsuccess = () => r(q.result);
    });
    const all = await new Promise((r) => {
      const q = db.transaction("work").objectStore("work").getAll();
      q.onsuccess = () => r(q.result);
    });
    const base = all.find((x) => x.kind === "mutation" && !x.deleted);
    const now = Date.now() - 3 * 3600_000;
    const records = [];
    for (let i = 0; i < n; i += 1) {
      const key = crypto.randomUUID();
      const itemId = `stranded-${i}`;
      const value = {
        ...base.value,
        key,
        target: `skip:${itemId}`,
        draftTarget: `skip:${itemId}`,
        operation: "inbox.skip",
        request: {
          item_id: itemId,
          body: {
            ...base.value.request.items[0].body,
            idempotency_key: crypto.randomUUID(),
          },
          quiet_key: crypto.randomUUID(),
        },
        receipt: undefined,
        state: "attention",
        attempts: 1,
        nextAt: now + 2000,
        createdAt: now + i,
        failure: {
          state: "attention",
          status: 400,
          message: "This response needs a correction before it can be sent.",
          detail: "Unknown saved operation",
        },
      };
      records.push({
        ...base,
        key: JSON.stringify([base.scope, "mutation", key]),
        target: key,
        value,
        updatedAt: now + i,
      });
    }
    await new Promise((r, j) => {
      const tx = db.transaction("work", "readwrite");
      const store = tx.objectStore("work");
      store.delete(base.key);
      for (const record of records) store.put(record);
      tx.oncomplete = r;
      tx.onerror = () => j(tx.error);
    });
    db.close();
    for (const k of Object.keys(localStorage))
      if (k.startsWith("anx:local-work:v1:")) localStorage.removeItem(k);
    for (const record of records)
      localStorage.setItem(
        "anx:local-work:v1:" + record.key,
        JSON.stringify(record),
      );
  }, N);

  phase = 2;
  await page.reload();
  await expect.poll(() => sent.length, { timeout: 60_000 }).toBe(N);
  expect(new Set(sent.map((s) => s.id)).size).toBe(N);
  await expect(page.locator("[data-decide-skip-stuck]")).toHaveCount(0);
  expect(batches).toEqual([]);
});
