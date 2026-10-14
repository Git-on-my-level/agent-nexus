// @vitest-environment jsdom
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { writable } from "svelte/store";
const { scope, load, save } = vi.hoisted(() => ({
  scope: { current: null },
  load: vi.fn(),
  save: vi.fn(),
}));
vi.mock("$lib/localWorkScope.js", () => ({
  localWorkScope: { subscribe: (...args) => scope.current.subscribe(...args) },
  canUseLocalWorkScope: () => false,
}));
vi.mock("$lib/localWorkStore.js", () => ({
  loadLocalWork: load,
  saveLocalWork: save,
  localWorkEpoch: () => "epoch",
  localWorkPrincipal: (key) => {
    const [, , agent, actor] = JSON.parse(key);
    return agent || actor || "";
  },
}));
import { protocolDraft } from "$lib/actions/protocolDraft.js";
const anonymous = JSON.stringify(["org", "workspace", "", ""]);
let actions;
beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  vi.clearAllMocks();
  scope.current = writable(anonymous);
  load.mockResolvedValue(null);
  save.mockResolvedValue();
  actions = [];
});
afterEach(() => {
  actions.forEach((action) => action.destroy());
  vi.restoreAllMocks();
});
function form(initial) {
  let current = initial,
    control;
  const node = document.createElement("form");
  const options = {
    target: "identity:create",
    fields: ["name", "request", "rejected"],
    read: () => current,
    onRestore: (value) => {
      current = value || { name: "", request: null, rejected: false };
    },
    onControl: (value) => {
      control = value;
    },
  };
  const action = protocolDraft(node, options);
  actions.push(action);
  return {
    node,
    control,
    current: () => current,
    edit: (value) => {
      current = value;
      action.update(options);
    },
  };
}
it("persists only declared non-secret fields and restores without sending", async () => {
  const original = {
    name: "Reader",
    request: null,
    rejected: false,
    password: "never-save",
    token: "never-save",
  };
  const first = form(original);
  first.node.dispatchEvent(new Event("input"));
  expect(JSON.stringify(localStorage)).not.toContain("never-save");
  first.node.remove();
  actions.pop().destroy();
  const second = form({ name: "", request: null, rejected: false });
  expect(second.current().name).toBe("Reader");
  expect(save).not.toHaveBeenCalled();
});
it("a later draft checkpoint cannot erase an immutable original request journal", async () => {
  const first = form({
    name: "Reader",
    request: { request_key: "original", actor: { id: "reader" } },
    rejected: false,
  });
  const original = await first.control.persist();
  const journal = Object.keys(localStorage).find((key) =>
    key.endsWith(":request:original"),
  );
  first.edit({ name: "Other draft", request: null, rejected: false });
  expect(JSON.parse(localStorage.getItem(journal))).toEqual(original);
  await first.control.clear(original);
  expect(localStorage.getItem(journal)).not.toBeNull();
});
it("forbids a changed body under an existing submitted key", async () => {
  const first = form({
    name: "Reader",
    request: { request_key: "original", actor: { id: "reader" } },
    rejected: false,
  });
  await first.control.persist();
  first.edit({
    name: "Different",
    request: { request_key: "original", actor: { id: "different" } },
    rejected: false,
  });
  await expect(first.control.persist()).rejects.toThrow(
    "Saved request changed",
  );
});
it("purges public drafts and submissions when a verified identity arrives", async () => {
  const first = form({
    name: "Reader",
    request: { request_key: "original", actor: { id: "reader" } },
    rejected: false,
  });
  await first.control.persist();
  scope.current.set(JSON.stringify(["org", "workspace", "verified", "reader"]));
  await Promise.resolve();
  expect(
    Object.keys(localStorage).filter((key) => key.includes("identity:create")),
  ).toEqual([]);
  expect(first.current().request).toBeNull();
});
it("refuses submission if durable storage fails", async () => {
  const first = form({ name: "Reader", request: null, rejected: false });
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("denied");
  });
  await expect(first.control.persist()).rejects.toThrow("denied");
  expect(first.current().name).toBe("Reader");
});

it("refuses a credential hidden inside an otherwise declared request", async () => {
  const first = form({
    name: "Reader",
    request: { request_key: "key", credential: { private_key: "never-save" } },
    rejected: false,
  });
  await expect(first.control.persist()).rejects.toThrow(
    "Only non-secret fields",
  );
  expect(JSON.stringify(localStorage)).not.toContain("never-save");
});

it("refuses to checkpoint a form after it is disposed", async () => {
  const original = { name: "Closed form", request: null, rejected: false };
  const closed = form(original);
  actions.pop().destroy();
  const before = JSON.stringify(localStorage);
  await expect(closed.control.persist()).rejects.toThrow("Identity changed");
  expect(JSON.stringify(localStorage)).toBe(before);
});
