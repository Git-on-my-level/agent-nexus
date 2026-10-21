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

const DEFAULT_PAGE = {
  status: 520,
  error: { message: "Error: 520" },
  url: new URL("https://example.com/o/acme/w/new/overview"),
};
const pageState = vi.hoisted(() => ({ current: null }));
vi.mock("$app/stores", () => ({
  page: {
    subscribe: (fn) => {
      fn(pageState.current ?? DEFAULT_PAGE);
      return () => {};
    },
  },
}));
afterEach(() => {
  cleanup();
  __resetDev();
  support.enabled = true;
  pageState.current = null;
});

it.each([false, true])(
  "hides troubleshooting when disabled, dev=%s, without layout data",
  (dev) => {
    support.enabled = false;
    __setDev(dev);
    const { container, getByRole } = render(ErrorPage, { data: {} });
    expect(getByRole("button", { name: "Try again" })).toBeTruthy();
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

const JARGON = /ANX_WORKSPACES|control plane/i;

it.each([false, true])(
  "tells a viewer without access to switch account, troubleshooting=%s",
  (troubleshooting) => {
    support.enabled = troubleshooting;
    pageState.current = {
      status: 404,
      error: {
        code: "workspace_not_accessible",
        message:
          "You don't have access to this workspace, or it doesn't exist.",
        signInUrl: "/signin?return_path=%2Fr%2Fcard%3Aabc",
      },
      url: new URL("https://example.com/o/acme/w/omi/r/card:abc"),
    };
    const { container, getByRole } = render(ErrorPage, {
      data: { shellCapabilities: { publicOrigin: "https://example.com" } },
    });
    expect(getByRole("heading").textContent).toBe(
      "You don't have access to this workspace, or it doesn't exist",
    );
    expect(
      getByRole("button", { name: "Switch account" }).getAttribute("href"),
    ).toBe("/signin?return_path=%2Fr%2Fcard%3Aabc");
    expect(getByRole("button", { name: "Return to dashboard" })).toBeTruthy();
    expect(container.textContent).not.toMatch(JARGON);
    expect(container.textContent).not.toContain("Workspace not found");
  },
);

it.each([
  "/\\evil.example/signin",
  "\\\\evil.example/signin",
  "/%5Cevil.example/signin",
  "//evil.example/signin",
  "https://evil.example/signin",
  "javascript:alert(1)",
])("never follows an off-origin sign-in link %j", (signInUrl) => {
  pageState.current = {
    status: 404,
    error: { code: "workspace_not_accessible", message: "x", signInUrl },
    url: new URL("https://example.com/o/acme/w/omi"),
  };
  const { queryByRole } = render(ErrorPage, { data: {} });
  expect(queryByRole("button", { name: "Switch account" })).toBeNull();
});

it("offers a retry when the workspace lookup could not be answered", () => {
  pageState.current = {
    status: 503,
    error: {
      code: "workspace_lookup_unavailable",
      message: "We couldn't look up this workspace right now.",
    },
    url: new URL("https://example.com/o/acme/w/omi"),
  };
  const { container, getByRole } = render(ErrorPage, { data: {} });
  expect(getByRole("button", { name: "Try again" })).toBeTruthy();
  expect(container.textContent).not.toMatch(JARGON);
});
