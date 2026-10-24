// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import CollectionTable from "../../src/lib/components/collection/CollectionTable.svelte";
import { normalizeCollection } from "../../src/lib/collection.js";

afterEach(cleanup);

const STATES = [
  { key: "failing", label: "Failing", tone: "bad" },
  { key: "healthy", label: "Healthy", tone: "good" },
];
const collection = (rows) =>
  normalizeCollection({
    states: STATES,
    groups: [{ key: "all", rows }],
  });
const row = (id, state) => ({ ref: `card:${id}`, title: id, state });

describe("CollectionTable state filter", () => {
  it("drops a picked state that a refresh emptied, so rows stay visible", async () => {
    const { container, rerender } = render(CollectionTable, {
      collection: collection([row("a", "failing"), row("b", "healthy")]),
    });
    await fireEvent.click(
      container.querySelector('[data-collection-filter="failing"]'),
    );
    expect(container.querySelectorAll("[data-collection-row]")).toHaveLength(1);

    // Every loop recovered: no Failing rows, so no Failing pill either.
    await rerender({
      collection: collection([row("a", "healthy"), row("b", "healthy")]),
    });
    expect(container.querySelectorAll("[data-collection-row]")).toHaveLength(2);
  });
  it("keeps a picked state that still has rows", async () => {
    const { container, rerender } = render(CollectionTable, {
      collection: collection([row("a", "failing"), row("b", "healthy")]),
    });
    await fireEvent.click(
      container.querySelector('[data-collection-filter="failing"]'),
    );
    await rerender({
      collection: collection([
        row("a", "failing"),
        row("b", "healthy"),
        row("c", "healthy"),
      ]),
    });
    expect(
      [...container.querySelectorAll("[data-collection-row]")].map((el) =>
        el.getAttribute("data-collection-row"),
      ),
    ).toEqual(["card:a"]);
  });
});
