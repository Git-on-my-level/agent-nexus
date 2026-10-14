// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { tick } from "svelte";
import InboxControlWork from "../../src/lib/components/inbox/InboxControlWork.svelte";
import { authenticatedAgent } from "../../src/lib/authSession.js";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
} from "../../src/lib/workspaceContext.js";
import * as store from "../../src/lib/localWorkStore.js";
import * as outbox from "../../src/lib/mutationOutbox.js";
const item = {
  id: "action",
  action: "acknowledge",
  title: "Handle failed delivery",
  body: { if_revision: 2 },
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
function delay(value) {
  let release;
  const promise = new Promise((resolve) => (release = () => resolve(value)));
  vi.spyOn(store, "loadLocalWork").mockReturnValue(promise);
  return release;
}
it("captures the clicked base before waiting for restore", async () => {
  const enqueue = vi.spyOn(outbox, "enqueueInboxControls").mockResolvedValue();
  const release = delay(null);
  const { component } = render(InboxControlWork, { props: { href: "/inbox" } });
  const selected = structuredClone(item);
  const queued = component.queue([selected]);
  selected.body.if_revision = 3;
  await tick();
  expect(enqueue).not.toHaveBeenCalled();
  release();
  await queued;
  expect(enqueue.mock.calls[0][0].draft.items[0].body.if_revision).toBe(2);
});
it("retains a restored control instead of replacing it with a new click", async () => {
  const enqueue = vi.spyOn(outbox, "enqueueInboxControls").mockResolvedValue();
  const release = delay({ items: [item] });
  const { component } = render(InboxControlWork, { props: { href: "/inbox" } });
  const queued = component.queue([item]);
  const rejected = expect(queued).rejects.toThrow("saved inbox request");
  release();
  await rejected;
  expect(enqueue).not.toHaveBeenCalled();
});
it.each(["unmount", "principal change"])(
  "fences a restore wait after %s",
  async (kind) => {
    const enqueue = vi
      .spyOn(outbox, "enqueueInboxControls")
      .mockResolvedValue();
    const release = delay(null);
    const view = render(InboxControlWork, { props: { href: "/inbox" } });
    const queued = view.component.queue([item]);
    const rejected = expect(queued).rejects.toThrow(
      "Select the inbox item again",
    );
    if (kind === "unmount") view.unmount();
    else authenticatedAgent.set({ agent_id: "other", actor_id: "other" });
    release();
    await rejected;
    expect(enqueue).not.toHaveBeenCalled();
  },
);
