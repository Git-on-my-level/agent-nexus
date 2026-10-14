import { mkdir } from "node:fs/promises";

import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import { nextPaint } from "../helpers/pageReady.js";
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * The Overview initiative tiles and the initiative page, in a real browser.
 *
 * Only one theme is captured because only one exists: `app.css` defines a
 * single dark token set, with no `data-theme` and no `prefers-color-scheme`
 * block. `chart-interaction.spec.js` covers the one thing a light theme would
 * change here — that chart series colours are chosen from the background
 * rather than baked in.
 */

const WORKSPACE = "/o/local/w/local";
const CARD_REF = "card:release-b";
const CARD_PATH = `${WORKSPACE}/initiatives/${encodeURIComponent(CARD_REF)}`;
const NOW = "2026-10-04T12:00:00Z";

const plan = {
  steps: [
    {
      id: "contracts",
      title: "Shared report contracts",
      after: [],
      ref: "card:contracts",
    },
    { id: "plan-model", title: "Plan model on cards", after: ["contracts"] },
    { id: "ui-chips", title: "Ref chips everywhere", after: ["contracts"] },
    { id: "tiles", title: "Overview tiles", after: ["plan-model", "ui-chips"] },
  ],
};

const planState = {
  steps: [
    { id: "contracts", status: "done", resolvable: true },
    { id: "plan-model", status: "active", resolvable: false },
    { id: "ui-chips", status: "blocked", resolvable: false },
    { id: "tiles", status: "not_started", resolvable: false },
  ],
  progress: { done: 1, total: 4 },
  critical_path: ["plan-model", "tiles"],
  next_steps: ["plan-model"],
  shape: "dag",
  last_movement_at: "2026-10-04T09:00:00Z",
};

/** A promise somebody else resolves. */
function deferred() {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

/**
 * @param {import("@playwright/test").Page} page
 * @param {{work?: object, plan?: object, planState?: object}} [overrides] the
 *   authored plan, the card record and the computed plan state, so a case can
 *   be about one of them.
 * @returns {object} the live fixture: swap `plan` / `planState` between reads,
 *   `hold` a plan read open, and `emit` a workspace event.
 */
async function installInitiativePage(page, overrides = {}) {
  await page.clock.setFixedTime(new Date(NOW));
  await installWorkspaceApi(page, {});
  const api = {
    plan: overrides.plan ?? plan,
    planState: { ...planState, ...(overrides.planState ?? {}) },
    /**
     * The computed summary the card record carries, as core puts it there.
     * Null leaves the key off, which is the planless shape.
     */
    workSummary: overrides.workSummary ?? null,
    /** Plan reads served, so a case can speak about "the second one". */
    planReads: 0,
    /** Card reads served, counted the same way. */
    cardReads: 0,
    /** `{ at, gate }`: hold the nth plan read open until the gate resolves. */
    hold: null,
    /** The same, for the nth card read. */
    holdCard: null,
    /** Stream connections waiting for something to deliver. */
    waiters: [],
    /** Events with no connection to deliver them on yet. */
    queued: [],
    emitted: 0,
  };
  /**
   * Push one workspace event down the stream, the way core does: one event
   * per connection, and the client reconnects for the next.
   */
  api.emit = (refs, type = "card_moved") => {
    api.emitted += 1;
    const event = {
      id: `evt-${api.emitted}`,
      type,
      ts: new Date().toISOString(),
      refs,
    };
    const waiter = api.waiters.shift();
    if (waiter) waiter.resolve(event);
    else api.queued.push(event);
  };
  await page.route("**/*", async (route) => {
    const request = route.request();
    if (!["fetch", "xhr"].includes(request.resourceType())) {
      return route.fallback();
    }
    const url = new URL(request.url());
    const path = decodeURIComponent(url.pathname);
    const json = (body) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    if (path.endsWith("/participants") && request.method() === "GET") {
      // Nobody is running against this card. An empty read is an absence; a
      // failed one is not, and the two must not render the same way.
      return json({ participants: [], next_cursor: "" });
    }
    if (path.endsWith("/observations") && request.method() === "GET") {
      return json({ observations: [], next_cursor: "" });
    }
    if (path.endsWith("/plan") && request.method() === "GET") {
      api.planReads += 1;
      // Snapshot at request time, so a read held open answers with the plan
      // that was true when it was issued rather than with the latest one.
      const body = { plan: api.plan, plan_state: api.planState };
      if (api.hold && api.hold.at === api.planReads) {
        await api.hold.gate.promise;
      }
      return json(body);
    }
    if (path === "/events" && request.method() === "GET") {
      // The cursor seed: the stream starts after the newest event core has.
      return json({ events: [] });
    }
    if (path === "/stream/events") {
      const waiter = deferred();
      const queued = api.queued.shift();
      if (queued) waiter.resolve(queued);
      else api.waiters.push(waiter);
      const event = await waiter.promise;
      return route
        .fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: `id: ${event.id}\nevent: event\ndata: ${JSON.stringify({ event })}\n\n`,
        })
        .catch(() => {});
    }
    if (path === "/refs/resolve" && request.method() === "POST") {
      return json({
        items: [
          {
            ref: "card:contracts",
            kind: "card",
            title: "Shared report contracts",
            status: "done",
            owner: "Codex Luna",
            progress: { done: 5, total: 5 },
            resolvable: true,
          },
        ],
      });
    }
    if (path.startsWith("/work/") && request.method() === "GET") {
      api.cardReads += 1;
      /*
       * Snapshot at request time, like the plan read: a card read held open
       * answers with the record that was true when it was issued, which is
       * the whole point of holding it.
       */
      const carried = {
        plan_state: api.planState,
        ...(api.workSummary ? { work_summary: api.workSummary } : {}),
      };
      if (api.holdCard && api.holdCard.at === api.cardReads) {
        await api.holdCard.gate.promise;
      }
      return json({
        work: {
          ...carried,
          ref: CARD_REF,
          handle: "release-b",
          title: "Release B",
          summary:
            "Initiative plans, live dashboards and agent ergonomics.\n\nThe card body sits below the plan.",
          phase: "in_progress",
          priority: "high",
          definition_of_done: [
            "Plans render in the shape the plan has",
            "Tiles answer state and progress in ten seconds",
          ],
          next_action: "Wire the Overview tiles",
          source: { authority: "nexus" },
          ...(overrides.work ?? {}),
        },
      });
    }
    return route.fallback();
  });
  return api;
}

