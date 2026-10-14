import { expect, test } from "@playwright/test";
import { setup } from "./helpers/durable-discussion.js";

const thread = "thread-durable-comment";
const id = "canonical-topic";
const href = `/o/local/w/local/threads/${thread}?tab=about`;
const originalStamp = "2026-10-13T00:00:00Z";
const newerStamp = "2026-10-13T02:00:00Z";
const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

async function prepare(context, send = null, initial = {}) {
  await setup(context, (route) => json(route, { events: [] }));
  let topic = {
    id,
    thread_id: thread,
    title: "Original topic",
    summary: "Original summary",
    updated_at: originalStamp,
    provenance: {
      sources: ["event:seed", "inferred"],
      notes: "Original source notes",
    },
    ...initial,
  };
  const writes = [];
  await context.route(`**/${thread}/workspace`, (route) =>
    json(route, {
      thread: {
        id: thread,
        topic_ref: `topic:${id}`,
        title: topic.title,
        summary: topic.summary,
        type: "process",
        status: "active",
        updated_at: "2026-10-13T01:00:00Z",
        provenance: { sources: ["inferred"] },
      },
      context: {
        documents: [],
        recent_events: [],
        key_artifacts: [],
        open_cards: [],
      },
    }),
  );
  await context.route(`**/topics/${id}`, async (route) => {
    if (route.request().method() === "GET") return json(route, { topic });
    const body = route.request().postDataJSON();
    writes.push(body);
    if (send) return send(route, body);
    topic = { ...topic, ...body.patch, updated_at: newerStamp };
    return json(route, { topic });
  });
  return {
    writes,
    latest: (patch = {}) => {
      topic = { ...topic, ...patch, updated_at: newerStamp };
    },
    commitPatch: (patch) => {
      topic = { ...topic, ...patch, updated_at: newerStamp };
      return topic;
    },
  };
}
const title = (page) => page.getByLabel("Title", { exact: true });
const summary = (page) => page.getByLabel("Summary", { exact: true });
const save = (page) =>
  page.getByRole("button", { name: "Save changes", exact: true });
async function edit(page) {
  await page.goto(href);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await title(page).fill("Authored topic");
  await summary(page).fill("Complete authored summary");
}

test("topic draft, original canonical version and explicit discard survive navigation and reload", async ({
  page,
  context,
}) => {
  const { writes } = await prepare(context);
  await edit(page);
  await page
    .getByRole("button", { name: "Close editor", exact: true })
    .first()
    .click();
  await page.reload();
  await expect(title(page)).toHaveValue("Authored topic");
  await expect(summary(page)).toHaveValue("Complete authored summary");
  await page.getByRole("tab", { name: "Messages", exact: true }).click();
  await page.getByRole("tab", { name: "About", exact: true }).click();
  await expect(title(page)).toHaveValue("Authored topic");
  await save(page).click();
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(writes).toHaveLength(1);
  expect(writes[0].if_updated_at).toBe(originalStamp);
  expect(writes[0].actor_id).toBe("actor-comment");
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await title(page).fill("Discard this explicit draft");
  await page
    .getByRole("button", { name: "Discard draft", exact: true })
    .click();
  await page.reload();
  await expect(title(page)).toHaveCount(0);
});

for (const failure of ["524", "network"]) {
  test(`topic lost ${failure} confirmation replays one original request across reload and two tabs`, async ({
    page,
    context,
  }, testInfo) => {
    let healthy = false;
    const committed = new Map();
    const { writes } = await prepare(context, (route, body) => {
      if (!committed.has(body.request_key))
        committed.set(body.request_key, {
          topic: { id, ...body.patch, updated_at: newerStamp },
        });
      if (healthy) return json(route, committed.get(body.request_key));
      return failure === "network"
        ? route.abort("failed")
        : json(
            route,
            { error: { message: "Private timeout diagnostics" } },
            524,
          );
    });
    await edit(page);
    await save(page).click();
    await expect(
      page.locator('[data-mutation-state="retrying"]'),
    ).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath(`topic-${failure}-retrying.png`),
      fullPage: true,
    });
    await page.reload();
    await expect(title(page)).toHaveValue("Authored topic");
    const other = await context.newPage();
    await other.goto(href);
    healthy = true;
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await other.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect.poll(() => writes.length).toBeGreaterThan(1);
    await expect(page.locator('[data-mutation-state="retrying"]')).toHaveCount(
      0,
    );
    expect(committed.size).toBe(1);
    for (const body of writes) expect(body).toEqual(writes[0]);
    expect(writes[0].if_updated_at).toBe(originalStamp);
  });
}

