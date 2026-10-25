// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { createRawSnippet } from "svelte";
import { afterEach, describe, expect, it } from "vitest";

import CollectionTable from "../../src/lib/components/collection/CollectionTable.svelte";

afterEach(cleanup);

const collection = () => ({
  states: [{ key: "moving", label: "Moving", tone: "good" }],
  counts: { moving: 2 },
  hasMore: false,
  groups: [
    {
      key: "all",
      label: "",
      rows: ["a", "b"].map((id) => ({
        ref: `card:${id}`,
        title: `Row ${id}`,
        state: "moving",
        href: `/initiatives/${id}`,
        subtitle: null,
        owner: null,
        progress: null,
        time: null,
        activity: null,
        next: { label: "You", isViewer: true, href: "/decide" },
        changed: false,
      })),
    },
  ],
});
const details = createRawSnippet((row) => ({
  render: () => `<p data-test-details>${row().title} details</p>`,
}));
const rowEl = (container, id) =>
  container.querySelector(`[data-collection-row="card:${id}"]`);
const toggle = (container, id) =>
  rowEl(container, id).querySelector("[data-collection-toggle]");
const shown = (container, id) =>
  rowEl(container, id).querySelector("[data-collection-details]");

describe("CollectionTable row expand", () => {
  it("opens on a click anywhere on the row and closes on the next", async () => {
    const { container } = render(CollectionTable, {
      collection: collection(),
      details,
    });
    const row = rowEl(container, "a").querySelector(".row");
    expect(shown(container, "a")).toBeNull();
    expect(toggle(container, "a").getAttribute("aria-expanded")).toBe("false");

    await fireEvent.click(row.querySelector("[data-collection-chevron]"));
    expect(shown(container, "a").textContent).toContain("Row a details");
    expect(toggle(container, "a").getAttribute("aria-expanded")).toBe("true");
    expect(toggle(container, "a").getAttribute("aria-controls")).toBe(
      shown(container, "a").id,
    );
    expect(rowEl(container, "a").dataset.expanded).toBe("true");

    await fireEvent.click(row);
    expect(shown(container, "a")).toBeNull();
    expect(toggle(container, "a").getAttribute("aria-expanded")).toBe("false");
  });

  it("toggles from the title button, the keyboard control", async () => {
    const { container } = render(CollectionTable, {
      collection: collection(),
      details,
    });
    const button = toggle(container, "a");
    // A real <button>: Enter and Space activate it natively.
    expect(button.tagName).toBe("BUTTON");
    expect(button.getAttribute("type")).toBe("button");
    await fireEvent.click(button);
    expect(shown(container, "a")).not.toBeNull();
    await fireEvent.click(button);
    expect(shown(container, "a")).toBeNull();
  });

  it("keeps several rows open at once", async () => {
    const { container } = render(CollectionTable, {
      collection: collection(),
      details,
    });
    await fireEvent.click(toggle(container, "a"));
    await fireEvent.click(toggle(container, "b"));
    expect(shown(container, "a")).not.toBeNull();
    expect(shown(container, "b")).not.toBeNull();
  });

  it("leaves the row's own links to do what they do", async () => {
    const { container } = render(CollectionTable, {
      collection: collection(),
      details,
    });
    const link = rowEl(container, "a").querySelector('a[href="/decide"]');
    link.addEventListener("click", (event) => event.preventDefault());
    await fireEvent.click(link);
    expect(shown(container, "a")).toBeNull();
  });

  it("keeps the title a link when there is nothing to expand", () => {
    const { container } = render(CollectionTable, {
      collection: collection(),
    });
    expect(toggle(container, "a")).toBeNull();
    expect(
      rowEl(container, "a").querySelector("a[data-watch-title]").href,
    ).toContain("/initiatives/a");
    expect(rowEl(container, "a").dataset.expanded).toBeUndefined();
  });
});
