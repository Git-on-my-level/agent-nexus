import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { setup } from "./helpers/pm-work.js";

const root = "/o/local/w/local";
const stamp = (hours = 0) =>
  new Date(Date.now() - hours * 3600000).toISOString();

test("a reply that proposes decisions shows answerable rows linked to Inbox", async ({
  page,
}) => {
  const { conversations, turns } = await setup(page);
  conversations.push({
    id: "conversation-sample",
    title: "Weekly review",
    created_at: stamp(1),
  });
  turns.push({
    id: "turn-sample",
    text: "What needs my decision?",
    response:
      "One item needs you: the release note handoff. Proposed as decision:decision-sample.",
    status: "delivered",
    created_at: stamp(1),
    evidence_refs: ["card:release", "decision:decision-sample"],
  });
  await page.goto(`${root}/pm?conversation=conversation-sample`);
  const list = page.getByRole("list", {
    name: "Decisions proposed in this reply",
  });
  await expect(list).toBeVisible();
  const row = list.getByRole("listitem");
  await expect(row).toContainText("Update the sample handoff note");
  // The row names the task it concerns and links to it, not the raw ref.
  await expect(
    row.getByRole("link", { name: "Release the sample workspace" }),
  ).toHaveAttribute("href", "/o/local/w/local/tasks/card%3Arelease");
  await expect(row.getByRole("link", { name: "Answer" })).toHaveAttribute(
    "href",
    "/o/local/w/local/inbox?item=decision:decision-sample",
  );
});

