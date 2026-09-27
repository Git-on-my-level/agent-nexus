import { coreClient } from "$lib/coreClient";
import { findAgentSummary } from "$lib/actorSession";
import { agentPath, runLabel } from "$lib/agentPresence.js";

/**
 * Run attribution on events. Core records `run_attribution` (`run_id`,
 * `host_id`, `agent_id`, `adapter`) on every write made inside a launcher
 * run; the UI shows it as a quiet "via run exec-…" beside the author and
 * links to the run on the agent's page.
 */

/** @type {Map<string, Promise<object | null>>} */
const runs = new Map();

/** The run behind an attribution, fetched once per run id. */
export function loadAttributedRun(runId) {
  const id = String(runId ?? "").trim();
  if (!id) return Promise.resolve(null);
  if (!runs.has(id)) {
    runs.set(
      id,
      coreClient
        .getRun(id)
        .then((response) => response?.run ?? null)
        .catch(() => {
          runs.delete(id);
          return null;
        }),
    );
  }
  return runs.get(id);
}

export function normalizeRunAttribution(value) {
  if (!value || typeof value !== "object") return null;
  const runId = String(value.run_id ?? "").trim();
  if (!runId) return null;
  return {
    runId,
    agentId: String(value.agent_id ?? "").trim(),
    hostId: String(value.host_id ?? "").trim(),
    adapter: String(value.adapter ?? "").trim(),
  };
}

/** Agent page path with the run selected, or "" when the agent is unknown. */
export function runAttributionPath(attribution, agents) {
  const agent = findAgentSummary(attribution?.agentId, agents);
  const base = agent
    ? agentPath(agent)
    : attribution?.agentId
      ? `/agents/${encodeURIComponent(attribution.agentId)}`
      : "";
  if (!base) return "";
  return `${base}?run=${encodeURIComponent(attribution.runId)}#runs`;
}

/** Label while the run loads: a short id; afterwards its launcher id. */
export function runAttributionLabel(attribution, run = null) {
  const label = run ? runLabel(run) : "";
  if (label) return label;
  return String(attribution?.runId ?? "").slice(0, 8);
}

/** Test hook. */
export function resetAttributedRuns() {
  runs.clear();
}