test("initiative page shows the plan under a collapsed description", async ({
  page,
}, testInfo) => {
  test.setTimeout(90_000);
  await installInitiativePage(page);
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.goto(CARD_PATH);

  const planSection = page.locator("[data-initiative-plan]");
  await expect(planSection).toBeVisible({ timeout: 60_000 });

  // The Plan section does not badge the state; the header reads it from the brief.
  await expect(planSection.locator("[data-health]")).toHaveCount(0);

  /*
   * And the header is that badge plus one line. There is no Reload — the card,
   * its plan and its chips re-read themselves off the event stream — and the
   * PM button says what it does, so it no longer reads as a copy of the
   * sidebar's "Ask PM" with a different destination.
   */
  await expect(page.getByRole("button", { name: "Reload" })).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "Ask PM about this" }),
  ).toHaveAttribute("href", /\/pm\?work_ref=card%3Arelease-b$/);

  // The plan's shape picks the view: this one branches, so it is a tree.
  await expect(page.locator("[data-plan-shape='dag']")).toBeVisible();
  await expect(page.locator(".plan-node")).toHaveCount(4);
  await expect(page.locator(".plan-edge--critical")).toHaveCount(1);

  // A step with a ref renders as a resolved chip, not a bare id.
  await expect(
    page.locator(".plan-node [data-anx-ref='card:contracts']"),
  ).toContainText("Shared report contracts");

  /*
   * One plan list. The tree is a picture of the plan, and the rows under it
   * are the plan — there is no second, collapsed copy of the same four steps
   * to open. Each row carries what the fold used to hide: the status word on
   * every step including the done one, the critical-path marker, and "after X"
   * only where a step has a dependency.
   */
  await expect(page.getByText("View plan steps")).toHaveCount(0);
  const rows = page.locator(".plan-track--tree > li");
  await expect(rows).toHaveCount(4);
  await expect(rows.locator(".plan-node__status")).toHaveText([
    "Done",
    "In progress",
    "Blocked",
    "Not started",
  ]);
  await expect(rows.locator("[data-plan-critical]")).toHaveCount(2);
  await expect(
    rows.filter({ hasText: "after Shared report contracts" }),
  ).toHaveCount(2);
  // The dependency list names steps, and only the steps that have one.
  await expect(rows.nth(0).locator(".plan-node__after")).toHaveCount(0);

  // The hand-written note still stands — it names a step the plan has not
  // passed — and says what it is rather than passing for computed state.
  await expect(page.locator("[data-next-note]")).toContainText(
    "Wire the Overview tiles",
  );
  await expect(
    page.locator("[data-next-note] [data-anx-provenance]"),
  ).toHaveText(/^Hand-written$/);

  /*
   * Nothing to report is not worth a heading: no participation section at
   * all, and the Evidence section itself hidden. Asserted on the section,
   * which is always in the markup — an assertion on the empty-state line
   * inside it would pass just as happily if the line had never rendered.
   */
  await expect(page.locator("[data-participation]")).toHaveCount(0);
  const evidence = page.locator("[data-task-evidence]");
  await expect(evidence).toHaveCount(1);
  await expect(evidence).toBeHidden();
  await expect(page.getByRole("heading", { name: "Evidence" })).toBeHidden();

  // Computed beside hand-written: the plan says it was read just now, and the
  // prose below it says a person wrote it. Before this, a reader had no way to
  // tell which of the two they were looking at.
  await expect(planSection.locator("[data-anx-provenance]")).toHaveText(
    /^Live · updated just now$/,
  );
  // The collapsed description sits above the plan.
  const order = await page
    .locator("[data-initiative-plan], [data-initiative-description]")
    .evaluateAll((nodes) =>
      nodes.map((node) =>
        node.hasAttribute("data-initiative-plan") ? "plan" : "body",
      ),
    );
  expect(order).toEqual(["body", "plan"]);

  await mkdir(".screenshots/review", { recursive: true });
  await page.screenshot({
    path: ".screenshots/review/initiative-desktop.png",
    animations: "disabled",
  });
  await page.screenshot({
    path: testInfo.outputPath("initiative-desktop.png"),
    animations: "disabled",
  });
  await testInfo.attach("initiative-desktop", {
    path: testInfo.outputPath("initiative-desktop.png"),
    contentType: "image/png",
  });
});