test("board and table preserve source states and show the same commitments", async ({
  page,
}) => {
  const { calls } = await setup(page);
  await page.goto(`${root}/tasks`);
  await expect(
    page.getByRole("link", {
      name: "Release the sample workspace",
      exact: true,
    }),
  ).toBeVisible();
  const tableRefs = await page
    .locator("[data-work-ref]")
    .evaluateAll((rows) => rows.map((row) => row.dataset.workRef).sort());
  // The status cell; the two-line phone row repeats it, hidden on desktop.
  await expect(
    page.getByRole("cell").getByText("Custom waiting state", { exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Board", exact: true }).click();
  await expect(
    page.getByRole("region", { name: "Task board grouped by phase" }),
  ).toBeVisible();
  expect(
    await page
      .locator("[data-work-ref]")
      .evaluateAll((rows) => rows.map((row) => row.dataset.workRef).sort()),
  ).toEqual(tableRefs);
  // Source now lives inside the single Filters disclosure.
  await page
    .getByRole("group")
    .filter({ hasText: "Filters" })
    .locator("summary")
    .click();
  await page.getByLabel("Source", { exact: true }).selectOption("github");
  await expect(page.locator("[data-work-ref]")).toHaveCount(1);
  expect(calls.filter((call) => call.method !== "GET")).toHaveLength(0);
});

test("board pointer drag lifts the card, then moves it to another phase", async ({
  page,
}) => {
  const { calls } = await setup(page);
  page.on("dialog", (dialog) => dialog.accept());
  await page.goto(`${root}/tasks?view=board`);
  const board = page.getByRole("region", {
    name: "Task board grouped by phase",
  });
  await expect(board).toBeVisible();
  const card = page.locator('[data-work-ref="card:release"]');
  // A labelled <section> is exposed as a region.
  const target = page.getByRole("region", { name: "In progress" });
  await expect(card).toBeVisible();
  await expect(target).toBeVisible();
  const from = await card.boundingBox();
  const to = await target.boundingBox();
  expect(from).toBeTruthy();
  expect(to).toBeTruthy();
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x + 40, to.y + 80, { steps: 12 });
  await expect(page.locator("[data-work-drag-overlay]")).toBeVisible();
  await page.mouse.up();
  await expect
    .poll(() =>
      calls.some(
        (call) => call.path === "/pm/decisions" && call.method === "POST",
      ),
    )
    .toBe(true);
});

test("board card click still opens the task after pointer-drag handlers are wired", async ({
  page,
}) => {
  await setup(page);
  await page.goto(`${root}/tasks?view=board`);
  await page
    // The card link's name also carries its meta line.
    .getByRole("link", { name: /^Document the sample outcome/ })
    .click();
  await expect(page).toHaveURL(/\/tasks\/card%3Adocs/);
});

test("failed refresh retains last-good evidence and never promotes a claim to verified", async ({
  page,
}) => {
  const { calls } = await setup(page);
  await page.goto(`${root}/tasks/card%3Arelease`);
  await expect(
    page.getByRole("heading", { name: "Release the sample workspace" }),
  ).toBeVisible();
  // One source line: what was read, how often, when; the claim badge is on
  // it, and the read-by-read history is a disclosure.
  const sourceLine = page.locator("[data-evidence-source]");
  await expect(sourceLine).toContainText("GitHub #12 · 1 observation");
  await expect(
    page.getByText("Reported claim", { exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByText("Serving revision not observed")).toBeVisible();
  await expect(
    page.getByText("Verified evidence", { exact: true }),
  ).toHaveCount(0);
  // The refresh button names the source it will read.
  await page.getByRole("button", { name: "Check GitHub now" }).click();
  await expect(
    page.getByText(
      "Refresh queued. Evidence changes only after a reader reports back.",
    ),
  ).toBeVisible();
  expect(
    calls.filter((call) => call.method === "POST").map((call) => call.path),
  ).toEqual(["/work/card:release/refresh"]);
});

test("a live task event re-reads the list; a failed re-read keeps visible work", async ({
  page,
}) => {
  test.setTimeout(60_000);
  let fail = false;
  await setup(page, {
    handle: async ({ path, method, reply }) => {
      if (fail && path === "/work" && method === "GET") {
        await reply(
          { error: { message: "Source projection unavailable" } },
          503,
        );
        return true;
      }
    },
  });
  // Registered first: Playwright tries the newest route first, and the
  // stream URL below also contains "/events?".
  await page.route("**/events?**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: [] }),
    }),
  );
  // One waiter per live connection. Claiming the first stream bound the
  // event to a request Playwright often aborts during navigation, so later
  // connections never saw card_moved. Aborted connections leave the queue.
  const deferred = () => {
    let resolve;
    let reject;
    const promise = new Promise((res, rej) => {
      resolve = res;
      reject = rej;
    });
    return { promise, resolve, reject };
  };
  const liveWaiters = [];
  let queuedLive = 0;
  const release = () => {
    const waiter = liveWaiters.shift();
    if (waiter) waiter.resolve();
    else queuedLive += 1;
  };
  let connected;
  const streamReady = new Promise((resolve) => {
    connected = resolve;
  });
  await page.route("**/stream/events**", async (route) => {
    const request = route.request();
    const waiter = deferred();
    if (queuedLive > 0) {
      queuedLive -= 1;
      waiter.resolve();
    } else liveWaiters.push(waiter);
    connected();
    const onFailed = (failed) => {
      if (failed !== request) return;
      page.off("requestfailed", onFailed);
      const index = liveWaiters.indexOf(waiter);
      if (index >= 0) liveWaiters.splice(index, 1);
      waiter.reject(new Error("stream aborted"));
    };
    page.on("requestfailed", onFailed);
    try {
      await waiter.promise;
    } catch {
      await route.abort().catch(() => {});
      return;
    }
    page.off("requestfailed", onFailed);
    const event = {
      id: "evt-live-1",
      type: "card_moved",
      ts: new Date().toISOString(),
      refs: ["card:docs"],
    };
    await route
      .fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: `id: evt-live-1\nevent: event\ndata: ${JSON.stringify({ event })}\n\n`,
      })
      .catch(() => {});
  });
  await page.goto(`${root}/tasks`);
  await expect(page.locator("[data-work-ref]")).toHaveCount(3);
  await expect(page.getByRole("button", { name: "Reload" })).toHaveCount(0);
  await streamReady;
  fail = true;
  release();
  await expect(page.getByText("Reconnecting…", { exact: true })).toBeVisible({
    timeout: 40_000,
  });
  await expect(page.getByRole("alert")).toHaveCount(0);
  await expect(page.locator("[data-work-ref]")).toHaveCount(3);
  await expect(
    page.getByText("Showing the previously loaded records.", { exact: false }),
  ).toBeVisible({ timeout: 40_000 });
  await expect(page.locator("[data-work-ref]")).toHaveCount(3);
});

