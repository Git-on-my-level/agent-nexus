import { get, writable } from "svelte/store";

import { replaceAgentRegistry } from "$lib/actorSession";
import { coreClient } from "$lib/coreClient";
import {
  liveAgentChanges,
  liveWorkspaceEvents,
} from "$lib/liveWorkspaceEvents.js";

/**
 * The workspace agent roster (`GET /agents`), kept current for the shell.
 *
 * One loader per workspace feeds the Agents nav badge, the Agents page and
 * the name registry (derived agents are named "codex on workstation-a" everywhere).
 * It re-reads when core's roster stream (`GET /stream/agents`) says the
 * roster changed (on connect, after run, presence, host and bridge changes,
 * and after a reconnect), when an ask is filed or answered (workspace
 * events, which move agents in and out of "waiting on you"), when the tab
 * becomes visible, and on a slow fallback timer for server restarts.
 */

export const agentRoster = writable(
  /** @type {{ workspace: string, agents: object[], status: "idle" | "ready" | "error", error: string, loadedAt: number }} */ ({
    workspace: "",
    agents: [],
    status: "idle",
    error: "",
    loadedAt: 0,
  }),
);

const FALLBACK_POLL_MS = 120_000;
const EVENT_DELAY_MS = 800;

/** Workspace events that change who is waiting on a human. */
const ASK_EVENT_TYPES = [
  "human_attention_requested",
  "human_attention_responded",
];

/** @type {null | { workspace: string, users: number, stop: () => void, refresh: () => Promise<void> }} */
let controller = null;

async function fetchRoster(workspace) {
  const response = await coreClient.listAgents();
  const agents = Array.isArray(response?.agents) ? response.agents : [];
  replaceAgentRegistry(agents, workspace);
  agentRoster.set({
    workspace,
    agents,
    status: "ready",
    error: "",
    loadedAt: Date.now(),
  });
}

/**
 * Keep the roster for `workspace` current while at least one consumer is
 * mounted. Returns a release function.
 */
export function startAgentRoster(workspace) {
  const key = String(workspace ?? "").trim();
  if (!key) return () => {};
  if (controller && controller.workspace !== key) {
    controller.stop();
    controller = null;
  }
  if (controller) {
    controller.users += 1;
  } else {
    let stopped = false;
    let inflight = null;
    let again = false;
    const run = async () => {
      if (stopped) return;
      if (inflight) {
        again = true;
        return inflight;
      }
      inflight = (async () => {
        try {
          await fetchRoster(key);
        } catch (error) {
          const current = get(agentRoster);
          agentRoster.set({
            ...current,
            workspace: key,
            // Keep the last roster on screen; say why it may be stale.
            agents: current.workspace === key ? current.agents : [],
            status:
              current.workspace === key && current.status === "ready"
                ? "ready"
                : "error",
            error: error instanceof Error ? error.message : String(error),
          });
        } finally {
          inflight = null;
          if (again && !stopped) {
            again = false;
            void run();
          }
        }
      })();
      return inflight;
    };
    const visible = () => typeof document === "undefined" || !document.hidden;
    const timer = setInterval(() => {
      if (visible()) void run();
    }, FALLBACK_POLL_MS);
    const onVisibility = () => {
      if (visible()) void run();
    };
    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", onVisibility);
    }
    const stopRosterStream = liveAgentChanges({
      client: coreClient,
      onChange: () => run(),
    });
    const stopAskEvents = liveWorkspaceEvents({
      client: coreClient,
      types: ASK_EVENT_TYPES,
      debounceMs: EVENT_DELAY_MS,
      onChange: () => run(),
    });
    controller = {
      workspace: key,
      users: 1,
      refresh: () => run() ?? Promise.resolve(),
      stop: () => {
        stopped = true;
        clearInterval(timer);
        stopRosterStream();
        stopAskEvents();
        if (typeof document !== "undefined") {
          document.removeEventListener("visibilitychange", onVisibility);
        }
      },
    };
    const current = get(agentRoster);
    if (current.workspace !== key) {
      agentRoster.set({
        workspace: key,
        agents: [],
        status: "idle",
        error: "",
        loadedAt: 0,
      });
    }
    void run();
  }
  return () => {
    if (!controller || controller.workspace !== key) return;
    controller.users -= 1;
    if (controller.users <= 0) {
      controller.stop();
      controller = null;
    }
  };
}

/** Re-read now (after an action on this page). */
export function refreshAgentRoster() {
  return controller?.refresh() ?? Promise.resolve();
}

/** Test hook. */
export function resetAgentRoster() {
  controller?.stop();
  controller = null;
  agentRoster.set({
    workspace: "",
    agents: [],
    status: "idle",
    error: "",
    loadedAt: 0,
  });
}