test("offline topic submission survives closing its original tab", async ({
  page,
  context,
}, testInfo) => {
  const { writes } = await prepare(context);
  await edit(page);
  await page.evaluate(() =>
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => false,
    }),
  );
  await save(page).click();
  await expect(page.locator('[data-mutation-state="pending"]')).toBeVisible();
  await expect(save(page)).toHaveText("Save changes");
  await page.screenshot({
    path: testInfo.outputPath("topic-offline.png"),
    fullPage: true,
  });
  expect(writes).toHaveLength(0);
  await page.close();
  const other = await context.newPage();
  await other.goto(href);
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0].patch.summary).toBe("Complete authored summary");
});

test("topic conflict preserves newer edits, hides diagnostics, and rebases only on explicit action", async ({
  page,
  context,
}, testInfo) => {
  let latest = originalStamp;
  const { writes, latest: updateCanonical } = await prepare(
    context,
    (route, body) =>
      body.if_updated_at !== latest
        ? json(route, { error: { message: "Private conflict details" } }, 409)
        : json(route, { topic: { id, ...body.patch, updated_at: newerStamp } }),
  );
  await edit(page);
  latest = newerStamp;
  updateCanonical();
  await save(page).click();
  await expect(page.locator('[data-mutation-state="attention"]')).toBeVisible();
  await expect(page.locator('[data-mutation-state="attention"]')).toContainText(
    "This topic changed. Your edits are saved.",
  );
  await summary(page).fill("Newer summary written after conflict");
  await page.reload();
  await expect(summary(page)).toHaveValue(
    "Newer summary written after conflict",
  );
  await expect(
    page.getByText("Private conflict details", { exact: true }),
  ).not.toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("topic-conflict.png"),
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Use latest version", exact: true })
    .click();
  await expect(summary(page)).toHaveValue(
    "Newer summary written after conflict",
  );
  await save(page).click();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1].if_updated_at).toBe(newerStamp);
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  expect(writes[1].request_key).not.toBe(writes[0].request_key);
});

test("confirmed topic save refreshes the header after leaving About", async ({
  page,
  context,
}) => {
  let finish;
  const gate = new Promise((resolve) => {
    finish = resolve;
  });
  const prepared = await prepare(context, async (route, body) => {
    await gate;
    return json(route, {
      topic: prepared.commitPatch(body.patch),
    });
  });
  await edit(page);
  await save(page).click();
  await expect(page.locator('[data-mutation-state="sending"]')).toBeVisible();
  await page.getByRole("tab", { name: "Messages", exact: true }).click();
  finish();
  await expect(
    page.getByRole("heading", { name: "Authored topic" }),
  ).toBeVisible();
});

test("confirmed topic send preserves newer text including an empty summary", async ({
  page,
  context,
}) => {
  let finish;
  const gate = new Promise((resolve) => {
    finish = resolve;
  });
  await prepare(context, async (route, body) => {
    await gate;
    return json(route, {
      topic: { id, ...body.patch, updated_at: newerStamp },
    });
  });
  await edit(page);
  await save(page).click();
  await expect(page.locator('[data-mutation-state="sending"]')).toBeVisible();
  await summary(page).fill("");
  finish();
  await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
  await expect(summary(page)).toHaveValue("");
  await page.reload();
  await expect(summary(page)).toHaveValue("");
});

test("total device storage denial retains visible topic work and prevents transport", async ({
  page,
  context,
}) => {
  await context.addInitScript(() => {
    // Establish the fixture's identity and tour preference in this same
    // script before denying storage. Separate init-script order is unspecified.
    localStorage.setItem("anx_ui_actor_id:local", "actor-comment");
    localStorage.setItem("workspaceTourSeen.local", "1");
    Storage.prototype.setItem = () => {
      throw new Error("Storage denied");
    };
    window.indexedDB.open = () => {
      throw new Error("Storage denied");
    };
  });
  const { writes } = await prepare(context);
  await edit(page);
  await save(page).click();
  await expect(page.locator('[data-mutation-state="attention"]')).toBeVisible();
  await expect(title(page)).toHaveValue("Authored topic");
  await expect(summary(page)).toHaveValue("Complete authored summary");
  expect(writes).toHaveLength(0);
  await expect(
    page.getByRole("button", { name: "Copy text", exact: true }).first(),
  ).toBeVisible();
});

