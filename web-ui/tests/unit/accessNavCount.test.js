// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

const coreClientMock = vi.hoisted(() => ({
  listPendingHostEnrollments: vi.fn(),
}));

vi.mock("$lib/coreClient", () => ({ coreClient: coreClientMock }));

const { pendingAccessCount, resetPendingAccessCount } =
  await import("../../src/lib/pendingAccessCount.js");
const AccessNavCount = (
  await import("../../src/lib/components/access/AccessNavCount.svelte")
).default;

/** Render with the store already settled; `enabled: false` starts no poll. */
function show(value, props = {}) {
  pendingAccessCount.set({ workspace: "main", count: null, ...value });
  return render(AccessNavCount, {
    props: { workspace: "main", enabled: false, ...props },
  });
}

afterEach(() => {
  cleanup();
  resetPendingAccessCount();
  vi.clearAllMocks();
});

describe("AccessNavCount", () => {
  it("renders nothing at zero", () => {
    show({ count: 0, forbidden: false });
    expect(document.querySelector("[data-access-nav-count]")).toBeNull();
  });

  it("renders nothing before the first read lands", () => {
    show({ count: null, forbidden: false });
    expect(document.querySelector("[data-access-nav-count]")).toBeNull();
  });

  it("renders nothing for a reader who may not decide access", () => {
    // A count can never be set alongside a refusal today; the badge must not
    // depend on that for the number to stay hidden.
    show({ count: 3, forbidden: true });
    expect(document.querySelector("[data-access-nav-count]")).toBeNull();
  });

  it("renders nothing for another workspace's number", () => {
    show({ workspace: "other", count: 4, forbidden: false });
    expect(document.querySelector("[data-access-nav-count]")).toBeNull();
  });

  it("shows the number with a label that says what it is", () => {
    show({ count: 1, forbidden: false });
    const badge = document.querySelector("[data-access-nav-count]");
    expect(badge.textContent.trim()).toBe("1");
    expect(badge.getAttribute("aria-label")).toBe("1 access request waiting");
    expect(badge.getAttribute("title")).toBe("1 access request waiting");
  });

  it("caps the number so the badge cannot widen the sidebar", () => {
    show({ count: 250, forbidden: false });
    const badge = document.querySelector("[data-access-nav-count]");
    expect(badge.textContent.trim()).toBe("99+");
    expect(badge.getAttribute("aria-label")).toBe(
      "250 access requests waiting",
    );
  });

  it("is decorative on the menu trigger, whose own label carries the number", () => {
    show({ count: 2, forbidden: false }, { variant: "trigger" });
    const badge = document.querySelector("[data-access-trigger-count]");
    expect(badge.textContent.trim()).toBe("2");
    // An explicit aria-label on the trigger button suppresses descendant
    // names, so announcing here would be announcing into a void.
    expect(badge.getAttribute("aria-hidden")).toBe("true");
    expect(badge.getAttribute("aria-label")).toBeNull();
    expect(document.querySelector("[data-access-nav-count]")).toBeNull();
  });

  it("starts no poll until it is enabled with a workspace", () => {
    show({ count: 0, forbidden: false }, { enabled: true, workspace: "" });
    expect(coreClientMock.listPendingHostEnrollments).not.toHaveBeenCalled();
  });
});
