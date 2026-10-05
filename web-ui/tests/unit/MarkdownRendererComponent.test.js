// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import MarkdownRenderer from "../../src/lib/components/MarkdownRenderer.svelte";
import { refResolveExample } from "../../src/lib/fixtures/refResolveExample.js";
import { indexResolvedRefs } from "../../src/lib/refResolve.js";

const resolved = indexResolvedRefs(refResolveExample);

const mount = (source, props = {}) =>
  render(MarkdownRenderer, {
    source,
    resolved,
    organizationSlug: "scaling",
    workspaceSlug: "anx",
    ...props,
  });

afterEach(() => cleanup());

describe("MarkdownRenderer", () => {
  it("renders nothing for empty source", () => {
    const { container } = mount("");
    expect(container.querySelector(".markdown-rendered")).toBeNull();
  });

  it("renders GFM task lists, tables and code blocks", () => {
    const { container } = mount(
      "- [x] done\n- [ ] open\n\n| a |\n|---|\n| 1 |\n\n```\nraw\n```\n",
    );
    expect(container.querySelectorAll('input[type="checkbox"]')).toHaveLength(
      2,
    );
    expect(container.querySelector("table")).not.toBeNull();
    expect(container.querySelector("pre code").textContent).toContain("raw");
  });

  it("renders a details disclosure and hides HTML comments", () => {
    const { container } = mount(
      "<!-- fleet-sync:evidence:v1 -->\n\n<details><summary>Why</summary>\n\nBecause.\n\n</details>\n",
    );
    expect(container.querySelector("details summary").textContent).toBe("Why");
    expect(container.textContent).toContain("Because.");
    expect(container.innerHTML).not.toContain("fleet-sync");
  });

  it("mounts a real ref chip for an ANX ref in prose, with its resolved title", () => {
    const { container } = mount("Blocked by card:pushed-series until Friday.");
    const chips = container.querySelectorAll("[data-anx-ref]");
    expect(chips).toHaveLength(1);
    expect(chips[0].textContent).toContain(
      "Pushed series and declared adapters",
    );
    expect(container.textContent).toContain("Blocked by ");
    expect(container.textContent).toContain(" until Friday.");
  });

  it("mounts a GitHub chip for an autolinked pull request", () => {
    const { container } = mount(
      "fixed in https://github.com/Git-on-my-level/agent-nexus/pull/246",
    );
    const chip = container.querySelector("[data-anx-ref]");
    expect(chip).not.toBeNull();
    expect(chip.getAttribute("href")).toBe(
      "https://github.com/Git-on-my-level/agent-nexus/pull/246",
    );
    expect(chip.getAttribute("target")).toBe("_blank");
  });

  it("leaves placeholders as text when chips are turned off", () => {
    const { container } = mount("Blocked by card:pushed-series.", {
      refChips: false,
    });
    expect(container.querySelector("[data-anx-ref]")).toBeNull();
    expect(container.textContent).toContain("card:pushed-series");
  });

  it("renders a single run with no block wrapper in inline mode", () => {
    const { container } = mount("just **a** run", { inline: true });
    expect(container.querySelector("span.markdown-rendered")).not.toBeNull();
    expect(container.querySelector("p")).toBeNull();
  });

  it("drops script markup", () => {
    const { container } = mount("<script>alert(1)</script>safe\n");
    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).toContain("safe");
  });
});