test("PM retains failed draft and retries the same message intent without claiming completion", async ({
  page,
}) => {
  let attempts = 0;
  const { calls } = await setup(page, {
    handle: async ({ path, reply }) => {
      if (path.endsWith("/messages") && attempts++ === 0) {
        await reply({ error: { message: "PM bridge unavailable" } }, 503);
        return true;
      }
    },
  });
  await page.goto(`${root}/pm?work_ref=card%3Arelease`);
  await page
    .getByLabel("Message PM", { exact: true })
    .fill("What is still unverified?");
  await page.getByRole("button", { name: "Send message", exact: true }).click();
  await expect(
    page.locator('.pm-composer [data-mutation-state="retrying"]'),
  ).toBeVisible();
  await expect(page.getByLabel("Saved message", { exact: true })).toHaveValue(
    "What is still unverified?",
  );
  await expect(page.locator(".pm-composer details[open]")).toHaveCount(0);
  // A queued turn is a transient state, so it reads as one live
  // "Working · <elapsed>" row rather than the durable badge this page used to
  // show — and rather than the last step's own label, which repeated a word
  // the step list below it already carried.
  await expect(page.getByText(/^Working/)).toBeVisible();
  const sends = calls.filter((call) => call.path.endsWith("/messages"));
  expect(sends).toHaveLength(2);
  expect(sends[0].body.request_key).toBe(sends[1].body.request_key);
  expect(
    calls.filter(
      (call) => call.path === "/pm/conversations" && call.method === "POST",
    ),
  ).toHaveLength(1);
});

test("answer and failed delivery remain separately inspectable", async ({
  page,
}) => {
  const { calls } = await setup(page, {
    handle: async ({ path, reply, actions }) => {
      if (path.endsWith("/dispatch")) {
        actions[0] = {
          ...actions[0],
          status: "failed",
          receipt: { detail: "Source delivery failed" },
        };
        await reply({ error: { message: "Source delivery failed" } }, 503);
        return true;
      }
    },
  });
  await page.goto(`${root}/inbox?item=decision:decision-sample`);
  await page
    .getByLabel("Your note (recorded with the decision)")
    .fill("Approved for the sample note only");
  await page.getByRole("button", { name: "Approve" }).click();
  await expect(
    page.getByText("Pending delivery", { exact: true }),
  ).toBeVisible();
  expect(calls.filter((call) => call.path.endsWith("/dispatch"))).toHaveLength(
    0,
  );
  await page
    .getByRole("button", { name: "Deliver approved instruction" })
    .click();
  // The receipt shows on the row badge and in the pane; assert the pane.
  await expect(
    page.getByLabel("Selected inbox item").getByText("Failed", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Approved for the sample note only", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Outcome verified", { exact: true })).toHaveCount(
    0,
  );
});

for (const viewport of [
  { width: 390, height: 844 },
  { width: 1440, height: 1000 },
]) {
  test(`work and decision surfaces are accessible at ${viewport.width}px`, async ({
    page,
  }, testInfo) => {
    test.setTimeout(120_000);
    await page.setViewportSize(viewport);
    await setup(page);
    for (const route of [
      { path: "/tasks", name: "tasks" },
      {
        path: "/inbox?item=decision:decision-sample",
        name: "inbox",
      },
      { path: "/pm", name: "pm" },
      { path: "/docs", name: "docs" },
      { path: "/docs/release-runbook", name: "docs-detail" },
      { path: "/integrations", name: "integrations" },
    ]) {
      await page.goto(`${root}${route.path}`);
      // Six cold routes under a loaded dev server. The check is that the
      // heading paints, not that it paints inside the default 10s.
      await expect(page.locator("h1").first()).toBeVisible({ timeout: 20_000 });
      await expect(page.getByText("Loading tasks…")).toHaveCount(0);
      await expect(
        page.getByRole("button", { name: /Loading health|Loading decisions/ }),
      ).toHaveCount(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth + 1,
        ),
      ).toBe(true);
      const violations = (
        await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
          .analyze()
      ).violations;
      expect(violations).toEqual([]);
      await page.screenshot({
        path: testInfo.outputPath(`${route.name}-${viewport.width}.png`),
        fullPage: true,
      });
    }
  });
}

