// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import AttachmentChip from "../../src/lib/components/AttachmentChip.svelte";
import {
  buildPrimitiveRefRoutes,
  resolveRefLink,
} from "../../src/lib/refLinkModel";

afterEach(cleanup);

function resolvedAttachment(rows, id = "att-1") {
  const artifactRoutesById = buildPrimitiveRefRoutes({
    artifacts: rows,
    events: [],
    cards: [],
    documents: [],
    threadId: "thread-1",
  }).artifactRoutesById;
  return resolveRefLink(`artifact:${id}`, {
    threadId: "thread-1",
    boardId: "",
    humanize: true,
    artifactRoutesById,
    eventRoutesById: {},
    workspaceSlug: "ws",
    organizationSlug: "org",
  });
}

describe("AttachmentChip", () => {
  it("shows pending upload state", () => {
    const resolved = resolveRefLink("artifact:new-id", {
      threadId: "",
      boardId: "",
      humanize: true,
      artifactRoutesById: {},
      eventRoutesById: {},
      workspaceSlug: "ws",
      organizationSlug: "org",
    });
    const { container } = render(AttachmentChip, {
      resolved,
      pending: true,
      artifactOverlay: {
        original_filename: "x.png",
        content_type: "image/png",
      },
      size: "inline",
    });
    const busy = container.querySelector('[aria-busy="true"]');
    expect(busy).toBeTruthy();
    expect(busy?.getAttribute("aria-label") ?? "").toMatch(/Uploading/i);
  });

  it("renders ready attachment with filename and type", () => {
    const resolved = resolvedAttachment([
      {
        id: "att-1",
        kind: "attachment",
        original_filename: "notes.md",
        content_type: "text/markdown",
        byte_size: 2048,
      },
    ]);
    const { container } = render(AttachmentChip, {
      resolved,
      size: "inline",
    });
    // Artifact detail pages are gone; ready chips render inert text + download.
    const chip = container.querySelector('span[aria-label^="Attachment:"]');
    expect(chip).toBeTruthy();
    expect(chip?.textContent ?? "").toContain("notes.md");
    // Type badge/size moved out of the DOM into the aria-label on inert chips.
    expect(chip?.getAttribute("aria-label") ?? "").toMatch(/MD/);
    expect(chip?.getAttribute("aria-label") ?? "").toMatch(/Attachment/i);
    expect(container.querySelector("a.attachment-chip-link")).toBeNull();
  });

  it("shows missing state when unrouted and there is no artifact link", () => {
    const resolved = resolveRefLink("artifact:missing-uuid-here", {
      threadId: "",
      boardId: "",
      humanize: true,
      artifactRoutesById: {},
      eventRoutesById: {},
      workspaceSlug: "",
      organizationSlug: "",
    });
    const { container } = render(AttachmentChip, { resolved, size: "inline" });
    expect(container.textContent ?? "").toMatch(/unavailable/i);
  });

  it("omits sidebar download control in tight size", () => {
    const resolved = resolvedAttachment([
      {
        id: "att-1",
        kind: "attachment",
        original_filename: "notes.md",
        content_type: "text/markdown",
        byte_size: 2048,
      },
    ]);
    const { container } = render(AttachmentChip, {
      resolved,
      size: "tight",
    });
    expect(
      container.querySelector("button[aria-label^='Download']"),
    ).toBeFalsy();
    const chip = container.querySelector('span[aria-label^="Attachment:"]');
    expect(chip?.className ?? "").toMatch(/text-\[11px\]/);
  });

  it("renders nothing when trashed_at is set on merged metadata", () => {
    const resolved = resolvedAttachment(
      [
        {
          id: "att-trash",
          kind: "attachment",
          original_filename: "old.bin",
          content_type: "application/octet-stream",
          byte_size: 10,
          trashed_at: "2025-01-01T00:00:00Z",
        },
      ],
      "att-trash",
    );
    const { container } = render(AttachmentChip, {
      resolved,
      size: "inline",
    });
    expect(container.querySelector(".attachment-chip-root")).toBeFalsy();
  });
});
