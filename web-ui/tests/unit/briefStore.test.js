// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { tick } from "svelte";
import { afterEach, expect, it, vi } from "vitest";

import DecideNavCount from "../../src/lib/components/decide/DecideNavCount.svelte";
import { selectedActorId } from "../../src/lib/actorSession.js";
import { readerScopeKey } from "../../src/lib/readerScope.js";
import { coreClient } from "$lib/coreClient";
import { refreshBrief } from "../../src/lib/briefStore.js";
import {
  setCurrentOrganizationSlug,
  setCurrentWorkspaceSlug,
} from "../../src/lib/workspaceContext.js";

const harness = vi.hoisted(() => ({
  scope: () => "",
  inbox: [],
  events: [],
}));

vi.mock("$lib/liveWorkspaceEvents.js", () => ({
  liveInboxChanges: vi.fn(({ onChange }) => {
    const record = { scope: harness.scope(), stopped: false, onChange };
    harness.inbox.push(record);
    return () => {
      record.stopped = true;
    };
  }),
  liveWorkspaceEvents: vi.fn(({ onChange }) => {
    const record = { onChange };
    harness.events.push(record);
    return () => {};
  }),
}));

vi.mock("$lib/coreClient", () => ({
  coreClient: {
    getBrief: vi.fn(async () => ({ schema: "brief.v1" })),
  },
  captureInboxResponseSender: vi.fn(),
}));

harness.scope = readerScopeKey;

afterEach(() => {
  cleanup();
  harness.inbox.length = 0;
  harness.events.length = 0;
  vi.useRealTimers();
  vi.clearAllMocks();
  selectedActorId.set("");
  setCurrentOrganizationSlug("");
  setCurrentWorkspaceSlug("");
});

it("attaches the live brief subscription after a reader switch while the nav badge stays mounted", async () => {
  setCurrentOrganizationSlug("acme");
  setCurrentWorkspaceSlug("ops");
  selectedActorId.set("ada");
  const first = readerScopeKey();

  render(DecideNavCount, { workspace: "ops", enabled: true });
  await tick();

  expect(
    harness.inbox
      .filter((entry) => !entry.stopped)
      .map(({ scope, stopped }) => ({ scope, stopped })),
  ).toEqual([{ scope: first, stopped: false }]);

  selectedActorId.set("grace");
  const next = readerScopeKey();
  await tick();
  await Promise.resolve();

  expect(
    harness.inbox
      .filter((entry) => !entry.stopped)
      .map(({ scope, stopped }) => ({ scope, stopped })),
  ).toEqual([{ scope: next, stopped: false }]);
  expect(
    harness.inbox.some((entry) => entry.scope === first && entry.stopped),
  ).toBe(true);
});

it("bounds receipt-driven brief reads and coalesces inbox/event invalidations with a trailing read", async () => {
  vi.useFakeTimers();
  setCurrentOrganizationSlug("load");
  setCurrentWorkspaceSlug("brief");
  selectedActorId.set("load-reader");
  render(DecideNavCount, { workspace: "brief", enabled: true });
  await tick();
  await vi.advanceTimersByTimeAsync(0);
  coreClient.getBrief.mockClear();
  const event = harness.events.at(-1);
  const inbox = harness.inbox.at(-1);
  for (let i = 0; i < 60; i++) {
    event.onChange([{ type: "run_recorded" }]);
    inbox.onChange([]);
    await vi.advanceTimersByTimeAsync(1000);
  }
  expect(coreClient.getBrief).toHaveBeenCalledTimes(12);
  const before = coreClient.getBrief.mock.calls.length;
  event.onChange([{ type: "card_updated" }]);
  await vi.advanceTimersByTimeAsync(5000);
  expect(coreClient.getBrief.mock.calls.length).toBeGreaterThan(before);
  event.onChange([{ type: "card_updated" }]);
  cleanup();
  const stopped = coreClient.getBrief.mock.calls.length;
  await vi.advanceTimersByTimeAsync(10_000);
  expect(coreClient.getBrief.mock.calls.length).toBe(stopped);
});

it("refreshes confirmed actions immediately and follows invalidations during a slow read", async () => {
  vi.useFakeTimers();
  setCurrentOrganizationSlug("slow");
  setCurrentWorkspaceSlug("brief");
  selectedActorId.set("slow-reader");
  render(DecideNavCount, { workspace: "brief", enabled: true });
  await tick();
  await vi.advanceTimersByTimeAsync(0);
  coreClient.getBrief.mockClear();
  await refreshBrief();
  expect(coreClient.getBrief).toHaveBeenCalledTimes(1);
  let finish;
  coreClient.getBrief.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const event = harness.events.at(-1);
  event.onChange([{ type: "run_recorded" }]);
  await vi.advanceTimersByTimeAsync(5000);
  expect(coreClient.getBrief).toHaveBeenCalledTimes(2);
  event.onChange([{ type: "run_recorded" }]);
  await vi.advanceTimersByTimeAsync(10_000);
  expect(coreClient.getBrief).toHaveBeenCalledTimes(2);
  finish({ schema: "brief.v1" });
  await vi.advanceTimersByTimeAsync(1);
  expect(coreClient.getBrief).toHaveBeenCalledTimes(3);
});

it("refreshes a pending invalidation on return from a hidden tab", async () => {
  vi.useFakeTimers();
  setCurrentOrganizationSlug("hidden");
  setCurrentWorkspaceSlug("brief");
  selectedActorId.set("hidden-reader");
  render(DecideNavCount, { workspace: "brief", enabled: true });
  await tick();
  await vi.advanceTimersByTimeAsync(0);
  coreClient.getBrief.mockClear();
  harness.events.at(-1).onChange([{ type: "card_updated" }]);
  const visibility = vi.spyOn(document, "visibilityState", "get");
  visibility.mockReturnValue("hidden");
  document.dispatchEvent(new Event("visibilitychange"));
  await vi.advanceTimersByTimeAsync(1000);
  expect(coreClient.getBrief).not.toHaveBeenCalled();
  visibility.mockReturnValue("visible");
  document.dispatchEvent(new Event("visibilitychange"));
  await vi.advanceTimersByTimeAsync(0);
  expect(coreClient.getBrief).toHaveBeenCalledTimes(1);
  visibility.mockRestore();
});
