import { expect, test } from "@playwright/test";

import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * The shared markdown renderer and the compact badges, in a real browser.
 *
 * The unit tests cover what `renderMarkdown` emits. These cover what only a
 * browser can show: that the ref-chip placeholders actually hydrate into
 * chips with working previews, that a `<details>` disclosure opens, that an
 * HTML comment leaves no trace in the DOM, and that a badge's full text is
 * reachable on hover rather than truncated away.
 */

const WORKSPACE = "/o/local/w/local";
const CARD_REF = "card:renderer";
const CARD_PATH = `${WORKSPACE}/tasks/${encodeURIComponent(CARD_REF)}`;
const PR_URL = "https://github.com/Git-on-my-level/agent-nexus/pull/246";
const NOW = "2026-10-04T12:00:00Z";

const BODY = [
  "**Goal:** one renderer everywhere.",
  "",
  "<!-- fleet-sync:evidence:v1 -->",
  "",
  "- [x] markdown lib",
  "- [ ] replace the ad-hoc renderers",
  "",
  "| surface | state |",
  "| --- | --- |",
  "| docs | done |",
  "",
  "Blocked by card:contracts until the shape lands, and landed in " +
    `${PR_URL}.`,
  "",
  "<details><summary>Why one renderer</summary>",
  "",
  "Because five of them disagreed about `<details>`.",
  "",
  "</details>",
  "",
  "```",
  "card:not-a-chip",
  "```",
].join("\n");

const plan = {
  steps: [
    {
      id: "land-the-pr",
      title: "Land the renderer",
      after: [],
      ref: PR_URL,
    },
    { id: "replace-rest", title: "Replace the rest", after: ["land-the-pr"] },
  ],
};

const planState = {
  steps: [
    { id: "land-the-pr", status: "active" },
    { id: "replace-rest", status: "not_started" },
  ],
  progress: { done: 0, total: 2 },
  critical_path: ["land-the-pr", "replace-rest"],
  next_steps: ["land-the-pr"],
  shape: "chain",
  health: "on_track",
  last_movement_at: "2026-10-04T04:00:00Z",
};

async function installCard(page, { externalResolve = true } = {}) {
  await page.clock.setFixedTime(new Date(NOW));
  await installWorkspaceApi(page, {});
  await page.route("**/*", async (route) => {
    const request = route.request();
    if (!["fetch", "xhr"].includes(request.resourceType())) {
      return route.fallback();
    }
    const path = decodeURIComponent(new URL(request.url()).pathname);
    const json = (body) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    if (path.endsWith("/plan") && request.method() === "GET") {
      return json({ plan, plan_state: planState });
    }
    if (path === "/refs/resolve" && request.method() === "POST") {
      const items = [
        {
          ref: "card:contracts",
          kind: "card",
          title: "Shared report contracts",
          status: "done",
          owner_display: "Codex Luna",
          resolvable: true,
        },
      ];
      if (externalResolve) {
        items.push({
          ref: PR_URL,
          kind: "external",
          authority: "github",
          native_id: "Git-on-my-level/agent-nexus#246",
          title: "One shared markdown renderer",
          url: PR_URL,
          status: "merged",
          resolvable: true,
        });
      }
      return json({ items });
    }
    if (path.endsWith("/observations") && request.method() === "GET") {
      // Read cleanly, with nothing reported: the quiet path, not a failure.
      return json({ observations: [], next_cursor: "" });
    }
    if (path.endsWith("/participants") && request.method() === "GET") {
      return json({ participants: [], next_cursor: "" });
    }
    if (path.startsWith("/work/") && request.method() === "GET") {
      const card = {
        ref: CARD_REF,
        handle: "renderer",
        title: "One shared renderer",
        summary: BODY,
        phase: "in_progress",
        source: { authority: "nexus" },
      };
      /*
       * Answer the way core does for the parameter the page sent.
       *
       * `summary=1` puts the computed object at `summary` and moves the body
       * to `summary_text`. A mock that returns prose either way renders the
       * body no matter what the page reads, and that is how a page passing
       * the object straight to the markdown renderer passed this spec with
       * every planned card's body blank.
       */
      if (new URL(request.url()).searchParams.get("summary") !== "1") {
        return json({ work: card });
      }
      // The summary core would compute for this plan: on track, its two
      // steps, moving when the plan state says it moved. Core's word for
      // `on_track` is "In progress".
      const computed = {
        status: {
          state: "on_track",
          label: "In progress",
          reason: "Open steps are progressing.",
        },
        progress: { done: 0, total: 2, unit: "steps" },
        last_movement_at: planState.last_movement_at,
      };
      return json({
        work: {
          ...card,
          summary_text: BODY,
          summary: computed,
          work_summary: computed,
        },
      });
    }
    return route.fallback();
  });
}

async function openCard(page, options) {
  await installCard(page, options);
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-body]")).toBeVisible({
    timeout: 60_000,
  });
}

test("the card body renders GFM through the shared renderer", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await openCard(page);
  const body = page.locator("[data-initiative-body] .markdown-rendered");

  // Task lists, tables and code blocks: full GFM.
  await expect(body.locator('input[type="checkbox"]')).toHaveCount(2);
  await expect(body.locator('input[type="checkbox"]').first()).toBeChecked();
  await expect(body.locator("table th").first()).toHaveText("surface");
  await expect(body.locator("pre code")).toContainText("card:not-a-chip");
  await expect(body.locator("strong").first()).toHaveText("Goal:");
});