test("the ⌘K palette acts on the task in view and navigates by keyboard", async ({
  page,
}) => {
  const { work } = await setup(page);
  const task = work.find((item) => item.ref === "card:docs");
  task.board_ref = "board:sample";
  task.updated_at = stamp(1);
  const writes = [];
  await page.route("**/boards/**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        board: { ref: "board:sample", updated_at: stamp(1) },
      }),
    }),
  );
  await page.route("**/cards/**", async (route) => {
    const request = route.request();
    writes.push({
      path: decodeURIComponent(new URL(request.url()).pathname),
      method: request.method(),
      body: request.postDataJSON(),
    });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ card: { ref: "card:docs" } }),
    });
  });
  await page.goto(`${root}/tasks/card%3Adocs`);
  await expect(
    page.getByRole("heading", { name: "Document the sample outcome" }),
  ).toBeVisible();

  // M opens the palette on "Move to"; typing narrows, Enter moves.
  await page.keyboard.press("m");
  const palette = page.getByRole("dialog", { name: "Command palette" });
  await expect(palette.getByRole("button", { name: "Move to" })).toBeVisible();
  await expect(
    palette.getByRole("option", { name: "Move to In review" }),
  ).toBeVisible();
  await page.keyboard.type("review");
  await page.keyboard.press("Enter");
  await expect(palette).toHaveCount(0);
  await expect(
    page.getByText("Moved “Document the sample outcome” to In review."),
  ).toBeVisible();
  const move = writes.find((write) => write.path.endsWith("/move"));
  expect(move?.body).toMatchObject({ column_key: "review" });

  // ⌘K lists actions on this task, then destinations with their shortcuts.
  await page.keyboard.press("ControlOrMeta+k");
  await expect(palette.getByRole("option", { name: /Move to…/ })).toBeVisible();
  await expect(palette.getByRole("option", { name: /^Inbox/ })).toContainText(
    "GI",
  );
  await page.keyboard.type("audit");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/events$/);

  // G then D goes to Docs from anywhere outside a text field.
  await page.keyboard.press("g");
  await page.keyboard.press("d");
  await expect(page).toHaveURL(/\/docs$/);
});

test("PM pins multiple refs in history and resolves answer chips with streamed activity", async ({
  page,
}) => {
  const { conversations, turns } = await setup(page);
  conversations.push({
    id: "context-chat",
    title: "Release question",
    work_ref: "card:release",
    context_refs: ["card:release", "topic:sample"],
    created_at: stamp(),
  });
  turns.push({
    id: "context-turn",
    text: "What is this task?",
    status: "sending",
    claimed: true,
    created_at: stamp(),
    deadline: stamp(-1),
    activity: [
      {
        sequence: 1,
        kind: "tool",
        label: "Reading the task",
        target: "card:release",
      },
    ],
    partial_response: "The release is in review.",
  });
  await page.goto(`${root}/pm?conversation=context-chat`);
  await expect(page.getByLabel("Conversation context")).toContainText(
    "Release the sample workspace",
  );
  await expect(page.getByLabel("Conversation context")).toContainText(
    "Sample project",
  );
  await expect(page.getByLabel("Conversation context")).toContainText("review");
  await expect(page.getByText("Draft answer", { exact: true })).toBeVisible();
  // One line, with the steps behind it: the summary says "Working", not the
  // last step's own label, and the step list is closed until it is asked for.
  const run = page.locator('[data-pm-run="pending"]');
  await expect(run.locator("> summary")).toContainText("Working");
  await expect(page.getByLabel("Turn activity")).toBeHidden();
  await run.locator("> summary").click();
  await expect(page.getByLabel("Turn activity")).toContainText(
    "Reading the task",
  );
  await expect(page.getByLabel("Turn activity")).toContainText("card:release");
  turns[0].status = "delivered";
  turns[0].response = "Read card:release and topic:sample.";
  // Wait for the five-second conversation poll before inspecting mounted chips.
  await expect(page.locator(".pm-response")).toContainText("Read", {
    timeout: 10000,
  });
  await expect(
    page.locator(".pm-response [data-anx-ref='card:release']"),
  ).toHaveText(/Release the sample workspace/);
  await expect(
    page.locator(".pm-response [data-anx-ref='topic:sample']"),
  ).toHaveText(/Sample project/);
  await expect(page.locator(".pm-response")).not.toContainText("not found");
  await page.getByText("History", { exact: false }).first().click();
  await expect(
    page.getByRole("navigation", { name: "Conversation history" }),
  ).toContainText("Release the sample workspace");
  await expect(
    page.getByRole("navigation", { name: "Conversation history" }),
  ).toContainText("review");
});

/*
 * A PM agent runs on the reader's own computer, so a workspace can have none.
 * Core reports that, and the UI then offers setup in the slot Ask PM
 * occupies rather than a conversation nothing can answer. The full state
 * matrix lives in `pm-onboarding.spec.js`; these two keep the real work
 * surfaces honest.
 */
