// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import { afterEach, expect, it, vi } from "vitest";
import { writable } from "svelte/store";
import {
  clearPrincipalLocalWork,
  loadLocalWork,
  saveLocalWork,
} from "../../src/lib/localWorkStore.js";
import { localWorkScope } from "$lib/localWorkScope.js";

vi.mock("$lib/localWorkScope.js", () => ({
  localWorkScope: writable(
    JSON.stringify(["org", "workspace", "human", "actor"]),
  ),
  canUseLocalWorkScope: () => true,
}));
vi.mock("$lib/hostBridgeStatus.js", async (original) => ({
  ...(await original()),
  observeHostBridge: () => () => {},
}));
import HostCard from "../../src/lib/components/access/HostCard.svelte";
const scope = JSON.stringify(["org", "workspace", "human", "actor"]);
const host = { id: "host-one", slug: "workstation", excluded_names: [] };
const target = "host:host-one:exclusion";
afterEach(async () => {
  cleanup();
  localWorkScope.set(scope);
  vi.restoreAllMocks();
  // Single-fork suites retain other localWorkStore module instances and their
  // Node BroadcastChannels. Teardown is local, not a simulated cross-tab logout.
  await clearPrincipalLocalWork(scope, { broadcast: false });
});

it("does not erase a newer draft written by another tab", async () => {
  let finish;
  const send = vi.fn(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await open({ onexclusions: send });
  await type("cursor");
  await fireEvent.submit(
    screen.getByLabelText("Name to exclude on workstation").closest("form"),
  );
  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  await saveLocalWork(scope, "draft", target, { name: "codex" });
  finish();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Exclude" }).disabled).toBe(
      false,
    ),
  );
  expect(await loadLocalWork(scope, "draft", target)).toEqual({
    name: "codex",
  });
});

it("refuses sending when device storage is unavailable", async () => {
  const send = vi.fn();
  await open({ onexclusions: send });
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("Device full");
  });
  await type("cursor");
  await fireEvent.submit(
    screen.getByLabelText("Name to exclude on workstation").closest("form"),
  );
  await waitFor(() =>
    expect(screen.getAllByRole("alert").length).toBeGreaterThan(0),
  );
  expect(send).not.toHaveBeenCalled();
  expect(screen.getByLabelText("Name to exclude on workstation").value).toBe(
    "cursor",
  );
});

it("identity replacement clears visible text and fences a late confirmation", async () => {
  let finish;
  const send = vi.fn(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await open({ onexclusions: send });
  await type("cursor");
  await fireEvent.submit(
    screen.getByLabelText("Name to exclude on workstation").closest("form"),
  );
  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  localWorkScope.set(
    JSON.stringify(["org", "workspace", "replacement", "replacement"]),
  );
  await waitFor(() =>
    expect(
      screen.queryByLabelText("Name to exclude on workstation"),
    ).toBeNull(),
  );
  finish();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Edit" }).disabled).toBe(false),
  );
  expect(await loadLocalWork(scope, "draft", target)).toEqual({
    name: "cursor",
  });
});
async function open(props = {}) {
  const view = render(HostCard, { host, canManage: true, ...props });
  const edit = await screen.findByRole("button", { name: "Edit" });
  await waitFor(() => expect(edit.disabled).toBe(false));
  await fireEvent.click(edit);
  return view;
}
async function type(value) {
  await fireEvent.input(
    await screen.findByLabelText("Name to exclude on workstation"),
    { target: { value } },
  );
}
it("keeps typed exclusions after Done and remount, without sending", async () => {
  const send = vi.fn();
  const view = await open({ onexclusions: send });
  await type("cursor");
  await fireEvent.click(screen.getByRole("button", { name: "Done" }));
  view.unmount();
  render(HostCard, { host, canManage: true });
  await waitFor(() =>
    expect(screen.getByLabelText("Name to exclude on workstation").value).toBe(
      "cursor",
    ),
  );
  expect(send).not.toHaveBeenCalled();
  expect(await loadLocalWork(scope, "draft", target)).toEqual({
    name: "cursor",
  });
});

it("removing an existing exclusion preserves the unrelated typed name", async () => {
  const send = vi.fn();
  await open({
    host: { ...host, excluded_names: ["old"] },
    onexclusions: send,
  });
  await type("cursor");
  await fireEvent.click(
    screen.getByRole("button", { name: "Allow old on workstation again" }),
  );
  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Exclude" }).disabled).toBe(
      false,
    ),
  );
  expect(screen.getByLabelText("Name to exclude on workstation").value).toBe(
    "cursor",
  );
  expect(await loadLocalWork(scope, "draft", target)).toEqual({
    name: "cursor",
  });
});
it("persists before sending and retains the draft on an uncertain failure", async () => {
  let checkpoint;
  const send = vi.fn(async () => {
    checkpoint = await loadLocalWork(scope, "draft", target);
    throw new Error("Lost acknowledgement");
  });
  await open({ onexclusions: send });
  await type("cursor");
  await fireEvent.submit(
    screen.getByLabelText("Name to exclude on workstation").closest("form"),
  );
  await screen.findByRole("alert");
  expect(send).toHaveBeenCalledTimes(1);
  expect(checkpoint).toEqual({ name: "cursor" });
  expect(screen.getByLabelText("Name to exclude on workstation").value).toBe(
    "cursor",
  );
});
it("confirmation preserves newer typing", async () => {
  let finish;
  const send = vi.fn(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await open({ onexclusions: send });
  await type("cursor");
  await fireEvent.submit(
    screen.getByLabelText("Name to exclude on workstation").closest("form"),
  );
  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  await type("codex");
  finish();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Exclude" }).disabled).toBe(
      false,
    ),
  );
  expect(screen.getByLabelText("Name to exclude on workstation").value).toBe(
    "codex",
  );
  expect(await loadLocalWork(scope, "draft", target)).toEqual({
    name: "codex",
  });
});
