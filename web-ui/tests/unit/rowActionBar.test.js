import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const rowActionBarPath = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../src/lib/components/RowActionBar.svelte",
);

describe("RowActionBar", () => {
  it("exposes hover-reveal and touch-visible toolbar classes", () => {
    const src = readFileSync(rowActionBarPath, "utf8");
    expect(src).toContain("row-action-bar");
    expect(src).toContain("opacity-0");
    expect(src).toContain("group-hover/");
    expect(src).toContain("focus-within:opacity-100");
    expect(src).toContain("@media (hover: none)");
  });

  it("defaults group name to row", () => {
    const src = readFileSync(rowActionBarPath, "utf8");
    expect(src).toContain('groupName = "row"');
  });
});
