import { test } from "@playwright/test";

import {
  QA_SCENES,
  buildSceneUrl,
  installQaEnvironment,
  installQaRoutes,
} from "../../scripts/qa-visual.mjs";
import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };

const WS = "/o/local/w/local";
const EMPTY_BRIEF = {
  ...brief,
  decisions: [],
  initiatives: [],
  answered_for_you: [],
};

/**
 * The QA scenes for screens that no longer exist (the Inbox, Overview and
 * Tasks pages, and the workspace root that used to be Overview) are replaced
 * by the scenes below: Decide, Watch and Initiatives read the brief.
 */
const RETIRED = (scene) =>
  /\/(?:inbox|tasks)(?:\/|$|\?)/.test(scene.path) || scene.path === WS;

const briefScene = (name, path, body, waitFor) => ({
  name,
  path,
  workspaceMode: "workspace-default",
  brief: body,
  waitFor,
});

const BRIEF_SCENES = [
  briefScene("decide-populated", `${WS}/decide`, brief, (page) =>
    page.locator("[data-decision-ref]").first().waitFor(),
  ),
  briefScene("decide-empty", `${WS}/decide`, EMPTY_BRIEF, (page) =>
    page.locator("[data-decide-count]").waitFor(),
  ),
  briefScene("decide-permalink", `${WS}/decide/ask-91`, brief, (page) =>
    page.locator("[data-decision-ref]").first().waitFor(),
  ),
  briefScene("watch-by-state", `${WS}/watch`, brief, (page) =>
    page.locator("[data-watch-row]").first().waitFor(),
  ),
  briefScene("watch-by-stage", `${WS}/watch?view=board`, brief, (page) =>
    page.locator("[data-watch-row]").first().waitFor(),
  ),
  briefScene("initiatives", `${WS}/initiatives`, brief, (page) =>
    page.getByRole("heading", { name: "Initiatives", exact: true }).waitFor(),
  ),
  {
    // The old command-palette scene opened on the Inbox page.
    name: "command-palette-open",
    path: `${WS}/decide`,
    workspaceMode: "workspace-default",
    brief,
    waitFor: async (page) => {
      await page.locator("[data-decision-ref]").first().waitFor();
      if (await page.locator(".shell-search-trigger").isVisible()) {
        await page.click(".shell-search-trigger");
      } else {
        await page.keyboard.press("ControlOrMeta+k");
      }
      await page.fill(".cmd-input", "launch");
      await page.waitForSelector(".cmd-result-row");
    },
  },
];

/*
 * Two QA scenes wait on shell furniture that moved: the PM slot
 * (`[data-pm-nav]`) and the "Waiting on you" nav hint. They wait on the page
 * itself here.
 */
const REWAITED = {
  "workspace-access": async (page) => {
    await page.waitForSelector('[data-host-enrollment="henr_qa_ci"]');
    await page.waitForSelector("text=reviewer");
    await page
      .getByRole("button", { name: "Revoke administration", exact: true })
      .waitFor();
  },
  "workspace-agents": async (page) => {
    await page.waitForSelector('[data-agents-fold="inactive"]');
  },
};

const SCENES = [
  ...QA_SCENES.filter(
    (scene) => !RETIRED(scene) && scene.name !== "command-palette-open",
  ).map((scene) =>
    REWAITED[scene.name] ? { ...scene, waitFor: REWAITED[scene.name] } : scene,
  ),
  ...BRIEF_SCENES,
];

/**
 * Runs the geometry audit over every QA visual scene (the same seeded,
 * fully mocked states `pnpm qa:diff` screenshots) at each audit viewport.
 * Adding a scene to `QA_SCENES` gets it audited here for free; the Decide,
 * Watch and Initiatives scenes are defined here because they read `/brief`.
 */
for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`layout sweep @ ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      colorScheme: "dark",
      reducedMotion: "reduce",
    });

    for (const scene of SCENES) {
      test(scene.name, async ({ page, baseURL }) => {
        await installQaEnvironment(page, scene);
        await installQaRoutes(page, scene);
        if (scene.brief)
          await page.route(
            (url) => url.pathname === "/brief",
            (route) =>
              route.fulfill({
                status: 200,
                contentType: "application/json",
                body: JSON.stringify({
                  ...scene.brief,
                  generated_at: new Date().toISOString(),
                }),
              }),
          );
        await page.goto(buildSceneUrl(baseURL, scene.path), {
          waitUntil: "domcontentloaded",
        });
        await scene.waitFor(page);
        await page.evaluate(() => document.fonts?.ready);
        await expectCleanLayout(page, scene.name, {
          scrollPositions: ["current", "bottom"],
        });
      });
    }
  });
}
