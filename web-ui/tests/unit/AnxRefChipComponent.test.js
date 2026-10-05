// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import AnxRefChip from "../../src/lib/components/AnxRefChip.svelte";
import {
  refResolveExample,
  refResolveExampleRequest,
} from "../../src/lib/fixtures/refResolveExample.js";
import { indexResolvedRefs } from "../../src/lib/refResolve.js";

const resolved = indexResolvedRefs(refResolveExample, refResolveExampleRequest);

const mount = (refValue, props = {}) =>
  render(AnxRefChip, {
    refValue,
    resolved,
    organizationSlug: "scaling",
    workspaceSlug: "anx",
    ...props,
  });

afterEach(() => cleanup());

describe("AnxRefChip", () => {
  it("renders a resolved task as a link with its title and kind", () => {
    const { container } = mount("card:initiative-plans");
    const chip = container.querySelector("[data-anx-ref]");
    expect(chip.tagName).toBe("A");
    expect(chip.getAttribute("href")).toBe(
      "/o/scaling/w/anx/tasks/initiative-plans",
    );
    expect(chip.textContent).toContain("Initiative plans on cards");
    expect(chip.textContent).toContain("Task");
  });

  it("carries the status on the dot so a reader sees state at a glance", () => {
    const { container } = mount("card:shared-report-contracts");
    expect(container.querySelector("[data-status]").dataset.status).toBe(
      "done",
    );
    expect(container.querySelector(".anx-ref-chip__dot").dataset.tone).toBe(
      "ok",
    );
  });

  it("marks a blocked ref danger", () => {
    const { container } = mount("card:pushed-series");
    expect(container.querySelector(".anx-ref-chip__dot").dataset.tone).toBe(
      "danger",
    );
  });

  it("renders an unresolvable ref as a dashed not-found chip, not nothing", () => {
    const { container } = mount("card:deleted-thing");
    const chip = container.querySelector("[data-anx-ref]");
    expect(chip.tagName).toBe("SPAN");
    expect(chip.classList.contains("anx-ref-chip--missing")).toBe(true);
    expect(chip.textContent).toContain("card:deleted-thing");
    expect(chip.textContent).toContain("not found");
    expect(chip.getAttribute("href")).toBeNull();
    expect(chip.getAttribute("aria-label")).toBe(
      "Not found: card:deleted-thing",
    );
  });

  it("keeps an unresolvable chip focusable so the keyboard can reach its preview", () => {
    const { container } = mount("card:deleted-thing");
    expect(
      container.querySelector("[data-anx-ref]").getAttribute("tabindex"),
    ).toBe("0");
  });

  it("opens an external pull request in a new tab, safely", () => {
    const { container } = mount(
      "https://github.com/Git-on-my-level/agent-nexus/pull/246",
    );
    const chip = container.querySelector("[data-anx-ref]");
    expect(chip.getAttribute("target")).toBe("_blank");
    expect(chip.getAttribute("rel")).toBe("noreferrer noopener");
    expect(chip.textContent).toContain("PR");
  });

  it("gives a board ref no link because there is no board surface", () => {
    const { container } = mount("board:release-b");
    const chip = container.querySelector("[data-anx-ref]");
    expect(chip.tagName).toBe("SPAN");
    expect(chip.classList.contains("anx-ref-chip--missing")).toBe(false);
    expect(chip.textContent).toContain("Release B");
  });

  it("hides the kind label when the surrounding column already says it", () => {
    const { container } = mount("card:initiative-plans", { showKind: false });
    expect(container.querySelector(".anx-ref-chip__kind")).toBeNull();
  });

  it("asks for a preview on hover and on focus, and closes on leave and blur", async () => {
    const onpreview = vi.fn();
    const onpreviewclose = vi.fn();
    const { container } = mount("card:initiative-plans", {
      onpreview,
      onpreviewclose,
    });
    const chip = container.querySelector("[data-anx-ref]");

    await fireEvent.mouseEnter(chip);
    expect(onpreview).toHaveBeenCalledTimes(1);
    expect(onpreview.mock.calls[0][0]).toMatchObject({
      title: "Initiative plans on cards",
    });
    expect(onpreview.mock.calls[0][1]).toBe(chip);

    await fireEvent.mouseLeave(chip);
    expect(onpreviewclose).toHaveBeenCalledTimes(1);

    await fireEvent.focusIn(chip);
    expect(onpreview).toHaveBeenCalledTimes(2);
    await fireEvent.focusOut(chip);
    expect(onpreviewclose).toHaveBeenCalledTimes(2);
  });

  it("never fetches for itself", () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount("card:initiative-plans");
    expect(fetchSpy).not.toHaveBeenCalled();
    fetchSpy.mockRestore();
  });
});
