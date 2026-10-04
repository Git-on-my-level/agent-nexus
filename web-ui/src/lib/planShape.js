/**
 * Shape and layout for initiative plans.
 *
 * A plan is a list of steps with `after[]` dependencies. Agents never choose a
 * view: the shape of what they wrote picks it. Adding one step with `after:`
 * can turn a timeline into a tree, so classification has to be a pure function
 * of the step list rather than an authored field.
 *
 * Everything here is pure and synchronous so the renderer, the Overview tiles
 * and the unit tests all read the same geometry. The server computes the same
 * values (see the initiative plan contract); when it sends them, prefer the
 * server's and use these as the fallback for fixtures and for plans the UI
 * holds before a save lands.
 */

/** Step statuses, in the order a reader scans them. */
export const PLAN_STEP_STATUSES = Object.freeze([
  "done",
  "active",
  "blocked",
  "not_started",
]);

export const PLAN_SHAPES = Object.freeze(["chain", "dag", "lanes"]);

export const PLAN_HEALTH = Object.freeze(["on_track", "stalled", "blocked"]);

/** A plan with no movement for this long reads as stalled. */
export const DEFAULT_STALLED_DAYS = 5;

/**
 * Caps that keep a hand-written plan from rendering into something unusable.
 * The server validates its own limits; these stop a pathological payload from
 * freezing the browser before that validation exists.
 */
export const PLAN_LIMITS = Object.freeze({ steps: 200, afterPerStep: 16 });

/**
 * Card phase to step status. `cancelled` is closed, not outstanding, so it
 * lands on `done` exactly as `CLOSED_PHASES` in `pm/presentation.js` treats it;
 * progress counts it as resolved rather than leaving it to block a path.
 */
const PHASE_TO_STEP_STATUS = Object.freeze({
  backlog: "not_started",
  ready: "not_started",
  unknown: "not_started",
  in_progress: "active",
  review: "active",
  blocked: "blocked",
  done: "done",
  cancelled: "done",
});

const asText = (value) => String(value ?? "").trim();

const isStepStatus = (value) => PLAN_STEP_STATUSES.includes(value);

/**
 * Clean a raw plan into a step list this module can lay out, and say what was
 * dropped. A plan is agent-authored, so a bad `after` must not take the page
 * down: the step survives and loses the edge, and the caller can surface the
 * issue instead of silently rendering a different plan than was written.
 *
 * @param {{ steps?: unknown[] }|null|undefined} plan
 * @returns {{ steps: Array<{id: string, title: string, ref: string, after: string[], due: string, status: string}>, issues: string[] }}
 */
export function normalizePlanSteps(plan) {
  const raw = Array.isArray(plan?.steps) ? plan.steps : [];
  const issues = [];
  if (raw.length > PLAN_LIMITS.steps) {
    issues.push(
      `Plan has ${raw.length} steps; only the first ${PLAN_LIMITS.steps} are shown.`,
    );
  }

  const byId = new Map();
  for (const entry of raw.slice(0, PLAN_LIMITS.steps)) {
    const id = asText(entry?.id);
    if (!id) {
      issues.push("A step without an id was skipped.");
      continue;
    }
    if (byId.has(id)) {
      issues.push(`Duplicate step id "${id}" was skipped.`);
      continue;
    }
    byId.set(id, {
      id,
      title: asText(entry?.title) || id,
      ref: asText(entry?.ref),
      due: asText(entry?.due),
      status: isStepStatus(asText(entry?.status)) ? asText(entry.status) : "",
      after: Array.isArray(entry?.after) ? entry.after.map(asText) : [],
    });
  }

  const steps = [];
  for (const step of byId.values()) {
    const after = [];
    for (const dependency of step.after) {
      if (after.length >= PLAN_LIMITS.afterPerStep) break;
      if (!dependency || dependency === step.id) continue;
      if (!byId.has(dependency)) {
        issues.push(
          `Step "${step.id}" depends on "${dependency}", which is not in the plan.`,
        );
        continue;
      }
      if (!after.includes(dependency)) after.push(dependency);
    }
    steps.push({ ...step, after });
  }

  // One plan can repeat the same fault on many steps; a reader needs the fact
  // once, not once per step.
  return { steps, issues: [...new Set(issues)] };
}

/**
 * A step's status as a reader should see it. A ref is the truth when we can
 * resolve it — that is the point of linking steps to real work instead of
 * writing progress prose. The authored `status` is the fallback for steps with
 * no ref, or whose ref we could not resolve.
 *
 * @param {{ ref?: string, status?: string }} step
 * @param {Record<string, {status?: string, phase?: string}>} [resolved]
 */
export function effectiveStepStatus(step, resolved = {}) {
  const ref = asText(step?.ref);
  const hit = ref ? resolved?.[ref] : null;
  if (hit) {
    const direct = asText(hit.status);
    if (isStepStatus(direct)) return direct;
    const phase = asText(hit.phase) || direct;
    const mapped = PHASE_TO_STEP_STATUS[phase];
    if (mapped) return mapped;
  }
  const authored = asText(step?.status);
  return isStepStatus(authored) ? authored : "not_started";
}

