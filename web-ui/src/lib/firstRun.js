/**
 * Has anything ever used this workspace?
 *
 * A workspace that was provisioned and never used renders an Overview of
 * five empty sections, whose only call to action is pinning a visual report
 * — a thing that cannot exist until an agent has written one. The reader has
 * no way to learn from that page that the first move is connecting the
 * computer their agents run on.
 *
 * So the Overview asks this question first and, when the answer is yes,
 * spends its whole first screen on that one move instead.
 *
 * Pure, and computed from the Overview snapshot the page has already loaded:
 * deciding this must not cost a request, and must not grow with the size of
 * a workspace that turns out not to be cold.
 */

/**
 * A section of the snapshot that could not be read is not evidence of
 * emptiness. `status !== "ok"` means core refused or failed, and treating
 * that as "nothing here" would greet a reader whose workspace is full with
 * an invitation to connect their first agent.
 *
 * @param {{ status?: string } | undefined | null} section
 */
function readable(section) {
  return section?.status === "ok";
}

/**
 * Whether the workspace has never had an agent in it.
 *
 * The roster is the deciding fact; the rest guard against calling a working
 * workspace cold because its agents happen to be between runs. Anything
 * unreadable answers "not cold" — the normal Overview is the safe fallback,
 * and it reports its own read failures.
 *
 * @param {ReturnType<typeof import("./overview.js").loadOverview> extends Promise<infer T> ? T : never} [model]
 * @returns {boolean}
 */
export function isColdStart(model) {
  if (!model) return false;
  if (!readable(model.agents) || !readable(model.work)) return false;
  if (!readable(model.initiatives) || !readable(model.needsYou)) return false;
  // One identity that has never run still counts: somebody got that far.
  if ((model.agents.known ?? 0) > 0) return false;
  if ((model.work.total ?? 0) > 0) return false;
  if ((model.initiatives.count ?? 0) > 0) return false;
  if ((model.initiatives.items?.length ?? 0) > 0) return false;
  if ((model.needsYou.count ?? 0) > 0) return false;
  if ((model.needsYou.rows?.length ?? 0) > 0) return false;
  return true;
}

/**
 * Whether the snapshot shows an agent identity that has reached this
 * workspace.
 *
 * The arrival banner is this fact, not the end of emptiness. A later
 * unreadable roster, or a task that landed without a roster row, is not
 * "your first agent is connected".
 *
 * @param {ReturnType<typeof import("./overview.js").loadOverview> extends Promise<infer T> ? T : never} [model]
 * @returns {boolean}
 */
export function hasArrivedAgent(model) {
  if (!model || !readable(model.agents)) return false;
  return (model.agents.known ?? 0) > 0;
}

/**
 * The agent harnesses the connect prompt is written for.
 *
 * Named in the copy because "paste this into your agent" is an instruction a
 * reader cannot follow until they know which of their tools counts as one.
 * Generic: these are the harnesses the OSS CLI already discovers, not a
 * hosted product's integration list.
 */
export const CONNECT_HARNESSES = "Claude Code, Codex or Cursor";
