import { expect, test } from "@playwright/test";

import { expectCleanLayout } from "../helpers/layoutAudit.js";
import { holdOpenStream } from "../helpers/openStream.js";

/**
 * How an inbox item reads: a heading that is a heading, a body that renders as
 * markdown, and the ask written once.
 *
 * Three shapes are covered, because all three arrive from real agents:
 *   - a legacy ask with its whole question in the title and no body,
 *   - a well-formed ask with `## Decision` / `## Options` / `## Evidence`,
 *   - an ask whose auto-created card is titled with the ask text.
 *
 * Both surfaces are checked — the list pane and the standalone item route —
 * at desktop and at 390px.
 */

const WS = "/o/local/w/local";
const INBOX_PATH = `${WS}/inbox`;
const ACTOR_ID = "actor-readability";
const FIRST_PAINT = { timeout: 20_000 };
const PHONE = { width: 390, height: 844 };
const DESKTOP = { width: 1440, height: 900 };

const LONG_SHA =
  "1fb951be68b4aa395611181d1d7af40857ea6c038e26393eab6433516f2ee888";

/** The shape the Inbox saw in dogfood: the entire ask filed as the title. */
const LEGACY_TITLE =
  "Should we ship the inbox rendering fix in 0.12.58 or hold it for the next " +
  "minor? Context: the detail pane prints the whole ask as one bold heading, " +
  `about 600 characters including the commit ${LONG_SHA}, and readers called ` +
  "it ugly and overwhelming. Options: ship now, which is renderer-only and " +
  "additive; hold for the next minor and land it with the data contract; or " +
  "split the contract out first and ship nothing today. Recommendation: ship " +
  "now, because the data contract lands separately in core.";

const LEGACY_HEADING =
  "Should we ship the inbox rendering fix in 0.12.58 or hold it for the next " +
  "minor?";

const WELL_FORMED_BODY = `## Decision
Ship the renderer change in 0.12.58, or hold it.

## Options
- **Ship now** — renderer-only and additive.
- **Hold for the next minor** — lands with the data contract.

## Evidence
- Commit \`${LONG_SHA}\`
- card:anx-742`;

/** The card title `anx ask` auto-creates: the ask text over again. */
const DUPLICATE_CARD_TITLE = LEGACY_TITLE;

/**
 * One long sentence, so there is no sentence end to cut on and the heading
 * takes the whole character budget. That does not fit two lines at 390px,
 * which is the case "Show full title" exists for.
 */
const ONE_SENTENCE_TITLE =
  "Confirm whether the release manager should cut 0.12.58 tonight with the " +
  "renderer change alone or wait for the data contract work that Sol is " +
  "finishing in core before the rollout window closes tomorrow morning";

function item(overrides) {
  return {
    status: "open",
    kind: "ask",
    thread_id: "thread-readability",
    related_refs: ["thread:thread-readability"],
    subject_ref: "thread:thread-readability",
    response_proposals: [],
    requester_label: "release-bot",
    requester_agent_id: "agent-release-bot",
    source_event_time: new Date(Date.now() - 3_600_000).toISOString(),
    ...overrides,
  };
}

const ITEMS = [
  item({ id: "inbox-legacy", title: LEGACY_TITLE, body: "" }),
  item({
    id: "inbox-well-formed",
    title: "Ship the inbox rendering fix in 0.12.58?",
    body: WELL_FORMED_BODY,
    response_proposals: ["Ship now.", "Hold for the next minor."],
  }),
  item({
    id: "inbox-duplicate-card",
    title: LEGACY_TITLE,
    body: "",
    subject_ref: "card:anx-742",
    related_refs: ["card:anx-742", "thread:thread-readability"],
  }),
  item({ id: "inbox-no-sentence", title: ONE_SENTENCE_TITLE, body: "" }),
];

const WORK = [
  {
    id: "anx-742",
    ref: "card:anx-742",
    handle: "anx-742",
    title: DUPLICATE_CARD_TITLE,
    phase: "in_progress",
    updated_at: new Date(Date.now() - 7_200_000).toISOString(),
    source: { authority: "nexus" },
  },
];