test("the plan diagram keeps a readable fallback and a focusable chip", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await installInitiativePage(page);
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });

  // Edges are decoration; the dependencies they draw are also written out,
  // on rows that are simply on the page rather than behind a fold.
  await expect(page.locator(".plan-tree__edges")).toHaveAttribute(
    "aria-hidden",
    "true",
  );
  const steps = page.locator(".plan-track--tree > li");
  await expect(steps).toHaveCount(4);
  await expect(
    steps.filter({ hasText: "after Shared report contracts" }),
  ).not.toHaveCount(0);

  // A chip inside the diagram is a real link the keyboard can reach.
  const chip = page.locator(".plan-node a[data-anx-ref='card:contracts']");
  await chip.focus();
  await expect(chip).toBeFocused();
  await expect(page.locator(".anx-ref-preview")).toBeVisible();
  await expect(page.locator(".anx-ref-preview")).toContainText("Codex Luna");
});

/**
 * The case David found on the omi card: the first phase is done, and the
 * page was still printing the sentence somebody typed when it was not.
 */
test("Who is next drops a note the plan has passed", async ({ page }) => {
  test.setTimeout(90_000);
  await installInitiativePage(page, {
    work: {
      // A role, not a principal. The avatar rendered this as "AG agent" —
      // initials for nobody — so the page names the owner it can resolve
      // instead and says plainly when it cannot.
      next_actor: "agent",
      next_action: "Phase 1: shared report contracts, with Luna",
    },
    planState: {
      steps: [
        { id: "contracts", status: "done", resolvable: true },
        { id: "plan-model", status: "active", resolvable: false },
        { id: "ui-chips", status: "not_started", resolvable: false },
        { id: "tiles", status: "not_started", resolvable: false },
      ],
      next_steps: ["plan-model", "ui-chips"],
      critical_path: ["plan-model", "tiles"],
    },
  });
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });

  // The note names only a step core says is done, so it is gone rather than
  // sitting under a computed next step and contradicting it.
  await expect(page.locator("[data-next-note]")).toHaveCount(0);

  // And nobody is given initials they do not have.
  await expect(page.locator("[data-next-actor-role]")).toContainText(
    "no principal named",
  );
});

