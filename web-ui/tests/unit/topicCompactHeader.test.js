import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const headerPath = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../src/lib/components/topic-detail/TopicDetailHeader.svelte",
);

const pagePath = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../src/lib/pages/WorkspaceTopicThreadDetailPage.svelte",
);

const topRowPath = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../src/lib/components/WorkspaceResourceTopRow.svelte",
);

describe("topic detail header", () => {
  it("TopicDetailHeader uses breadcrumb shell without lifecycle badge", () => {
    const src = readFileSync(headerPath, "utf8");
    expect(src).toContain("WorkspaceResourceTopRow");
    expect(src).toContain("showDesktop");
    expect(src).not.toContain("compact = false");
    expect(src).not.toContain('aria-label="Topic channel"');
    expect(src).not.toContain("BOARD_LIFECYCLE_STATE_LABELS");
    expect(src).not.toContain("topicLifecycleBadgeClass");
  });

  it("WorkspaceTopicThreadDetailPage uses shared tab list and compact header", () => {
    const src = readFileSync(pagePath, "utf8");
    expect(src).toContain("WorkspaceResourceTabList");
    expect(src).toContain("dense showDesktop={false}");
    expect(src).toContain("dense");
    expect(src).not.toContain("dense={isMessagesTab}");
    expect(src).not.toContain("showDesktop={!isMessagesTab}");
    expect(src).not.toContain("compact={isMessagesTab}");
  });

  it("WorkspaceResourceTopRow supports dense dock layouts", () => {
    const src = readFileSync(topRowPath, "utf8");
    expect(src).toContain("dense = false");
    expect(src).toContain("showDesktop = true");
  });
});