/**
 * Longest-path layering by Kahn's algorithm: a step sits one layer after its
 * latest dependency. Steps inside a dependency cycle can never be emitted, so
 * they come back separately rather than hanging the layout — the plan contract
 * rejects cycles, but `dependency-diagram` panels do not, and both render here.
 *
 * @param {Array<{id: string, after: string[]}>} steps
 * @returns {{ layers: string[][], cyclic: string[] }}
 */
export function layerSteps(steps) {
  const order = steps.map((step) => step.id);
  const position = new Map(order.map((id, index) => [id, index]));
  const remaining = new Map(steps.map((step) => [step.id, [...step.after]]));
  const layerOf = new Map();

  let frontier = order.filter((id) => remaining.get(id).length === 0);
  let depth = 0;
  while (frontier.length) {
    for (const id of frontier) layerOf.set(id, depth);
    const next = [];
    for (const [id, after] of remaining) {
      if (layerOf.has(id)) continue;
      if (after.every((dependency) => layerOf.has(dependency))) next.push(id);
    }
    frontier = next;
    depth += 1;
  }

  const layers = [];
  for (const [id, layer] of layerOf) {
    (layers[layer] ??= []).push(id);
  }
  for (const layer of layers) {
    layer.sort((a, b) => position.get(a) - position.get(b));
  }

  return {
    layers: layers.map((layer) => layer ?? []),
    cyclic: order.filter((id) => !layerOf.has(id)),
  };
}

/** Undirected connected components over `after` edges, in declaration order. */
function connectedComponents(steps) {
  const neighbours = new Map(steps.map((step) => [step.id, new Set()]));
  for (const step of steps) {
    for (const dependency of step.after) {
      neighbours.get(step.id).add(dependency);
      neighbours.get(dependency).add(step.id);
    }
  }

  const seen = new Set();
  const components = [];
  for (const step of steps) {
    if (seen.has(step.id)) continue;
    const group = [];
    const stack = [step.id];
    seen.add(step.id);
    while (stack.length) {
      const id = stack.pop();
      group.push(id);
      for (const next of neighbours.get(id)) {
        if (seen.has(next)) continue;
        seen.add(next);
        stack.push(next);
      }
    }
    components.push(group);
  }
  return components;
}

/**
 * Which view the plan's shape asks for.
 *
 * - `chain`: one path, start to finish. Renders as a timeline.
 * - `dag`: something branches or merges. Renders as a tech tree.
 * - `lanes`: separate chains running in parallel. Renders as lanes.
 *
 * A plan with no dependencies at all is a `chain`, not N one-step lanes: a
 * flat list of steps is the commonest plan there is, and a timeline reads it
 * far more cheaply than a lane per step. `lanes` therefore needs at least one
 * component that actually links two steps together.
 *
 * @param {Array<{id: string, after: string[]}>} steps
 * @returns {"chain"|"dag"|"lanes"}
 */
export function classifyPlanShape(steps) {
  if (!steps.length) return "chain";

  const outDegree = new Map(steps.map((step) => [step.id, 0]));
  let edges = 0;
  for (const step of steps) {
    if (step.after.length > 1) return "dag";
    for (const dependency of step.after) {
      edges += 1;
      const next = (outDegree.get(dependency) ?? 0) + 1;
      if (next > 1) return "dag";
      outDegree.set(dependency, next);
    }
  }

  if (edges === 0) return "chain";
  const components = connectedComponents(steps);
  return components.length > 1 ? "lanes" : "chain";
}

/**
 * The longest remaining chain of work: the path through steps that are not
 * done which reaches furthest before it runs out of successors. This is what
 * decides the finish date, so it is what gets highlighted.
 *
 * Ties break on declaration order, so the same plan always highlights the same
 * path.
 *
 * @param {Array<{id: string, after: string[]}>} steps
 * @param {Record<string, string>} statusById
 * @returns {string[]}
 */
export function criticalPath(steps, statusById = {}) {
  const open = steps.filter((step) => statusById[step.id] !== "done");
  if (!open.length) return [];

  const openIds = new Set(open.map((step) => step.id));
  const position = new Map(open.map((step, index) => [step.id, index]));
  const dependenciesOf = new Map(
    open.map((step) => [
      step.id,
      step.after.filter((dependency) => openIds.has(dependency)),
    ]),
  );

  // Longest path ending at each step, walked in layer order so every
  // dependency is already solved. Cyclic steps never become ready and stay out
  // of the result rather than looping forever.
  const { layers } = layerSteps(
    open.map((step) => ({ id: step.id, after: dependenciesOf.get(step.id) })),
  );
  const best = new Map();
  for (const layer of layers) {
    for (const id of layer) {
      let winner = null;
      for (const dependency of dependenciesOf.get(id)) {
        const candidate = best.get(dependency);
        if (!candidate) continue;
        if (
          !winner ||
          candidate.length > winner.length ||
          (candidate.length === winner.length &&
            position.get(candidate.at(-1)) < position.get(winner.at(-1)))
        ) {
          winner = candidate;
        }
      }
      best.set(id, winner ? [...winner, id] : [id]);
    }
  }

  let longest = [];
  for (const id of open.map((step) => step.id)) {
    const path = best.get(id);
    if (!path) continue;
    if (path.length > longest.length) longest = path;
  }
  return longest;
}

