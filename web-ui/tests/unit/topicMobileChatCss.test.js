import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const cssPath = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../src/app.css",
);

function appCss() {
  return readFileSync(cssPath, "utf8");
}

describe("topic mobile chat CSS", () => {
  it("pins the topic messages composer above the bottom nav", () => {
    const css = appCss();

    expect(css).toMatch(
      /\.page-dock-layout--fixed-mobile-chat\.page-dock-layout--topic-messages\s+\.page-dock-feed\s*\{[\s\S]*?position:\s*fixed;[\s\S]*?top:\s*6\.25rem;[\s\S]*?bottom:\s*calc\(3\.5rem \+ env\(safe-area-inset-bottom, 0px\)\);/,
    );
    expect(css).toMatch(
      /\.page-dock-layout--topic-messages\s+\.dd-surface,[\s\S]*?\.page-dock-layout--topic-messages\s+\.msgtab-wrap--pin\s*\{[\s\S]*?height:\s*100%;[\s\S]*?max-height:\s*100%;[\s\S]*?overflow:\s*hidden;/,
    );
  });

  it("keeps the topic override after the generic mobile drawer clamp", () => {
    const css = appCss();
    const genericClamp = css.indexOf(
      ".page-dock-layout--fixed-mobile-chat\n      .page-dock-feed:has([data-mobile-chat-expanded]) {",
    );
    const topicOverride = css.indexOf(
      "Topic Messages owns a fixed viewport slot from tabs to bottom nav.",
    );

    expect(genericClamp).toBeGreaterThan(-1);
    expect(topicOverride).toBeGreaterThan(genericClamp);
    expect(css.slice(topicOverride)).toMatch(
      /\.page-dock-layout--fixed-mobile-chat\.page-dock-layout--topic-messages\s+\.page-dock-feed:has\(\[data-mobile-chat-expanded\]\)\s*\{[\s\S]*?height:\s*auto;[\s\S]*?max-height:\s*none;/,
    );
  });
});