/**
 * The three things the live stream has to get right now that Reload is gone:
 * a plan that was emptied, two refreshes that overlap, and a change to a card
 * a step points at rather than to this card.
 */
test("a cleared plan leaves the page, rather than staying on screen", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const api = await installInitiativePage(page);
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });
  await expect(page.locator(".plan-track--tree > li")).toHaveCount(4);

  /*
   * Somebody emptied the plan. That reads back as a successful response with
   * no steps — not as an error — and the page used to apply only a plan that
   * had some, so four steps nobody has stayed on screen with nothing left to
   * clear them.
   */
  api.plan = { steps: [] };
  api.planState = {
    ...api.planState,
    steps: [],
    progress: { done: 0, total: 0 },
  };
  api.emit([CARD_REF]);

  await expect(page.locator("[data-initiative-plan]")).toHaveCount(0, {
    timeout: 20_000,
  });
  // And the card body takes the space the plan had, under its Description.
  await page.locator("[data-initiative-description] summary").click();
  await expect(
    page.getByText("The card body sits below the plan."),
  ).toBeVisible();
});

test("an older plan read cannot overwrite a newer one", async ({ page }) => {
  test.setTimeout(90_000);
  const api = await installInitiativePage(page);
  await page.goto(CARD_PATH);
  await expect(page.locator(".plan-track--tree > li")).toHaveCount(4, {
    timeout: 60_000,
  });

  // The next read is held open, so the refresh it belongs to is still in
  // flight when the one after it is issued.
  const slow = deferred();
  api.hold = { at: 2, gate: slow };
  api.emit([CARD_REF]);
  await expect.poll(() => api.planReads, { timeout: 20_000 }).toBe(2);

  // A second refresh, with the plan as it now stands: one step fewer.
  api.hold = null;
  api.plan = { steps: plan.steps.slice(0, 3) };
  api.planState = {
    ...api.planState,
    steps: api.planState.steps.slice(0, 3),
    progress: { done: 1, total: 3 },
  };
  api.emit([CARD_REF]);
  await expect(page.locator(".plan-track--tree > li")).toHaveCount(3, {
    timeout: 20_000,
  });

  /*
   * Now the held read answers — with what was true when it was issued. It is
   * the older of the two, and the page has to ignore it: both refreshes ran
   * under the same page ticket, so whichever landed last used to win, and on
   * a busy card that is routinely the stale one.
   */
  slow.resolve();
  await expect.poll(() => api.planReads, { timeout: 20_000 }).toBe(3);
  await page.waitForTimeout(1_000);
  await expect(page.locator(".plan-track--tree > li")).toHaveCount(3);
});

