// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import StatusPill from "../../src/lib/hosted/StatusPill.svelte";

afterEach(cleanup);

const LONG_LABEL =
  "Placement unavailable: host reported ENOSPC on the overlay upper directory";

function pill(container) {
  return container.querySelector("[data-testid='status-pill']");
}

const KNOWN_SHORT_STATUSES = [
  "ready",
  "active",
  "provisioning",
  "pending",
  "failed",
  "error",
  "degraded",
  "suspended",
];

describe("StatusPill", () => {
  it("renders the status when no label is given", () => {
    const { container } = render(StatusPill, { status: "ready" });
    expect(pill(container).textContent.trim()).toBe("ready");
  });

  it("prefers an explicit label over the status", () => {
    const { container } = render(StatusPill, {
      status: "suspended",
      label: "Draining",
    });
    expect(pill(container).textContent.trim()).toBe("Draining");
  });

  it("falls back to 'unknown' with neither status nor label", () => {
    const { container } = render(StatusPill, {});
    expect(pill(container).textContent.trim()).toBe("unknown");
  });

  describe("shrink safety", () => {
    // The label is arbitrary (call sites pass telemetry strings), so the pill
    // has to be able to shrink inside its row. `shrink-0` pinned it at its
    // intrinsic width and pushed row siblings out / overflowed the container.
    it("is not pinned with shrink-0", () => {
      const { container } = render(StatusPill, { status: "ready" });
      expect(pill(container).className.split(/\s+/)).not.toContain("shrink-0");
    });

    it("can shrink and ellipsize on one line", () => {
      const { container } = render(StatusPill, {
        status: "failed",
        label: LONG_LABEL,
      });
      const classes = pill(container).className.split(/\s+/);
      // min-w-0 defeats a flex item's automatic minimum size; max-w-full keeps
      // it inside a non-flex parent; truncate gives the one-line ellipsis.
      expect(classes).toContain("min-w-0");
      expect(classes).toContain("max-w-full");
      expect(classes).toContain("truncate");
    });

    it("keeps the full text in the DOM and on the title for hover / AT", () => {
      const { container } = render(StatusPill, {
        status: "failed",
        label: LONG_LABEL,
      });
      // Truncation is visual only: assistive tech still reads the whole label.
      expect(pill(container).textContent.trim()).toBe(LONG_LABEL);
      expect(pill(container).getAttribute("title")).toBe(LONG_LABEL);
    });

    it("never truncates in markup - short statuses keep their full text", () => {
      for (const status of KNOWN_SHORT_STATUSES) {
        const { container } = render(StatusPill, { status });
        const el = pill(container);
        expect(el.textContent.trim()).toBe(status);
        expect(el.getAttribute("title")).toBe(status);
        cleanup();
      }
    });
  });

  describe("tone", () => {
    it.each([
      ["ready", "bg-ok-soft"],
      ["active", "bg-ok-soft"],
      ["provisioning", "bg-warn-soft"],
      ["pending", "bg-warn-soft"],
      ["failed", "bg-danger-soft"],
      ["error", "bg-danger-soft"],
      ["degraded", "bg-danger-soft"],
      ["suspended", "bg-danger-soft"],
      ["something-else", "bg-panel-hover"],
    ])("maps %s to %s", (status, expected) => {
      const { container } = render(StatusPill, { status });
      expect(pill(container).className.split(/\s+/)).toContain(expected);
    });

    it("tones from status even when the label is arbitrary", () => {
      const { container } = render(StatusPill, {
        status: "failed",
        label: LONG_LABEL,
      });
      expect(pill(container).className.split(/\s+/)).toContain(
        "bg-danger-soft",
      );
    });
  });
});
