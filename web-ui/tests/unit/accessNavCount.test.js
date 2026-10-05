// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

const coreClientMock = vi.hoisted(() => ({
  listAccessRequests: vi.fn(),
  listPendingHostEnrollments: vi.fn(),
}));

vi.mock("$lib/coreClient", () => ({ coreClient: coreClientMock }));

const { get } = await import("svelte/store");
const { pendingAccessCount, resetPendingAccessCount } =
  await import("../../src/lib/pendingAccessCount.js");
const AccessNavCount = (
  await import("../../src/lib/components/access/AccessNavCount.svelte")
).default;

/**
 * Render with the store already settled.
 *
 * The poll is left in flight (both reads never resolve) so it cannot
 * overwrite the number under test; `enabled: false` starts no poll at all.
 */
function show(value, props = {}) {
  coreClientMock.listAccessRequests.mockReturnValue(new Promise(() => {}));
  coreClientMock.listPendingHostEnrollments.mockReturnValue(
    new Promise(() => {}),
  );
  // Seed before mounting: `startPendingAccessCount` only resets the store
  // when it is pointed at a different workspace.
  pendingAccessCount.set({ workspace: "main", count: null, ...value });
  return render(AccessNavCount, {
    props: { workspace: "main", enabled: true, ...props },
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
    expect(coreClientMock.listAccessRequests).not.toHaveBeenCalled();
  });

  it("renders nothing while disabled, whatever the store still holds", () => {
    // Disabling means this reader may not decide access. The cached number
    // came from an inventory they may not see, and nothing will refresh it.
    pendingAccessCount.set({ workspace: "main", count: 7, forbidden: false });
    render(AccessNavCount, { props: { workspace: "main", enabled: false } });
    expect(document.querySelector("[data-access-nav-count]")).toBeNull();
  });

  it("clears the cached number when it is disabled", () => {
    // Otherwise a count read by the previous human principal stays in the
    // store and the account-menu label keeps reading it.
    pendingAccessCount.set({ workspace: "main", count: 7, forbidden: false });
    render(AccessNavCount, { props: { workspace: "main", enabled: false } });
    expect(get(pendingAccessCount)).toEqual({
      workspace: "",
      count: null,
      forbidden: false,
    });
  });

  it("clears the cached number when the workspace goes away", () => {
    pendingAccessCount.set({ workspace: "main", count: 7, forbidden: false });
    render(AccessNavCount, { props: { workspace: "", enabled: true } });
    expect(get(pendingAccessCount).count).toBeNull();
    expect(document.querySelector("[data-access-nav-count]")).toBeNull();
  });

  it("shows nothing on the trigger while disabled either", () => {
    show(
      { count: 7, forbidden: false },
      { enabled: false, variant: "trigger" },
    );
    expect(document.querySelector("[data-access-trigger-count]")).toBeNull();
  });
});
