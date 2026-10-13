// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  clearPrincipalLocalWork,
  listLocalWork,
  loadLocalWork,
  localWorkEpoch,
  saveLocalWork,
} from "../../src/lib/localWorkStore.js";
import { mutationFailure } from "../../src/lib/mutationStatus.js";

const scope = JSON.stringify(["org", "workspace", "human", "actor"]);
beforeEach(async () => {
  await listLocalWork(scope);
  localStorage.clear();
});
afterEach(async () => {
  vi.restoreAllMocks();
  await clearPrincipalLocalWork(scope);
});

it("checkpoints before yielding, even when IndexedDB is unavailable", async () => {
  const saved = saveLocalWork(
    scope,
    "draft",
    "inbox:one:reply",
    "Keep this text",
  );
  expect(
    [...Array(localStorage.length)].some((_, i) =>
      localStorage.getItem(localStorage.key(i)).includes("Keep this text"),
    ),
  ).toBe(true);
  await saved;
  expect(await loadLocalWork(scope, "draft", "inbox:one:reply")).toBe(
    "Keep this text",
  );
});

it("namespaces work by organization, workspace, principal and target", async () => {
  await saveLocalWork(scope, "draft", "inbox:one:reply", "Private answer");
  for (const other of [
    ["other-org", "workspace", "human", "actor"],
    ["org", "other-workspace", "human", "actor"],
    ["org", "workspace", "other-human", "other-actor"],
  ])
    expect(
      await loadLocalWork(JSON.stringify(other), "draft", "inbox:one:reply"),
    ).toBeNull();
  expect(await loadLocalWork(scope, "draft", "inbox:two:reply")).toBeNull();
});

it("keeps independent records and does not resurrect a confirmed deletion", async () => {
  await Promise.all([
    saveLocalWork(scope, "inbox-send", "key-one", { text: "First" }),
    saveLocalWork(scope, "inbox-send", "key-two", { text: "Second" }),
  ]);
  await saveLocalWork(scope, "inbox-send", "key-one", null);
  expect(
    (await listLocalWork(scope, "inbox-send")).map((record) => record.target),
  ).toEqual(["key-two"]);
});

it("purges the principal across workspaces and fences late saves", async () => {
  const otherWorkspace = JSON.stringify(["org", "other", "human", "actor"]);
  const otherPrincipal = JSON.stringify(["org", "workspace", "other", "other"]);
  const epoch = localWorkEpoch(scope);
  localStorage.setItem(
    "anx.human-response.draft:workspace:item",
    "Legacy private text",
  );
  const earlierSave = saveLocalWork(scope, "draft", "reply", "Secret");
  await saveLocalWork(otherWorkspace, "draft", "reply", "Also secret");
  await saveLocalWork(
    otherPrincipal,
    "draft",
    "reply",
    "Another person's draft",
  );
  await clearPrincipalLocalWork(scope);
  expect(
    localStorage.getItem("anx.human-response.draft:workspace:item"),
  ).toBeNull();
  await earlierSave.catch(() => {});
  expect(await listLocalWork(scope)).toEqual([]);
  expect(await listLocalWork(otherWorkspace)).toEqual([]);
  expect(await loadLocalWork(otherPrincipal, "draft", "reply")).toBe(
    "Another person's draft",
  );
  expect(() =>
    saveLocalWork(scope, "draft", "reply", "Late secret", { epoch }),
  ).toThrow("signed out");
});

it("reports total storage failure instead of claiming work was saved", async () => {
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("quota");
  });
  await expect(
    saveLocalWork(scope, "draft", "reply", "Unsaved text"),
  ).rejects.toMatchObject({ localPersistence: true });
  const leaving = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(leaving);
  expect(leaving.defaultPrevented).toBe(true);
});

it("rejects anonymous namespaces and recovers other records despite corruption", async () => {
  expect(() =>
    saveLocalWork('["org","workspace","",""]', "draft", "reply", "text"),
  ).toThrow("identity");
  localStorage.setItem("anx:local-work:v1:corrupt", "broken JSON");
  await saveLocalWork(scope, "draft", "reply", "Recoverable");
  expect(await loadLocalWork(scope, "draft", "reply")).toBe("Recoverable");
});

it.each([0, 408, 429, 500, 524])(
  "retries uncertain status %s without exposing upstream bodies",
  (status) => {
    const error = Object.assign(new Error("<html>Cloudflare secret</html>"), {
      status,
    });
    const failure = mutationFailure(error);
    expect(failure.state).toBe("retrying");
    expect(failure.message).not.toContain("Cloudflare");
    expect(failure.detail).toContain("Cloudflare");
  },
);
it.each([400, 401, 403, 404, 409, 422])(
  "retains definitive status %s for attention",
  (status) => {
    expect(mutationFailure({ status, message: "server detail" })).toMatchObject(
      { state: "attention" },
    );
  },
);

it("preserves new-session work when the same cross-tab purge is delivered again", async () => {
  await saveLocalWork(scope, "draft", "reply", "Before sign-out");
  const epoch = crypto.randomUUID();
  await clearPrincipalLocalWork(scope, { epoch });
  await saveLocalWork(scope, "draft", "reply", "After sign-in");
  await clearPrincipalLocalWork(scope, { epoch, broadcast: false });
  expect(await loadLocalWork(scope, "draft", "reply")).toBe("After sign-in");
});
