// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, expect, it, vi } from "vitest";
import { __setDev, __resetDev } from "$app/environment";
import ErrorPage from "../../src/routes/+error.svelte";

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
});

it("hides local setup instructions in production error pages", () => {
  __setDev(false);
  const { container } = render(ErrorPage, { data: {} });
  expect(container.textContent).toContain("Contact your administrator");
  expect(container.textContent).not.toContain("Technical troubleshooting");
  expect(container.textContent).not.toContain("make serve");
  expect(container.textContent).not.toContain("ANX_WORKSPACES");
});

it("keeps local troubleshooting available in development", () => {
  __setDev(true);
  const { container } = render(ErrorPage, { data: {} });
  expect(container.textContent).toContain("Technical troubleshooting");
  expect(container.textContent).toContain("ANX_WORKSPACES");
});