/** The core path a request targets, or "" for a SvelteKit page/data load. */
function corePathOf(urlLike) {
  const url = typeof urlLike === "string" ? new URL(urlLike) : urlLike;
  let pathname = url.pathname;
  if (pathname.length > 1 && pathname.endsWith("/")) {
    pathname = pathname.slice(0, -1);
  }
  if (/^\/o\/[^/]+\/w\/[^/]+(\/|$)/.test(pathname)) return "";
  const hosted = pathname.match(/^\/ws\/[^/]+\/[^/]+(\/.*)?$/);
  if (hosted) pathname = hosted[1] || "/";
  return pathname;
}

/** @param {RegExp} pattern */
function core(pattern) {
  return (url) => {
    const pathname = corePathOf(url);
    return Boolean(pathname) && pattern.test(pathname);
  };
}

async function installApi(page) {
  const json = (route, body) =>
    route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });

  await page.addInitScript((actorId) => {
    localStorage.setItem("anx_ui_actor_id:local", actorId);
    localStorage.setItem("workspaceTourSeen.local", "1");
  }, ACTOR_ID);

  await page.route(
    (url) => corePathOf(url).startsWith("/stream/"),
    (route) => holdOpenStream(page, route),
  );
  await page.route(core(/^\/actors$/), (route) =>
    json(route, {
      actors: [{ id: ACTOR_ID, display_name: "Reader", tags: ["human"] }],
    }),
  );
  await page.route(core(/^\/pm\/decisions$/), (route) =>
    json(route, { items: [], has_more: false }),
  );
  await page.route(core(/^\/pm\/actions$/), (route) =>
    json(route, { items: [], has_more: false }),
  );
  await page.route(core(/^\/home\/unread$/), (route) =>
    json(route, { groups: [] }),
  );
  await page.route(core(/^\/docs$/), (route) => json(route, { documents: [] }));
  await page.route(core(/^\/refs\/resolve$/), (route) =>
    json(route, { refs: [] }),
  );
  await page.route(core(/^\/events/), (route) => json(route, { events: [] }));
  await page.route(core(/^\/work$/), (route) =>
    json(route, { work: WORK, next_cursor: "" }),
  );
  await page.route(core(/^\/work\/[^/]+$/), (route) =>
    json(route, { work: WORK[0] }),
  );
  await page.route(core(/^\/inbox$/), (route) => {
    const status =
      new URL(route.request().url()).searchParams.get("status") || "open";
    return json(route, {
      status,
      items: status === "completed" ? [] : ITEMS,
      next_cursor: "",
      generated_at: new Date().toISOString(),
    });
  });
  await page.route(core(/^\/inbox\/[^/]+$/), (route) => {
    const id = decodeURIComponent(corePathOf(route.request().url()).slice(7));
    return json(route, {
      item: ITEMS.find((entry) => entry.id === id) ?? null,
    });
  });
}

const heading = (page) => page.locator("[data-inbox-heading]");
const body = (page) => page.locator("[data-inbox-body]");
const contextLine = (page) => page.locator("[data-inbox-context-subject]");

/** Height of the heading box, in CSS pixels. */
async function headingHeight(page) {
  return (await heading(page).first().boundingBox())?.height ?? 0;
}

