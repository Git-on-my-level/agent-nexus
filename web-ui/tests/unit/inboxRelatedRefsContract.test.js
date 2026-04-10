import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoWebUiRoot = join(__dirname, "..", "..");

/** Inbox feature code must not treat legacy `refs` as an alias for `related_refs` on items. */
const FORBIDDEN_IN_INBOX_SOURCES =
  /\bitem(?:\?\.)?\.refs\b|\?\?\s*item\.refs\b/;

describe("inbox related_refs contract (static)", () => {
  it("inbox route and inboxUtils do not read item.refs", () => {
    const paths = [
      join(repoWebUiRoot, "src/routes/[workspace]/inbox/+page.svelte"),
      join(repoWebUiRoot, "src/lib/inboxUtils.js"),
    ];
    for (const filePath of paths) {
      const text = readFileSync(filePath, "utf8");
      expect(
        text,
        `forbidden legacy inbox ref access in ${filePath}`,
      ).not.toMatch(FORBIDDEN_IN_INBOX_SOURCES);
    }
  });
});