test("a workspace with no PM offers setup instead of Ask PM", async ({
  page,
}) => {
  await setup(page);
  await page.route("**/pm/presence", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ configured: false, connected: false }),
    }),
  );

  await page.goto(`${root}/tasks`);
  await expect(page.locator('[data-pm-nav="setup"]').first()).toBeVisible();
  await expect(page.getByRole("link", { name: "Ask PM" })).toHaveCount(0);

  await page.goto(`${root}/inbox?mailbox=needs-you`);
  await expect(page.getByRole("link", { name: "Ask PM" })).toHaveCount(0);

  // The conversation itself is not reachable; setup is what it leads to. The
  // redirect can abort the navigation before it commits, which rejects `goto`
  // although nothing is wrong; where we land is the assertion that matters.
  await page.goto(`${root}/pm`).catch((error) => {
    if (!String(error?.message ?? "").includes("ERR_ABORTED")) throw error;
  });
  await expect(page).toHaveURL(new RegExp(`${root}/pm/setup$`));
  await expect(page.locator("[data-pm-install-command]")).toContainText("anx");
});

/*
 * Moving work another system owns is a request a PM carries out at the source
 * and the reader answers in the Inbox. With no PM there is nobody to carry it
 * out, so the affordance is gone and nothing is written — rather than a
 * proposal waiting in an Inbox that cannot show it.
 */
test("a source-owned move is not offered with no PM, and writes nothing", async ({
  page,
}) => {
  await setup(page);
  await page.route("**/pm/presence", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ configured: false, connected: false }),
    }),
  );
  const writes = [];
  await page.route("**/pm/decisions", async (route) => {
    const request = route.request();
    if (request.method() === "POST") writes.push(request.postDataJSON());
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ items: [], has_more: false }),
    });
  });

  // `card:release` is owned by GitHub in this fixture.
  await page.goto(`${root}/tasks/card%3Arelease`);
  await expect(
    page.getByRole("heading", { name: "Release the sample workspace" }),
  ).toBeVisible();

  await page.keyboard.press("ControlOrMeta+k");
  const palette = page.getByRole("dialog", { name: "Command palette" });
  await expect(palette).toBeVisible();
  await expect(palette.getByRole("option", { name: /Move to/ })).toHaveCount(0);
  await expect(palette.getByRole("option", { name: /Ask PM/ })).toHaveCount(0);
  await page.keyboard.press("Escape");

  expect(writes).toEqual([]);
});

/*
 * The move is attempted while presence is unknown, because refusing it on an
 * unproven state would block a workspace that has a PM. Core settles it — and
 * that answer has to land in the UI, not just in a toast: the shell stops
 * waiting and offers setup, and the reader gets the UI's own explanation
 * rather than core's raw sentence.
 */
test("a move attempted before the PM state loads self-corrects", async ({
  page,
}) => {
  await setup(page);
  // Presence never answers: the request is left pending on purpose.
  await page.route("**/pm/presence", () => {});
  await page.route("**/pm/decisions", async (route) => {
    if (route.request().method() !== "POST") {
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ items: [], has_more: false }),
      });
    }
    await route.fulfill({
      status: 409,
      contentType: "application/json",
      body: JSON.stringify({
        error: {
          code: "pm_not_onboarded",
          message: "PM is not onboarded.",
        },
      }),
    });
  });

  // `card:release` is GitHub-owned in this fixture.
  await page.goto(`${root}/tasks/card%3Arelease`);
  await expect(
    page.getByRole("heading", { name: "Release the sample workspace" }),
  ).toBeVisible();

  // The rows are offered, because nothing yet says there is no PM. `M` opens
  // the move sub-list; its leaves are the requests themselves.
  await page.keyboard.press("m");
  const palette = page.getByRole("dialog", { name: "Command palette" });
  const request = palette
    .getByRole("option", { name: /Request move to .* at / })
    .first();
  await expect(request).toBeVisible();
  await request.click();

  // Core's answer, in the UI's words, with the way forward.
  await expect(page.getByText(/this workspace has none/i)).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Set up your PM" }).first(),
  ).toBeVisible();
  // And recorded, so the shell stops waiting and commits to setup.
  await expect(page.locator('[data-pm-nav="setup"]').first()).toBeVisible();
});

test("a connected PM keeps Ask PM and says nothing about setup", async ({
  page,
}) => {
  await setup(page);
  await page.route("**/pm/presence", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        configured: true,
        connected: true,
        last_seen_at: new Date().toISOString(),
        signal: "claim",
      }),
    }),
  );
  const presence = page.waitForResponse((response) =>
    new URL(response.url()).pathname.endsWith("/pm/presence"),
  );
  await page.goto(`${root}/pm`);
  await presence;
  await expect(page.getByRole("heading", { name: "Ask PM" })).toBeVisible();
  await expect(page.locator('[data-pm-nav="setup"]')).toHaveCount(0);
  await expect(page.locator("[data-pm-offline-note]")).toHaveCount(0);
});
