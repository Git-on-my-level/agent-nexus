// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({ before: null }));
vi.mock("$app/stores", async () => {
  const { writable } = await import("svelte/store");
  return { updated: writable(false) };
});
vi.mock("$app/navigation", () => ({
  beforeNavigate: (callback) => (h.before = callback),
}));

import UpdateAvailable from "../../src/lib/components/UpdateAvailable.svelte";
import { updated } from "$app/stores";

afterEach(() => {
  cleanup();
  updated.set(false);
});

it("stays quiet while this tab runs the deployed build", () => {
  render(UpdateAvailable);
  expect(document.querySelector("[data-update-available]")).toBeNull();
});

it("offers a reload and makes the next navigation a full page load once a newer build is out", () => {
  updated.set(true);
  render(UpdateAvailable);
  expect(document.querySelector("[data-update-available]")).toBeTruthy();
  const original = globalThis.location;
  const fake = { href: "http://app/o/a/w/b/decide" };
  Object.defineProperty(globalThis, "location", {
    value: fake,
    configurable: true,
  });
  try {
    h.before({ willUnload: false, to: { url: new URL("http://app/watch") } });
    expect(fake.href).toBe("http://app/watch");
  } finally {
    Object.defineProperty(globalThis, "location", {
      value: original,
      configurable: true,
    });
  }
});