test("an older card read cannot bring a cleared plan's header back", async ({
  page,
}) => {
  test.setTimeout(90_000);
  /*
   * The card record carries its own copy of the computed state, which is the
   * copy that goes stale: core puts `plan_state` and `work_summary` on every
   * card-bearing read, and a read issued before a plan was cleared answers
   * with the plan that was there.
   */
  const api = await installInitiativePage(page, {
    workSummary: {
      status: { state: "moving", label: "Moving" },
      progress: { done: 1, total: 4, unit: "steps" },
    },
  });
  await page.goto(CARD_PATH);
  await expect(page.locator(".plan-track--tree > li")).toHaveCount(4, {
    timeout: 60_000,
  });

  // Refresh A: its *card* read is held open, with the plan still in place.
  const slowCard = deferred();
  api.holdCard = { at: 2, gate: slowCard };
  api.emit([CARD_REF]);
  await expect.poll(() => api.cardReads, { timeout: 20_000 }).toBe(2);

  // The plan is cleared, and refresh B reads the card and the plan as they
  // now stand. The page follows it: no plan, and no step count.
  api.holdCard = null;
  api.plan = { steps: [] };
  api.planState = null;
  api.workSummary = { status: { state: "no_promise", label: "No promise" } };
  api.emit([CARD_REF]);
  await expect(page.locator("[data-initiative-plan]")).toHaveCount(0, {
    timeout: 20_000,
  });

  /*
   * Now A's card read lands, carrying the record from before the clear. Its
   * ticket covers the card read as well as the plan read, so it writes
   * nothing: guarding the plan alone left this read free to put the old
   * summary — and the old `plan_state` the header falls back to — back on
   * screen until the next event happened along.
   */
  slowCard.resolve();
  await expect.poll(() => api.cardReads, { timeout: 20_000 }).toBe(3);
  await page.waitForTimeout(1_000);
  await expect(page.locator("[data-initiative-plan]")).toHaveCount(0);
  await expect(page.locator(".plan-track--tree > li")).toHaveCount(0);
});

test("a read that found no plan is not overruled by the record's own copy", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const api = await installInitiativePage(page);
  await page.goto(CARD_PATH);
  await expect(page.locator(".plan-track--tree > li")).toHaveCount(4, {
    timeout: 60_000,
  });

  /*
   * The plan is cleared, but the card record still carries the `plan_state`
   * core computed for it a moment earlier — two reads of a card mid-change
   * disagree, and the dedicated plan read is the one that asked about the
   * plan. `plan === null` could not tell "this card has none" from "nothing
   * has come back yet", so the header read the record's copy and put the
   * step count of a plan that is gone back on the line.
   */
  api.plan = { steps: [] };
  api.emit([CARD_REF]);

  await expect(page.locator("[data-initiative-plan]")).toHaveCount(0, {
    timeout: 20_000,
  });
});

test("a linked card moving re-reads this card's plan", async ({ page }) => {
  test.setTimeout(90_000);
  const api = await installInitiativePage(page);
  await page.goto(CARD_PATH);
  const rows = page.locator(".plan-track--tree > li");
  await expect(rows).toHaveCount(4, { timeout: 60_000 });
  await expect(rows.nth(1).locator(".plan-node__status")).toHaveText(
    "In progress",
  );

  /*
   * `card:contracts` is the card behind step one. Finishing it is what moves
   * this plan, and the event for it carries that card's ref and not this
   * one's — so a page filtering the stream on its own ref alone sat here with
   * a step still reading "In progress".
   */
  api.planState = {
    ...api.planState,
    steps: [
      { id: "contracts", status: "done", resolvable: true },
      { id: "plan-model", status: "done", resolvable: false },
      { id: "ui-chips", status: "blocked", resolvable: false },
      { id: "tiles", status: "not_started", resolvable: false },
    ],
    progress: { done: 2, total: 4 },
  };
  api.emit(["card:contracts"]);

  await expect(rows.nth(1).locator(".plan-node__status")).toHaveText("Done", {
    timeout: 20_000,
  });
});

test("a named owner is who the card is waiting on, in Who is next", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await installInitiativePage(page, {
    // The workspace's own actor, which the registry names "Operator".
    work: { owner: "actor-operator" },
  });
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });

  // The Who is next section names the owner the registry resolves.
  await expect(
    page.locator("section", { hasText: "Who is next" }).first(),
  ).toContainText("Operator");
  await expect(page.locator("[data-next-actor-role]")).toHaveCount(0);
  await expect(page.getByText("Nobody assigned")).toHaveCount(0);
});

