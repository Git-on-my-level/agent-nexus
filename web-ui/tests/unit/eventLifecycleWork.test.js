// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { tick } from "svelte";

import EventLifecycleWork from "../../src/lib/components/EventLifecycleWork.svelte";
import { authenticatedAgent } from "../../src/lib/authSession.js";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
} from "../../src/lib/workspaceContext.js";
import * as localWorkStore from "../../src/lib/localWorkStore.js";
import * as mutationOutbox from "../../src/lib/mutationOutbox.js";

const event = {
  id: "parent",
  summary: "Message parent",
  archived_at: "2026-10-13T00:00:00Z",
  trashed_at: null,
};

beforeEach(() => {
  currentOrganizationSlug.set("org");
  currentWorkspaceSlug.set("workspace");
  authenticatedAgent.set({ agent_id: "human", actor_id: "actor" });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  authenticatedAgent.set(null);
  currentOrganizationSlug.set("");
  currentWorkspaceSlug.set("");
});

function delayLoad(value) {
  let release;
  const delayed = new Promise((resolve) => {
    release = () => resolve(value);
  });
  vi.spyOn(localWorkStore, "loadLocalWork").mockReturnValue(delayed);
  return release;
}

it("queues an unarchive that arrives before device restore finishes", async () => {
  const enqueue = vi
    .spyOn(mutationOutbox, "enqueueEventLifecycle")
    .mockResolvedValue();
  const release = delayLoad(null);
  const { component } = render(EventLifecycleWork, {
    props: {
      target: "event-lifecycle:thread:t1",
      href: "/threads/t1?tab=timeline",
    },
  });
  const queued = component.queue([event], "unarchive");
  await tick();
  expect(document.body.textContent).not.toContain("already saved");
  expect(enqueue).not.toHaveBeenCalled();
  release();
  await queued;
  expect(enqueue).toHaveBeenCalledOnce();
  expect(enqueue.mock.calls[0][0].draft.action).toBe("unarchive");
  expect(enqueue.mock.calls[0][0].draft.events[0].id).toBe("parent");
});

it("still refuses to replace a restored draft after waiting", async () => {
  const enqueue = vi
    .spyOn(mutationOutbox, "enqueueEventLifecycle")
    .mockResolvedValue();
  const release = delayLoad({
    action: "archive",
    reason: "",
    events: [
      {
        id: "other",
        title: "Other",
        if_lifecycle: { archived_at: null, trashed_at: null },
      },
    ],
  });
  const { component } = render(EventLifecycleWork, {
    props: {
      target: "event-lifecycle:thread:t1",
      href: "/threads/t1?tab=timeline",
    },
  });
  const queued = component.queue([event], "unarchive");
  release();
  await expect(queued).rejects.toThrow(
    "Send or discard the saved draft before replacing its events.",
  );
  expect(enqueue).not.toHaveBeenCalled();
});
