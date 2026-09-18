import { expect as baseExpect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

const expect = baseExpect.configure({ timeout: 15_000 });

/**
 * `formatTimestamp` returns a RELATIVE phrase under 7 days ("3h ago") and an
 * absolute date beyond it ("Mar 5, 2026"). The archived banner used to prefix
 * it with "on", which read as "was archived on 3h ago". These tests pin the
 * copy for both branches, and that the exact instant is still reachable from
 * the title.
 */

const ROOT = "/o/local/w/local";
const THREAD_ID = "topic-archived-copy";

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

// Pin "now" to the real clock at run start: the fixtures below are built from
// the same instant, so the server render and the hydrated client agree, and
// nothing drifts across a long run.
const NOW = new Date();
const before = (ms) => new Date(NOW.getTime() - ms).toISOString();

const PRINCIPALS = [
  { id: "actor-operator", display_name: "Operator", tags: ["human"] },
];

const TIMELINE = [
  {
    id: "evt-archived-copy",
    type: "message",
    ts: before(4 * HOUR),
    actor_id: "actor-operator",
    text: "Vendor reader is down; archiving until it is back.",
  },
];

function archivedTopic(archivedAt) {
  return {
    id: THREAD_ID,
    type: "process",
    title: "Vendor reconciliation",
    status: "active",
    current_summary: "Archived while the vendor reader is down.",
    updated_at: before(20 * MIN),
    updated_by: "actor-operator",
    archived_at: archivedAt,
    archived_by: "actor-operator",
  };
}

/** The banner sentence, whitespace-normalised. */
async function bannerText(page) {
  const banner = page.getByText(/was archived/);
  await expect(banner).toBeVisible();
  return (await banner.innerText()).replace(/\s+/g, " ").trim();
}

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`archived banner copy @ ${viewport.name}`, () => {
    test.describe.configure({ timeout: 120_000 });
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      // formatTimestamp's absolute branch goes through toLocaleDateString.
      timezoneId: "UTC",
      locale: "en-US",
    });

    test("a recent archive reads as a relative phrase, with no 'on'", async ({
      page,
    }) => {
      await installWorkspaceApi(page, {
        principals: PRINCIPALS,
        // 3h20m ago -> formatTimestamp's "3h ago" bucket.
        topic: archivedTopic(before(3 * HOUR + 20 * MIN)),
        timeline: TIMELINE,
      });
      await page.clock.setFixedTime(NOW);
      await page.goto(`${ROOT}/threads/${THREAD_ID}?tab=messages`);

      const text = await bannerText(page);
      expect(text).toMatch(/was archived 3h ago/);
      // The bug: "This thread was archived on 3h ago".
      expect(text).not.toMatch(/archived on /);

      // The precise instant is still one hover away.
      await expect(
        page.locator("[title]").filter({ hasText: /was archived/ }),
      ).toHaveAttribute("title", /\d{4}/);

      await expectCleanLayout(page, "archived banner, relative timestamp", {
        scrollPositions: ["top", "bottom"],
      });
    });

    test("an old archive reads as an absolute date, with no 'on'", async ({
      page,
    }) => {
      await installWorkspaceApi(page, {
        principals: PRINCIPALS,
        // Past the 7-day cutoff -> formatTimestamp's absolute branch.
        topic: archivedTopic(before(40 * DAY)),
        timeline: TIMELINE,
      });
      await page.clock.setFixedTime(NOW);
      await page.goto(`${ROOT}/threads/${THREAD_ID}?tab=messages`);

      const text = await bannerText(page);
      // "was archived Mar 5, 2026" - the month/day is calendar-dependent, the
      // shape is not.
      expect(text).toMatch(/was archived [A-Z][a-z]{2} \d{1,2}, \d{4}/);
      expect(text).not.toMatch(/archived on /);
      expect(text).not.toMatch(/ago/);

      await expectCleanLayout(page, "archived banner, absolute timestamp", {
        scrollPositions: ["top", "bottom"],
      });
    });

    test("the trashed banner does not say 'at 3h ago' either", async ({
      page,
    }) => {
      await installWorkspaceApi(page, {
        principals: PRINCIPALS,
        topic: {
          ...archivedTopic(""),
          trashed_at: before(3 * HOUR + 20 * MIN),
          trashed_by: "actor-operator",
          trash_reason: "Superseded by the new vendor board.",
        },
        timeline: TIMELINE,
      });
      await page.clock.setFixedTime(NOW);
      await page.goto(`${ROOT}/threads/${THREAD_ID}?tab=messages`);

      // The whole "Trashed by <actor> <when>" line, not the "Trashed" span.
      const trashedLine = page
        .locator("p")
        .filter({ hasText: /^Trashed\b/ })
        .first();
      await expect(trashedLine).toBeVisible();
      const text = (await trashedLine.innerText()).replace(/\s+/g, " ").trim();
      expect(text).toMatch(/3h ago/);
      expect(text).not.toMatch(/at 3h ago/);

      await expectCleanLayout(page, "trashed banner, relative timestamp", {
        scrollPositions: ["top", "bottom"],
      });
    });
  });
}
