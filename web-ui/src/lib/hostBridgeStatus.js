import { writable } from "svelte/store";
import { coreClient } from "$lib/coreClient";
import { liveAgentChanges } from "$lib/liveWorkspaceEvents.js";

// One visible-host batch per workspace, shared by both pages and every badge.
// Keep last data on transient errors; a failure never becomes "No bridge".
export const hostBridgeStatus = writable({
  workspace: "",
  hosts: {},
  checked: [],
  error: false,
});
let controller;
export function bridgeBadge(
  host,
  observation,
  now = Date.now(),
  loading = false,
) {
  if (host?.revoked_at || observation?.revoked_at)
    return { state: "revoked", label: "Revoked" };
  if (!observation)
    return loading
      ? { state: "loading", label: "Checking bridge…" }
      : { state: "unknown", label: "Bridge unavailable" };
  if (!observation.bridge) return { state: "none", label: "No bridge" };
  const live = Date.parse(observation.bridge.expires_at) > now;
  return { state: live ? "live" : "stale", label: live ? "Live" : "Stale" };
}
export function observeHostBridge(workspace, id) {
  if (!workspace || !id) return () => {};
  if (controller?.workspace !== workspace) {
    controller?.stop();
    controller = null;
  }
  if (!controller) {
    let stopped = false,
      inflight = false,
      again = false,
      timer;
    const ids = new Map();
    const state = { workspace, ids, run, stop };
    controller = state;
    hostBridgeStatus.set({ workspace, hosts: {}, checked: [], error: false });
    async function run() {
      if (stopped || document.hidden || !ids.size) return;
      if (inflight) {
        again = true;
        return;
      }
      inflight = true;
      const admitted = [...ids.keys()].slice(0, 100);
      try {
        const result = await coreClient.getHostBridgeStatus(admitted);
        if (!stopped)
          hostBridgeStatus.set({
            workspace,
            hosts: Object.fromEntries(
              (result?.hosts ?? []).map((h) => [h.host_id, h]),
            ),
            checked: admitted,
            error: false,
          });
      } catch {
        if (!stopped)
          hostBridgeStatus.update((v) => ({
            ...v,
            checked: admitted,
            error: true,
          }));
      } finally {
        inflight = false;
        if (again && !stopped) {
          again = false;
          schedule();
        }
      }
    }
    function schedule() {
      clearTimeout(timer);
      timer = setTimeout(run, 100);
    }
    const release = liveAgentChanges({
      client: coreClient,
      onChange: schedule,
    });
    const poll = setInterval(run, 60_000);
    const visible = () => {
      if (!document.hidden) schedule();
    };
    document.addEventListener("visibilitychange", visible);
    function stop() {
      stopped = true;
      clearTimeout(timer);
      clearInterval(poll);
      release();
      document.removeEventListener("visibilitychange", visible);
    }
    state.schedule = schedule;
  }
  const owned = controller;
  owned.ids.set(id, (owned.ids.get(id) ?? 0) + 1);
  owned.schedule();
  return () => {
    const n = owned.ids.get(id) ?? 0;
    if (n <= 1) owned.ids.delete(id);
    else owned.ids.set(id, n - 1);
    if (!owned.ids.size) {
      owned.stop();
      if (controller === owned) controller = null;
    }
  };
}