/**
 * Steps that could be picked up right now: not finished, and nothing they wait
 * on is still outstanding.
 *
 * @param {Array<{id: string, after: string[]}>} steps
 * @param {Record<string, string>} statusById
 */
export function nextActionableSteps(steps, statusById = {}) {
  return steps
    .filter((step) => {
      const status = statusById[step.id];
      if (status === "done") return false;
      return step.after.every(
        (dependency) => statusById[dependency] === "done",
      );
    })
    .map((step) => step.id);
}

/**
 * Plan health. `blocked` outranks `stalled`: a blocked step on the critical
 * path is a thing someone has to act on, where staleness is only a signal.
 *
 * @param {{ statusById: Record<string, string>, path: string[], lastMovedAt?: string|number|Date|null, now?: number, stalledDays?: number }} input
 * @returns {"on_track"|"stalled"|"blocked"}
 */
export function planHealth({
  statusById = {},
  path = [],
  lastMovedAt = null,
  now = Date.now(),
  stalledDays = DEFAULT_STALLED_DAYS,
}) {
  if (path.some((id) => statusById[id] === "blocked")) return "blocked";

  const movedAt = lastMovedAt ? new Date(lastMovedAt).getTime() : NaN;
  if (Number.isFinite(movedAt)) {
    const idleDays = (Number(now) - movedAt) / 86_400_000;
    if (idleDays > stalledDays) return "stalled";
  }
  return "on_track";
}

/**
 * Everything a plan view needs, in one pass: the shape that picks the view,
 * the layered geometry, the edges, the highlighted path and the summary a tile
 * shows. A server-sent `shape` or `health` wins, since the server can see
 * activity this module cannot; both are derived here when absent, so fixtures
 * and unsaved edits still render.
 *
 * @param {{ steps?: unknown[], shape?: string, health?: string }|null|undefined} plan
 * @param {{ resolved?: Record<string, object>, lastMovedAt?: string|number|Date|null, now?: number, stalledDays?: number }} [options]
 */
export function planLayout(plan, options = {}) {
  const { steps, issues } = normalizePlanSteps(plan);
  const { resolved = {}, lastMovedAt = null, now, stalledDays } = options;

  const statusById = {};
  for (const step of steps) {
    statusById[step.id] = effectiveStepStatus(step, resolved);
  }

  const { layers, cyclic } = layerSteps(steps);
  if (cyclic.length) {
    issues.push(
      `${cyclic.length} step${cyclic.length === 1 ? "" : "s"} depend on each other in a loop and are listed after the diagram.`,
    );
  }

  const authoredShape = asText(plan?.shape);
  const shape = PLAN_SHAPES.includes(authoredShape)
    ? authoredShape
    : classifyPlanShape(steps);
  const path = criticalPath(steps, statusById);
  const done = steps.filter((step) => statusById[step.id] === "done").length;

  const stepById = new Map(steps.map((step) => [step.id, step]));
  const onPath = new Set(path);
  const column = new Map();
  layers.forEach((layer, index) => {
    layer.forEach((id, row) => column.set(id, { layer: index, row }));
  });
  cyclic.forEach((id, row) =>
    column.set(id, { layer: layers.length, row, cyclic: true }),
  );

  const nodes = steps.map((step) => ({
    ...step,
    status: statusById[step.id],
    onCriticalPath: onPath.has(step.id),
    ...(column.get(step.id) ?? { layer: 0, row: 0 }),
  }));

  const edges = [];
  for (const step of steps) {
    for (const dependency of step.after) {
      edges.push({
        from: dependency,
        to: step.id,
        onCriticalPath:
          onPath.has(dependency) &&
          onPath.has(step.id) &&
          path.indexOf(dependency) + 1 === path.indexOf(step.id),
      });
    }
  }

  const authoredHealth = asText(plan?.health);
  const health = PLAN_HEALTH.includes(authoredHealth)
    ? authoredHealth
    : planHealth({ statusById, path, lastMovedAt, now, stalledDays });

  const next = nextActionableSteps(steps, statusById).map(
    (id) => stepById.get(id).title,
  );

  return {
    shape,
    health,
    nodes,
    edges,
    layers,
    cyclic,
    criticalPath: path,
    progress: { done, total: steps.length },
    next,
    issues,
  };
}
