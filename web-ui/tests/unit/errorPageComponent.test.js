// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, expect, it, vi } from "vitest";
import { __setDev, __resetDev } from "$app/environment";
import ErrorPage from "../../src/routes/+error.svelte";

const support = vi.hoisted(() => ({ enabled: true }));
vi.mock("$lib/extensions/supportLink.js", () => ({
  get technicalTroubleshootingEnabled() {
    return support.enabled;
  },
}));

vi.mock("$app/stores", () => ({
  page: {
    subscribe: (fn) => {
      fn({
        status: 520,
        error: { message: "Error: 520" },
        url: new URL("https://example.com/o/acme/w/new/overview"),
      });
      return () => {};
    },
  },
}));
afterEach(() => {
  cleanup();
  __resetDev();
  support.enabled = true;
});

it.each([false, true])(
  "hides troubleshooting when disabled, dev=%s, without layout data",
  (dev) => {
    support.enabled = false;
    __setDev(dev);
    const { container } = render(ErrorPage, { data: {} });
    expect(container.textContent).toContain("Contact your administrator");
    expect(container.textContent).not.toContain("Technical troubleshooting");
    if (!dev) expect(container.textContent).not.toContain("make serve");
    expect(container.textContent).not.toContain("ANX_WORKSPACES");
  },
);

it.each([false, true])(
  "keeps self-host troubleshooting enabled, dev=%s",
  (dev) => {
    __setDev(dev);
    const { container } = render(ErrorPage, { data: {} });
    expect(container.textContent).toContain("Technical troubleshooting");
    expect(container.textContent).toContain("ANX_WORKSPACES");
  },
);
