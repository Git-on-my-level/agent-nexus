import {
  mkdtempSync,
  readFileSync,
  mkdirSync,
  writeFileSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { checkCommercialBoundary } from "../../scripts/check-commercial-boundary.mjs";
import { composeUi } from "../../scripts/compose-ui.mjs";

const roots = [];
afterEach(() =>
  roots
    .splice(0)
    .forEach((root) => rmSync(root, { recursive: true, force: true })),
);
function fixture() {
  const root = mkdtempSync(path.join(tmpdir(), "anx-boundary-"));
  roots.push(root);
  const sourceRoot = path.join(root, "source", "src");
  const bundleRoot = path.join(root, "bundle");
  mkdirSync(sourceRoot, { recursive: true });
  mkdirSync(bundleRoot);
  return { root, sourceRoot, bundleRoot };
}
describe("OSS commercial boundary", () => {
  it("accepts a standalone shell and requires source/build output", () => {
    const { sourceRoot, bundleRoot } = fixture();
    writeFileSync(
      path.join(sourceRoot, "shell.js"),
      'export const heading = "Workspace";',
    );
    expect(checkCommercialBoundary({ sourceRoot, bundleRoot })).toEqual([]);
    expect(
      checkCommercialBoundary({
        sourceRoot,
        bundleRoot: `${bundleRoot}/missing`,
      }),
    ).not.toEqual([]);
    expect(
      checkCommercialBoundary({ sourceRoot: `${sourceRoot}/missing` }),
    ).not.toEqual([]);
  });
  it("rejects routes, imported commercial modules, plan copy and prices in server/client output", () => {
    const { sourceRoot, bundleRoot } = fixture();
    mkdirSync(path.join(sourceRoot, "routes/hosted/billing"), {
      recursive: true,
    });
    writeFileSync(
      path.join(sourceRoot, "shell.ts"),
      'import "$lib/hosted/planCatalog.js";',
    );
    writeFileSync(
      path.join(bundleRoot, "server.js"),
      'const card={price:"$10",ctaLabel:"Upgrade to Pro"};',
    );
    const hits = checkCommercialBoundary({ sourceRoot, bundleRoot });
    expect(hits.some((hit) => hit.includes("Forbidden source directory"))).toBe(
      true,
    );
    expect(hits.some((hit) => hit.includes("commercial import"))).toBe(true);
    expect(hits.some((hit) => hit.includes("plan price"))).toBe(true);
    expect(hits.some((hit) => hit.includes("Upgrade to Pro"))).toBe(true);
  });
});
describe("UI composition", () => {
  it("requires declared replacements and leaves the standalone source unchanged", () => {
    const { root, sourceRoot } = fixture();
    const source = path.dirname(sourceRoot);
    const extension = path.join(root, "extension");
    const output = path.join(root, "output");
    mkdirSync(path.join(sourceRoot, "lib"));
    writeFileSync(path.join(sourceRoot, "lib/provider.js"), "local");
    mkdirSync(path.join(extension, "src/lib"), { recursive: true });
    writeFileSync(path.join(extension, "src/lib/provider.js"), "external");
    writeFileSync(
      path.join(extension, "extension.json"),
      JSON.stringify({ add: ["src/lib"] }),
    );
    expect(() =>
      composeUi({
        sourceRoot: source,
        extensionRoot: extension,
        outputRoot: output,
      }),
    ).toThrow("Undeclared extension replacement");
    writeFileSync(
      path.join(extension, "extension.json"),
      JSON.stringify({ replace: ["src/lib/provider.js"] }),
    );
    expect(
      composeUi({
        sourceRoot: source,
        extensionRoot: extension,
        outputRoot: output,
      }),
    ).toBe(output);
    expect(readFileSync(path.join(sourceRoot, "lib/provider.js"), "utf8")).toBe(
      "local",
    );
    expect(readFileSync(path.join(output, "src/lib/provider.js"), "utf8")).toBe(
      "external",
    );
    expect(() =>
      composeUi({
        sourceRoot: source,
        extensionRoot: extension,
        outputRoot: source,
      }),
    ).toThrow("outside");
  });
});
