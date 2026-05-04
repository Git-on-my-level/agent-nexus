import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const testDir = dirname(fileURLToPath(import.meta.url));
const appCssPath = resolve(testDir, "../../src/app.css");
const appHtmlPath = resolve(testDir, "../../src/app.html");

function appCss() {
  return readFileSync(appCssPath, "utf8");
}

function appHtml() {
  return readFileSync(appHtmlPath, "utf8");
}

describe("mobile text control CSS", () => {
  it("keeps iOS Safari from focus-zooming compact text controls", () => {
    const css = appCss();

    expect(css).toMatch(
      /@media\s*\(hover:\s*none\)\s*and\s*\(pointer:\s*coarse\)\s*\{[\s\S]*?input\[type="text"\],[\s\S]*?textarea,[\s\S]*?\.ui-input,[\s\S]*?\.cdm-prose-input,[\s\S]*?font-size:\s*16px\s*!important;/,
    );
  });

  it("does not disable user zoom in the viewport meta tag", () => {
    expect(appHtml()).not.toMatch(
      /user-scalable\s*=\s*no|maximum-scale\s*=\s*1/,
    );
  });
});
