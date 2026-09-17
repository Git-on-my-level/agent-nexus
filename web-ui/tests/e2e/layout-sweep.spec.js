import { test } from "@playwright/test";

import {
  QA_SCENES,
  buildSceneUrl,
  installQaEnvironment,
  installQaRoutes,
} from "../../scripts/qa-visual.mjs";
import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

/**
 * Runs the geometry audit over every QA visual scene (the same seeded,
 * fully mocked states `pnpm qa:diff` screenshots) at each audit viewport.
 * Adding a scene to `QA_SCENES` gets it audited here for free.
 */
for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`layout sweep @ ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      colorScheme: "dark",
      reducedMotion: "reduce",
    });

    for (const scene of QA_SCENES) {
      test(scene.name, async ({ page, baseURL }) => {
        await installQaEnvironment(page, scene);
        await installQaRoutes(page, scene);
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