for (const viewport of [
  { name: "desktop", ...DESKTOP },
  { name: "phone", ...PHONE },
]) {
  test.describe(`inbox item readability @ ${viewport.name}`, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height } });

    test.beforeEach(async ({ page }) => {
      test.setTimeout(90_000);
      await installApi(page);
    });

    test("a legacy ask gets a heading, and the rest reads as body prose", async ({
      page,
    }) => {
      await page.goto(`${INBOX_PATH}/inbox-legacy`);
      await expect(heading(page)).toHaveText(LEGACY_HEADING, FIRST_PAINT);

      // One or two lines, never 600 characters of bold.
      expect(await headingHeight(page)).toBeLessThan(80);

      // What came off the title is below, in normal-weight prose.
      await expect(body(page)).toContainText("Context: the detail pane prints");
      await expect(body(page)).toContainText("Recommendation: ship now");
      await expect(body(page)).not.toContainText(LEGACY_HEADING);
      await expectCleanLayout(page, "legacy ask detail");

      // This heading fits two lines at both widths, so nothing is hidden and
      // there is nothing to expand. The phone-only test below covers the
      // heading that does not fit.
      await expect(page.locator("[data-inbox-heading-expand]")).toHaveCount(0);
    });

    test("a well-formed ask renders its sections", async ({ page }) => {
      await page.goto(`${INBOX_PATH}/inbox-well-formed`);
      await expect(heading(page)).toHaveText(
        "Ship the inbox rendering fix in 0.12.58?",
        FIRST_PAINT,
      );

      const rendered = body(page).locator(".markdown-rendered");
      await expect(
        rendered.getByRole("heading", { name: "Decision" }),
      ).toBeVisible();
      await expect(
        rendered.getByRole("heading", { name: "Options" }),
      ).toBeVisible();
      await expect(
        rendered.getByRole("heading", { name: "Evidence" }),
      ).toBeVisible();
      // Options read as a list, with the markdown emphasis rendered.
      await expect(rendered.locator("ul > li")).toHaveCount(4);
      await expect(rendered.locator("strong").first()).toHaveText("Ship now");
      // A SHA reads as code, and wraps rather than scrolling the page.
      await expect(rendered.locator("code").first()).toHaveText(LONG_SHA);
      // No raw HTML surface: the renderer is the sanitized one.
      await expect(rendered.locator("script")).toHaveCount(0);

      // The recommendation stays highlighted where it always was.
      await expect(page.getByText("Recommended")).toBeVisible();
      await expectCleanLayout(page, "well-formed ask detail");
    });

    test("the card created for an ask never reprints it", async ({ page }) => {
      await page.goto(`${INBOX_PATH}/inbox-duplicate-card`);
      await expect(heading(page)).toHaveText(LEGACY_HEADING, FIRST_PAINT);

      await expect(contextLine(page)).toHaveAttribute(
        "data-inbox-context-repeats-ask",
        "true",
      );
      await expect(contextLine(page)).toContainText(
        "Card created for this ask",
      );
      await expect(contextLine(page)).not.toContainText("Recommendation:");
      // The link still goes to the card.
      await expect(contextLine(page).getByRole("link")).toHaveAttribute(
        "href",
        /\/tasks\/card%3Aanx-742$/,
      );

      // The ask text appears once on the page: in the body.
      const occurrences = await page
        .getByText("Options: ship now, which is renderer-only")
        .count();
      expect(occurrences).toBe(1);
      await expectCleanLayout(page, "ask whose card repeats it");
    });

    test("the list leads a row with the heading and the pane splits the ask", async ({
      page,
    }) => {
      await page.goto(INBOX_PATH);
      await expect(
        page.getByRole("heading", { name: "Inbox", exact: true }),
      ).toBeVisible(FIRST_PAINT);

      const row = page.locator('[data-inbox-row="inbox:inbox-legacy"]');
      const rowTitle = row.locator("[data-inbox-row-title]");
      await expect(rowTitle).toHaveText(LEGACY_HEADING);
      // The full title is a hover away, not on the row.
      await expect(rowTitle).toHaveAttribute("title", LEGACY_TITLE);

      await row.click();
      await expect(heading(page)).toHaveText(LEGACY_HEADING);
      await expect(body(page)).toContainText("Recommendation: ship now");
      await expectCleanLayout(page, "inbox list pane after picking a row");
    });
  });
}

test.describe("inbox item heading clamp @ phone", () => {
  test.use({ viewport: PHONE });

  test.beforeEach(async ({ page }) => {
    test.setTimeout(90_000);
    await installApi(page);
  });

  test("a heading too tall for two lines is clamped, and opens", async ({
    page,
  }) => {
    await page.goto(`${INBOX_PATH}/inbox-no-sentence`);
    const head = heading(page).first();
    await expect(head).toContainText("Confirm whether the release manager", {
      timeout: 20_000,
    });

    // Two lines of 17px/24px type, and the ellipsis says there is more.
    const clamped = await headingHeight(page);
    expect(clamped).toBeLessThanOrEqual(56);
    await expect(head).toContainText("…");
    // What the clamp cut is in the body, so nothing is only on hover.
    await expect(body(page)).toContainText("contract work that Sol is");
    await expectCleanLayout(page, "clamped heading");

    const expand = page.locator("[data-inbox-heading-expand]");
    await expect(expand).toHaveText("Show full title");
    await expand.click();
    await expect(head).toHaveAttribute("data-inbox-heading-expanded", "true");
    expect(await headingHeight(page)).toBeGreaterThan(clamped);
    await expect(expand).toHaveText("Show less");
    await expectCleanLayout(page, "expanded heading");

    await expand.click();
    await expect(head).not.toHaveAttribute("data-inbox-heading-expanded");
    expect(await headingHeight(page)).toBe(clamped);
  });
});
