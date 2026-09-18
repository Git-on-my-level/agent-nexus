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
  it("TopicDetailHeader uses breadcrumb shell with an h1 title", () => {
    const src = readFileSync(headerPath, "utf8");
    expect(src).toContain("WorkspaceResourceTopRow");
    // The desktop title block was removed; no call site may reintroduce it.
    expect(src).not.toContain("showDesktop");
    expect(src).not.toContain("desktopAriaLabel");
    expect(src).toContain("<h1");
    expect(src).not.toContain("compact = false");
    expect(src).not.toContain('aria-label="Topic channel"');
    expect(src).not.toContain("BOARD_LIFECYCLE_STATE_LABELS");
    expect(src).not.toContain("topicLifecycleBadgeClass");
  });

  it("WorkspaceTopicThreadDetailPage uses shared tab list and compact header", () => {
    const src = readFileSync(pagePath, "utf8");
    expect(src).toContain("WorkspaceResourceTabList");
    expect(src).toContain("{detailAsTopic} dense");
    expect(src).not.toContain("showDesktop");
    expect(src).not.toContain("dense={isMessagesTab}");
    expect(src).not.toContain("showDesktop={!isMessagesTab}");
    expect(src).not.toContain("compact={isMessagesTab}");
  });

  it("WorkspaceResourceTopRow supports dense dock layouts", () => {
    const src = readFileSync(topRowPath, "utf8");
    expect(src).toContain("dense = false");
  });

  it("WorkspaceResourceTopRow has no unreachable desktop title block", () => {
    // Every call site passed `showDesktop={false}`, so the `desktop` snippet,
    // its aria label and its `{#if}` branch could never render. Keep them gone.
    const src = readFileSync(topRowPath, "utf8");
    expect(src).not.toContain("showDesktop");
    expect(src).not.toContain("desktopAriaLabel");
    expect(src).not.toContain("{@render desktop()}");
  });
});
