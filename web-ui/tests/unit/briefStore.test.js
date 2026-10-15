// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { tick } from "svelte";
import { afterEach, expect, it, vi } from "vitest";

import DecideNavCount from "../../src/lib/components/decide/DecideNavCount.svelte";
import { selectedActorId } from "../../src/lib/actorSession.js";
import { readerScopeKey } from "../../src/lib/readerScope.js";
import {
  setCurrentOrganizationSlug,
  setCurrentWorkspaceSlug,
} from "../../src/lib/workspaceContext.js";

const harness = vi.hoisted(() => ({
  scope: () => "",
  inbox: [],
}));

vi.mock("$lib/liveWorkspaceEvents.js", () => ({
  liveInboxChanges: vi.fn(() => {
    const record = { scope: harness.scope(), stopped: false };
    harness.inbox.push(record);
    return () => {
      record.stopped = true;
    };
  }),
  liveWorkspaceEvents: vi.fn(() => () => {}),
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

  expect(harness.inbox.filter((entry) => !entry.stopped)).toEqual([
    { scope: first, stopped: false },
  ]);

  selectedActorId.set("grace");
  const next = readerScopeKey();
  await tick();
  await Promise.resolve();

  expect(harness.inbox.filter((entry) => !entry.stopped)).toEqual([
    { scope: next, stopped: false },
  ]);
  expect(
    harness.inbox.some((entry) => entry.scope === first && entry.stopped),
  ).toBe(true);
});