test("opening the editor during delayed device hydration cannot replace its authored draft", async ({
  page,
  context,
}) => {
  const { writes } = await prepare(context);
  await edit(page);
  await context.addInitScript(() => {
    const original = indexedDB.open.bind(indexedDB);
    indexedDB.open = (...args) => {
      const request = original(...args);
      Object.defineProperty(request, "onsuccess", {
        set(handler) {
          request.addEventListener("success", (event) =>
            setTimeout(() => handler?.call(request, event), 3000),
          );
        },
      });
      return request;
    };
  });
  await page.reload();
  const open = page.getByRole("button", { name: "Edit", exact: true });
  await expect(open).toBeDisabled();
  // Programmatic dispatch exercises both the handler and its bubbling click
  // checkpoint while ordinary readers see a disabled control.
  await open.dispatchEvent("click");
  await expect(title(page)).toHaveValue("Authored topic");
  await expect(summary(page)).toHaveValue("Complete authored summary");
  await save(page).click();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0].if_updated_at).toBe(originalStamp);
  expect(writes[0].patch.title).toBe("Authored topic");
});

test("principal replacement clears topic drafts across tabs and fences a late send", async ({
  page,
  context,
}) => {
  let finish;
  const gate = new Promise((resolve) => {
    finish = resolve;
  });
  const { writes } = await prepare(context, async (route) => {
    await gate;
    return route.abort("timedout");
  });
  await edit(page);
  await save(page).click();
  await expect.poll(() => writes.length).toBe(1);
  const other = await context.newPage();
  await other.goto(href);
  await expect(summary(other)).toHaveValue("Complete authored summary");
  await summary(other).fill("PRIVATE newer topic draft");
  await page.evaluate(async () => {
    const auth = await import("/src/lib/authSession.js");
    auth.authenticatedAgent.set({
      agent_id: "replacement-owner",
      actor_id: "replacement-actor",
    });
  });
  await expect(title(other)).toHaveCount(0);
  await expect(summary(other)).toHaveCount(0);
  finish();
  await expect(other.locator("[data-unsent-indicator]")).toHaveCount(0);
  expect(writes).toHaveLength(1);
});

test("failed explicit rebase keeps the exact original request recoverable", async ({
  page,
  context,
}) => {
  let rejectRead = false;
  const { writes } = await prepare(context, (route) =>
    json(route, { error: { message: "Version conflict" } }, 409),
  );
  await context.route(`**/topics/${id}`, (route) =>
    rejectRead && route.request().method() === "GET"
      ? json(route, { error: "Private read failure" }, 503)
      : route.fallback(),
  );
  await edit(page);
  await save(page).click();
  await expect(page.locator('[data-mutation-state="attention"]')).toBeVisible();
  rejectRead = true;
  await page
    .getByRole("button", { name: "Use latest version", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Your edits are still here",
  );
  await expect(page.locator('[data-mutation-state="attention"]')).toBeVisible();
  await expect(summary(page)).toHaveValue("Complete authored summary");
  await page.getByRole("button", { name: "Retry send", exact: true }).click();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1]).toEqual(writes[0]);
});

for (const editedField of ["summary", "title"]) {
  test(`explicit rebase preserves concurrent changes to untouched fields when editing ${editedField}`, async ({
    page,
    context,
  }) => {
    let requireLatest = false;
    let api;
    api = await prepare(
      context,
      (route, body) => {
        if (requireLatest && body.if_updated_at !== newerStamp)
          return json(route, { error: { message: "Version conflict" } }, 409);
        return json(route, { topic: api.commitPatch(body.patch) });
      },
      { title: "  Original topic  ", summary: "Original summary\n\n" },
    );
    await page.goto(href);
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    const input = editedField === "summary" ? summary(page) : title(page);
    await input.fill("My authored change");
    const untouchedField = editedField === "summary" ? "title" : "summary";
    api.latest({ [untouchedField]: "Concurrent canonical change" });
    requireLatest = true;
    await save(page).click();
    await expect(
      page.locator('[data-mutation-state="attention"]'),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Use latest version", exact: true })
      .click();
    await expect(page.locator('[data-mutation-state="attention"]')).toHaveCount(
      0,
    );
    // Reload after rebasing proves the edited-from content base is durable.
    await page.reload();
    await expect(input).toHaveValue("My authored change");
    await save(page).click();
    await expect(page.locator('[data-mutation-state="sent"]')).toBeVisible();
    expect(api.writes).toHaveLength(2);
    expect(api.writes[0].if_updated_at).toBe(originalStamp);
    expect(api.writes[1].if_updated_at).toBe(newerStamp);
    expect(api.writes[1].request_key).not.toBe(api.writes[0].request_key);
    expect(api.writes[1].patch).toEqual({
      [editedField]: "My authored change",
    });
    const canonical = await page.evaluate(async (topicId) => {
      const { coreClient } = await import("/src/lib/coreClient.js");
      return (await coreClient.getTopic(topicId)).topic;
    }, id);
    expect(canonical[untouchedField]).toBe("Concurrent canonical change");
    expect(canonical.provenance).toEqual({
      sources: ["event:seed", "inferred"],
      notes: "Original source notes",
    });
  });
}
