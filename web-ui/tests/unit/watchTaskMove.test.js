// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it } from "vitest";
import { get } from "svelte/store";
import { authenticatedAgent } from "../../src/lib/authSession.js";
import { localWorkScope } from "../../src/lib/localWorkScope.js";
import { clearPrincipalLocalWork } from "../../src/lib/localWorkStore.js";
import { mutationOutbox } from "../../src/lib/mutationOutbox.js";
import {
  enqueueTaskMove,
  taskMoveDraft,
  taskMoveError,
  taskMoveRequest,
} from "../../src/lib/taskMoveWork.js";
import { workKey } from "../../src/lib/pm/presentation.js";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
} from "../../src/lib/workspaceContext.js";

const boardToken = "2026-10-15T10:00:00.123456789Z";

// publicWork shape for a Nexus backlog card: no internal id, summary=1 object,
// and version 0 when work_metadata has not been written yet.
function watchWork({ version = 0, authority = "nexus" } = {}) {
  return {
    ref: "card:ship-release",
    handle: "ship-release",
    title: "Ship the release",
    summary: { state: "active", placement: "backlog", promise: "" },
    column_key: "backlog",
    phase: "backlog",
    board_ref: "board:portfolio",
    source: { authority },
    version,
    decision_revision: `${version}.1`,
    head_revision_number: 1,
  };
}

function watchDraft(work, placement, beforeRef = "") {
  return taskMoveDraft(work, placement, {
    key: workKey(work),
    ref: "",
    boardToken,
    beforeCardId: beforeRef,
    approved: true,
  });
}

beforeEach(() => {
  localStorage.clear();
  mutationOutbox.set([]);
  currentOrganizationSlug.set("acme");
  currentWorkspaceSlug.set("personal");
  authenticatedAgent.set({ agent_id: "agent-1", actor_id: "actor-1" });
  Object.defineProperty(navigator, "onLine", {
    configurable: true,
    get: () => false,
  });
});

afterEach(async () => {
  await clearPrincipalLocalWork(get(localWorkScope));
  mutationOutbox.set([]);
  authenticatedAgent.set(null);
  currentOrganizationSlug.set("");
  currentWorkspaceSlug.set("");
});

it("enqueues a version-0 backlog card onto ready, in progress, and review", async () => {
  const work = watchWork({ version: 0 });
  for (const placement of ["ready", "in_progress", "review"]) {
    const draft = watchDraft(work, placement);
    const entry = await enqueueTaskMove(draft, {
      draftTarget: "tasks:move",
      href: "/o/acme/w/personal/watch?view=board",
    });
    expect(entry.operation).toBe("cards.move");
    expect(entry.target).toBe("task:card:ship-release:move");
    expect(entry.draftTarget).toBe("tasks:move");
    expect(entry.request.id).toBe("card:ship-release");
    expect(entry.request.body).toMatchObject({
      actor_id: "actor-1",
      column_key: placement,
      if_board_updated_at: boardToken,
      if_version: 0,
      before_card_id: "",
    });
    expect(entry.request.body).not.toHaveProperty("resolution");
  }
});

it("keeps a version-1 work.create card and a drag anchor on the same move", async () => {
  const work = watchWork({ version: 1 });
  const entry = await enqueueTaskMove(watchDraft(work, "ready", "card:peer"), {
    draftTarget: "tasks:move",
  });
  expect(entry.request.body).toMatchObject({
    column_key: "ready",
    if_version: 1,
    before_card_id: "card:peer",
  });
});

it("asks the PM for a card Nexus does not own", async () => {
  const work = watchWork({ authority: "github", version: 4 });
  work.source.revision = "abc";
  work.decision_revision = "abc";
  const entry = await enqueueTaskMove(watchDraft(work, "in_progress"), {
    draftTarget: "tasks:move",
  });
  expect(entry.operation).toBe("pm.decisions.create");
  expect(entry.request.body).toMatchObject({
    actor_id: "actor-1",
    payload: { phase: "in_progress" },
  });
  expect(entry.request.body).not.toHaveProperty("column_key");
});

it("still requires evidence before done and reports that reason", () => {
  const draft = watchDraft(watchWork({ version: 1 }), "done");
  expect(() => taskMoveRequest(draft, "actor-1", "key")).toThrow(/evidence/);
  let reason = "";
  try {
    taskMoveRequest(draft, "actor-1", "key");
  } catch (error) {
    reason = taskMoveError(error);
  }
  expect(reason).toBe(
    "Couldn't save that move. Add evidence before marking this initiative done.",
  );
  expect(taskMoveError("")).toBe("Couldn't save that move. Try again.");
});
