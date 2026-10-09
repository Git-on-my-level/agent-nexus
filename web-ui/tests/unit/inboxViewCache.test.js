// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { commitInboxView } from "../../src/lib/inboxViewCache.js";
import {
  clearWorkspaceViews,
  readWorkspaceView,
  writeWorkspaceView,
  workspaceViewRevision,
} from "../../src/lib/workspaceViewCache.js";

afterEach(clearWorkspaceViews);
const item = { id: "ask", related_refs: ["card:one"], status: "open" };
const snapshot = () =>
  [
    {
      value: {
        items: [
          { id: "decision", work_ref: "card:one", status: "awaiting_answer" },
        ],
      },
    },
    { value: { items: [{ id: "receipt", work_ref: "card:one" }] } },
    { value: { work: [{ ref: "card:one" }] } },
    { value: { items: [item] } },
    { value: { items: [] } },
    { value: { groups: [{ group_ref: "update" }] } },
  ].map((source) => ({ ...source, status: "fulfilled", complete: true }));

it("persists a confirmed answer and advances the revision that guards earlier reads", () => {
  writeWorkspaceView("reader:inbox", snapshot());
  writeWorkspaceView("other:inbox", snapshot());
  const revision = workspaceViewRevision();
  commitInboxView("reader", {
    answered: { id: item.id, status: "completed", responded_at: "now" },
  });
  expect(workspaceViewRevision()).toBeGreaterThan(revision);
  expect(readWorkspaceView("reader:inbox")[3].value.items).toEqual([]);
  expect(readWorkspaceView("reader:inbox")[4].value.items[0].status).toBe(
    "completed",
  );
  const stored = JSON.parse(
    localStorage.getItem("anx.workspace-views.v1"),
  ).entries.find(([key]) => key === "reader:inbox")[1].value;
  expect(stored[3].value.items).toEqual([]);
  expect(stored[4].value.items[0].responded_at).toBe("now");
  expect(stored[4].value.items[0].related_refs).toEqual(["card:one"]);
  expect(readWorkspaceView("other:inbox")[3].value.items).toEqual([item]);
});

it("persists approved and dismissed decisions and archives their work and receipts", () => {
  writeWorkspaceView("reader:inbox", snapshot());
  const decision = { id: "decision", work_ref: "card:one", status: "answered" };
  commitInboxView("reader", { decision });
  expect(readWorkspaceView("reader:inbox")[0].value.items).toEqual([decision]);
  commitInboxView("reader", { archivedRef: "card:one" });
  const stored = readWorkspaceView("reader:inbox");
  expect(stored[0].value.items).toEqual([]);
  expect(stored[1].value.items).toEqual([]);
  expect(stored[2].value.work).toEqual([]);
  expect(stored[5].value.groups).toEqual([{ group_ref: "update" }]);
});