test("a details disclosure opens and an HTML comment leaves no trace", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await openCard(page);
  const body = page.locator("[data-initiative-body] .markdown-rendered");

  // The marker is gone from the DOM, not merely invisible. (Svelte's own
  // empty anchor comments are everywhere, so this looks for authored ones.)
  const authoredComments = await body.evaluate((root) => {
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_COMMENT);
    const out = [];
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = (node.nodeValue ?? "").trim();
      if (text) out.push(text);
    }
    return out;
  });
  expect(authoredComments).toEqual([]);
  await expect(body).not.toContainText("fleet-sync");

  const disclosure = body.locator("details");
  await expect(disclosure.locator("summary")).toHaveText("Why one renderer");
  await expect(body.getByText("Because five of them disagreed")).toBeHidden();
  await disclosure.locator("summary").click();
  await expect(body.getByText("Because five of them disagreed")).toBeVisible();
});

test("refs written in prose become chips with a working preview", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await openCard(page);
  const body = page.locator("[data-initiative-body] .markdown-rendered");

  // The ANX ref carries its resolved title, not the raw ref.
  const chip = body.locator("[data-anx-ref='card:contracts']");
  await expect(chip).toContainText("Shared report contracts");
  // A ref inside a fenced block is an example, not a destination.
  await expect(body.locator("[data-anx-ref='card:not-a-chip']")).toHaveCount(0);

  // Hovering a chip opens the same preview card it opens everywhere else.
  await chip.hover();
  const preview = page.locator(".anx-ref-preview");
  await expect(preview).toBeVisible();
  await expect(preview).toContainText("Codex Luna");
});

test("a GitHub ref in prose becomes a chip carrying its status", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await openCard(page);
  const body = page.locator("[data-initiative-body] .markdown-rendered");

  const chip = body.locator(`[data-anx-ref='${PR_URL}']`);
  await expect(chip).toContainText("One shared markdown renderer");
  await expect(chip).toContainText("merged");
  await expect(chip).toHaveAttribute("href", PR_URL);
  await expect(chip).toHaveAttribute("target", "_blank");
  await expect(chip).not.toContainText("not found");
});

test("a plan step names its title, with the external ref as a chip beneath", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await openCard(page);
  const node = page.locator("[data-plan-node='land-the-pr']");
  await expect(node).toBeVisible();
  await expect(node.locator(".plan-node__title")).toHaveText(
    "Land the renderer",
  );
  const chip = node.locator("[data-anx-ref]");
  await expect(chip).toContainText("merged");
  await expect(chip).not.toContainText("not found");
});

test("a valid GitHub ref is never not-found, even when the resolve misses it", async ({
  page,
}) => {
  test.setTimeout(90_000);
  // Core answers the batch without the external row — an older core, or a
  // batch that failed. The chip must still link and label itself.
  await openCard(page, { externalResolve: false });
  const chip = page
    .locator("[data-plan-node='land-the-pr'] [data-anx-ref]")
    .first();
  await expect(chip).not.toContainText("not found");
  await expect(chip).toContainText("Git-on-my-level/agent-nexus#246");
  await expect(chip).toHaveAttribute("href", PR_URL);
});

test("badges stay compact and show their full text at once on hover", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await openCard(page);

  /*
   * Status: a pill with the label, never truncated. It lives in the page
   * header now, through the one `WorkSummary` every surface renders — the
   * Plan section used to badge it separately.
   *
   * The label is core's own wording, shown verbatim: core calls `on_track`
   * "In progress". The state is what the colour and the sort read.
   */
  const summary = page.locator('[data-work-summary="header"]');
  const health = summary.locator("[data-health]").first();
  await expect(health).toHaveAttribute("data-health", "on_track");
  await expect(health).toContainText("In progress");
  const clipped = await health.evaluate(
    (node) => node.scrollWidth > node.clientWidth + 1,
  );
  expect(clipped).toBe(false);

  /*
   * Age: two characters, green because the plan moved eight hours ago and an
   * initiative is expected to move every three days. The verb, the exact
   * instant and the expectation are in the tooltip.
   */
  const age = summary.locator("[data-freshness]").first();
  await expect(age).toHaveText("8h");
  await expect(age).toHaveClass(/ui-badge--ok/);
  await expect(age).toHaveAttribute(
    "data-tooltip",
    /^Moved .*\(8h\) — within the expected 3d$/,
  );
  await expect(age).toHaveAttribute("datetime", planState.last_movement_at);

  /*
   * The tooltip itself: the browser's own takes about a second and paints a
   * question-mark cursor while you wait. Ours is up in a frame or two, and
   * the cursor is gone.
   */
  await expect(page.locator("[data-anx-tooltip]")).toHaveCount(0);
  await expect(age).toHaveCSS("cursor", "auto");
  await age.hover();
  const tip = page.locator("[data-anx-tooltip]");
  await expect(tip).toBeVisible({ timeout: 300 });
  await expect(tip).toContainText("within the expected 3d");
  // And it goes away again when the pointer leaves.
  await page.mouse.move(0, 0);
  await expect(tip).toHaveCount(0);
});

test("the task page is quiet where there is nothing to say", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await openCard(page);

  // Participation and evidence have nothing to report, so each is one line
  // rather than a paragraph about an absence.
  await expect(page.locator("[data-participation-quiet]")).toContainText(
    "No activity",
  );
  await expect(page.locator("[data-evidence-quiet]")).toHaveText("No activity");

  // The developer disclaimers are still reachable, behind a toggle.
  const caveat = page.getByText("No shared evidence report for handoff yet", {
    exact: false,
  });
  await expect(caveat).toBeHidden();
  const toggle = page.getByText("What evidence means", { exact: true });
  await expect(toggle).toBeVisible();
  await toggle.click();
  await expect(caveat).toBeVisible();

  // "Created here — nothing to check" is a disclaimer too, and is folded.
  await expect(page.getByText("Why there is nothing to check")).toBeVisible();
  await expect(
    page.getByText("This task was created here, so there is no outside source"),
  ).toBeHidden();
});
