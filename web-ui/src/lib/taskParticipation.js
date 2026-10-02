/** Task-scoped, read-only participation. Never fetch or retain private sessions. */
export const PARTICIPATION_TASK_LIMIT = 8;
const PAGE_LIMIT = 100;
const MAX_PAGES = 3;
const CONCURRENCY = 3;
const CACHE_MS = 10_000;
const ACTIVITIES = new Set(["active", "idle", "left", "closed", "stale"]);
const text = (value) => (typeof value === "string" ? value.trim() : "");
const instant = (value) =>
  text(value) && Number.isFinite(Date.parse(value)) ? value : "";

export function participationTasks(tasks) {
  const seen = new Set();
  return (Array.isArray(tasks) ? tasks : [])
    .map((task) => ({ ref: text(task?.ref), title: text(task?.title) }))
    .filter((task) => {
      if (!task.ref || seen.has(task.ref)) return false;
      seen.add(task.ref);
      return true;
    })
    .slice(0, PARTICIPATION_TASK_LIMIT);
}

/** Explicit allowlist: owner-only IDs and future provider metadata stop here. */
export function publicParticipant(participant) {
  const id = text(participant?.participant_id);
  if (!id) return null;
  return {
    id,
    agentId: text(participant.agent_id),
    actorId: text(participant.actor_id),
    activity: ACTIVITIES.has(participant.activity)
      ? participant.activity
      : "unknown",
    active: participant.active === true,
    lastSeenAt: instant(participant.last_seen_at),
    expiresAt: instant(participant.expires_at),
  };
}

export function participationState(participant, now = Date.now()) {
  const activity = participant.activity;
  if (activity === "left") return { label: "Left", active: false };
  if (activity === "closed") return { label: "Session closed", active: false };
  if (
    activity === "unknown" ||
    !participant.expiresAt ||
    !participant.lastSeenAt
  )
    return { label: "Activity unknown", active: false };
  if (activity === "stale" || Date.parse(participant.expiresAt) <= now)
    return { label: "Stale · activity unknown", active: false };
  if (activity === "active" && participant.active)
    return { label: "Currently active", active: true };
  if (activity === "idle") return { label: "Idle", active: false };
  // A fresh participation report can have an idle parent session. The API does
  // not disclose that session's state, so do not guess offline or idle here.
  return { label: "Not currently active", active: false };
}

export function participationSummary(groups, now = Date.now()) {
  const rows = groups.flatMap((group) => group.participants);
  return {
    participating: rows.filter(
      (row) => !["left", "closed"].includes(row.activity),
    ).length,
    active: rows.filter((row) => participationState(row, now).active).length,
    partial: groups.some((group) => group.unavailable || group.hasMore),
  };
}

/** Instance-local cache prevents cross-workspace or cross-user reuse. */
export function createParticipationReader(client, { clock = Date.now } = {}) {
  const cache = new Map();
  const pending = new Map();
  let disposed = false;
  async function taskRead(ref, force) {
    if (disposed)
      return { participants: [], hasMore: false, unavailable: true };
    if (pending.has(ref)) return pending.get(ref);
    const cached = cache.get(ref);
    if (!force && cached && clock() - cached.at < CACHE_MS) return cached.value;
    const promise = (async () => {
      const participants = new Map();
      let cursor = "";
      const cursors = new Set();
      try {
        for (let page = 0; page < MAX_PAGES; page += 1) {
          if (disposed) throw new Error("Participation scope changed");
          const response = await client.listWorkParticipants(ref, {
            limit: PAGE_LIMIT,
            ...(cursor ? { cursor } : {}),
          });
          if (disposed) throw new Error("Participation scope changed");
          if (!Array.isArray(response?.participants))
            throw new Error("Invalid participation response");
          for (const raw of response.participants) {
            const participant = publicParticipant(raw);
            if (participant) participants.set(participant.id, participant);
          }
          cursor = text(response.next_cursor);
          if (!cursor || cursors.has(cursor)) break;
          cursors.add(cursor);
        }
        const value = {
          participants: [...participants.values()].sort((a, b) =>
            a.id.localeCompare(b.id),
          ),
          hasMore: Boolean(cursor),
          unavailable: false,
        };
        cache.set(ref, { at: clock(), value });
        return value;
      } catch {
        // Do not show a previous active count or raw error detail after an
        // access failure; even a denied task's existence must not be inferred.
        cache.delete(ref);
        return { participants: [], hasMore: false, unavailable: true };
      }
    })();
    pending.set(ref, promise);
    try {
      return await promise;
    } finally {
      pending.delete(ref);
    }
  }
  async function read(tasks, { agentId = "", force = false } = {}) {
    const selected = participationTasks(tasks);
    const groups = new Array(selected.length);
    let next = 0;
    await Promise.all(
      Array.from(
        { length: Math.min(CONCURRENCY, selected.length) },
        async () => {
          while (!disposed && next < selected.length) {
            const index = next++;
            const task = selected[index];
            const result = await taskRead(task.ref, force);
            groups[index] = {
              ...task,
              ...result,
              participants: result.participants.filter(
                (row) => !agentId || row.agentId === agentId,
              ),
            };
          }
        },
      ),
    );
    return groups.filter(Boolean);
  }
  read.dispose = () => {
    disposed = true;
    cache.clear();
  };
  return read;
}