test("a step pointing at a plain URL reads as a name, not a URL", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const url =
    "https://github.com/Git-on-my-level/agent-nexus/blob/main/README.md";
  await installInitiativePage(page, {
    // The second step points at a doc rather than at a card.
    plan: {
      steps: plan.steps.map((step) =>
        step.id === "plan-model" ? { ...step, ref: url } : step,
      ),
    },
    planState: {
      steps: [
        { id: "contracts", status: "done", resolvable: true },
        { id: "plan-model", status: "active", resolvable: true },
        { id: "ui-chips", status: "not_started", resolvable: false },
        { id: "tiles", status: "not_started", resolvable: false },
      ],
    },
  });
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });
  const chip = page.locator(`.plan-track--tree [data-anx-ref="${url}"]`);
  /*
   * A short label on screen, the whole URL on hover, and a working link.
   * Nothing core resolves answers for this ref — it is not a card, a doc or a
   * topic — so it used to render as the raw URL, dashed and marked "not
   * found", while linking perfectly well.
   */
  await expect(chip).toContainText("github.com/…/README.md");
  await expect(chip).not.toContainText("not found");
  await expect(chip).toHaveAttribute("title", url);
  await expect(chip).toHaveAttribute("href", url);
});

test("initiative page has no accessibility violations", async ({ page }) => {
  test.setTimeout(90_000);
  await installInitiativePage(page);
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });
  const results = await new AxeBuilder({ page })
    .include("[data-initiative-plan]")
    .withTags(["wcag2a", "wcag2aa"])
    .analyze();
  expect(results.violations).toEqual([]);
});

test("the plan reads at phone width", async ({ page }, testInfo) => {
  test.setTimeout(90_000);
  await installInitiativePage(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });

  // The tree scrolls sideways rather than squashing or overflowing the page.
  const scroller = page.locator(".plan-tree-scroll");
  await expect(scroller).toBeVisible();
  const overflows = await scroller.evaluate(
    (node) => node.scrollWidth > node.clientWidth,
  );
  expect(overflows).toBe(true);
  const documentOverflows = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth + 1,
  );
  expect(documentOverflows).toBe(false);

  /*
   * The rows below it keep their status words at 390px. They used to be the
   * first thing dropped on a phone, on the grounds that the dot carried the
   * status — which it does, as a colour, to readers who can see it.
   */
  const rows = page.locator(".plan-track--tree > li");
  await expect(rows.locator(".plan-node__status")).toHaveText([
    "Done",
    "In progress",
    "Blocked",
    "Not started",
  ]);
  // And the rail is not 18rem of nothing: this card has no outside source and
  // nothing waiting on the reader, so it starts collapsed.
  await expect(page.locator("[data-task-rail]")).toHaveAttribute(
    "data-task-rail",
    "collapsed",
  );

  await mkdir(".screenshots/review", { recursive: true });
  await page.screenshot({
    path: ".screenshots/review/initiative-phone.png",
    animations: "disabled",
  });
  await page.screenshot({
    path: testInfo.outputPath("initiative-phone.png"),
    animations: "disabled",
  });
  await testInfo.attach("initiative-phone", {
    path: testInfo.outputPath("initiative-phone.png"),
    contentType: "image/png",
  });
});
/**
 * The preview's keyboard path, against the real chip — the unit tests use a
 * stand-in anchor, which does not carry the chip's own focus handler and so
 * cannot show either of these failures.
 */
test("the preview is reachable by Tab and Escape does not reopen it", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await installInitiativePage(page);
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });

  const chip = page.locator(".plan-node a[data-anx-ref='card:contracts']");
  const card = page.locator(".anx-ref-preview");

  await chip.focus();
  await expect(card).toBeVisible();

  // Tab steps into the card's own controls, not past them to the next page
  // control. The card is appended at the end of the document, so document
  // order alone would never get a reader here.
  await page.keyboard.press("Tab");
  await expect(card).toBeVisible();
  const inCard = await card.evaluate((node) =>
    node.contains(document.activeElement),
  );
  expect(inCard).toBe(true);

  // Walk to Copy ref and dismiss from there.
  const copy = card.locator("button", { hasText: "Copy ref" });
  await copy.focus();
  await expect(copy).toBeFocused();
  await page.keyboard.press("Escape");

  // Escape restores focus to the chip, and that focus must not reopen the
  // dialog the reader just dismissed.
  await expect(card).toBeHidden();
  await expect(chip).toBeFocused();
  // Focus returns to the chip synchronously. One paint is enough to see a
  // reopen; the preview's own guard is what keeps that focus from opening it.
  await nextPaint(page);
  await expect(card).toBeHidden();
});
