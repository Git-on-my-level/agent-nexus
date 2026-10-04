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
 * The caps the plan contract states (`initiative_plan.max_steps`,
 * `max_dependencies_per_step`). Matching them matters in both directions: a
 * lower cap here would silently drop dependencies the server accepted.
 */
export const PLAN_LIMITS = Object.freeze({ steps: 200, afterPerStep: 50 });

/**
 * Workflow state to step status, mirroring `plans.Status` in core.
 *
 * Anything unlisted is `not_started`, which is why `cancelled` lands there:
 * core's mapping names the states that count as finished work, and a cancelled
 * card is not finished work. (An earlier version of this file mapped it to
 * `done` by analogy with `CLOSED_PHASES`; core is the authority.)
 */
const PHASE_TO_STEP_STATUS = Object.freeze({
  done: "done",
  published: "done",
  closed: "done",
  resolved: "done",
  merged: "done",
  blocked: "blocked",
  active: "active",
  in_progress: "active",
  review: "active",
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
 * `resolved` is whatever batch ref resolve produced. `indexResolvedRefs`
 * returns a Map, while fixtures and tests find a plain object easier to write,
 * so both are read here — property access alone silently found nothing in a
 * Map, which meant a linked step kept its authored status instead of its ref's.
 *
 * @param {{ ref?: string, status?: string }} step
 * @param {Map<string, object>|Record<string, {status?: string, phase?: string}>} [resolved]
 */
export function effectiveStepStatus(step, resolved = {}) {
  const ref = asText(step?.ref);
  const hit = ref ? lookupResolved(resolved, ref) : null;
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

/** Read a ref out of either a Map or a plain lookup object. */
function lookupResolved(resolved, ref) {
  if (!resolved) return null;
  if (typeof resolved.get === "function") return resolved.get(ref) ?? null;
  return resolved[ref] ?? null;
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

/**
 * Undirected connected components over `after` edges, in declaration order.
 * These are the lanes: separate runs of work with nothing joining them.
 */
export function planComponents(steps) {
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
 * Which view the plan's shape asks for, mirroring `plans.Compute` in core:
 * anything that branches or merges is a `dag`, otherwise more than one root
 * makes `lanes`, otherwise `chain`.
 *
 * Note what that means for a flat list of steps with no `after` at all: every
 * step is a root, so it is `lanes`, not `chain`. An earlier version of this
 * file deliberately called that a chain on the grounds that a timeline reads
 * more cheaply. Core now computes and sends `shape`, so a client rule that
 * disagreed would render one way with `plan_state` and another without it;
 * matching core matters more than the preference did.
 *
 * - `chain`: one path, start to finish. Renders as a timeline.
 * - `dag`: something branches or merges. Renders as a tech tree.
 * - `lanes`: separate runs in parallel. Renders as lanes.
 *
 * A plan containing a dependency loop is also a `dag`. Core rejects cycles
 * before they reach here, but `dependency-diagram` panels are not validated
 * for them and render through the same code, and the tree is the only view
 * that parks steps it cannot order.
 *
 * @param {Array<{id: string, after: string[]}>} steps
 * @returns {"chain"|"dag"|"lanes"}
 */
export function classifyPlanShape(steps) {
  if (!steps.length) return "chain";

  const children = new Map(steps.map((step) => [step.id, 0]));
  let roots = 0;
  let branching = false;
  for (const step of steps) {
    if (step.after.length === 0) roots += 1;
    if (step.after.length > 1) branching = true;
    for (const dependency of step.after) {
      const count = (children.get(dependency) ?? 0) + 1;
      if (count > 1) branching = true;
      children.set(dependency, count);
    }
  }

  if (branching) return "dag";
  if (layerSteps(steps).cyclic.length) return "dag";
  return roots > 1 ? "lanes" : "chain";
}

/**
 * The longest remaining path through the plan, and everything derived from the
 * same walk. This is a port of `plans.Compute`; the web UI uses core's
 * `plan_state` when it has one, and this when it does not — a fixture, an
 * unsaved edit, or a `dependency-diagram` panel — so the two have to agree.
 *
 * Every unfinished step weighs 1 and finished steps weigh 0, so the path that
 * wins is the one with the most work left on it. Ties compare the step ids
 * along the path, which is what makes the answer stable rather than dependent
 * on declaration order.
 *
 * @param {Array<{id: string, after: string[]}>} steps
 * @param {Record<string, string>} statusById
 */
function walkPlan(steps, statusById = {}) {
  const byId = new Map(steps.map((step) => [step.id, step]));
  const children = new Map(steps.map((step) => [step.id, []]));
  for (const step of steps) {
    for (const dependency of step.after) {
      children.get(dependency)?.push(step.id);
    }
  }

  // Dependency order; steps in a cycle can never be ordered and are appended so
  // they still carry a status rather than disappearing from the walk.
  const { layers, cyclic } = layerSteps(steps);
  const ids = layers.flat().concat(cyclic);

  const weight = (id) => (statusById[id] === "done" ? 0 : 1);
  const compare = (a, b) => a.join("\u0000") < b.join("\u0000");

  const prefix = new Map(ids.map((id) => [id, 0]));
  const paths = new Map();
  const nextSteps = [];
  let longest = 0;
  let best = [];

  for (const id of ids) {
    let prior = [];
    for (const dependency of byId.get(id).after) {
      if (!prefix.has(dependency)) continue;
      const candidate = prefix.get(dependency);
      if (
        candidate > prefix.get(id) ||
        (candidate === prefix.get(id) &&
          compare(paths.get(dependency) ?? [], prior))
      ) {
        prefix.set(id, candidate);
        prior = paths.get(dependency) ?? [];
      }
    }
    prefix.set(id, prefix.get(id) + weight(id));
    paths.set(id, [...prior, id]);

    if (children.get(id).length === 0) {
      const path = paths.get(id);
      if (
        prefix.get(id) > longest ||
        (prefix.get(id) === longest && compare(path, best))
      ) {
        longest = prefix.get(id);
        best = path;
      }
    }

    // Actionable: not finished, not blocked, and nothing it waits on is open.
    if (statusById[id] !== "done" && statusById[id] !== "blocked") {
      if (byId.get(id).after.every((dep) => statusById[dep] === "done")) {
        nextSteps.push(id);
      }
    }
  }

  // Longest path *through* each step, to find every blocked step that sits on
  // a longest remaining path — not only the one path that won the tie-break.
  const suffix = new Map(ids.map((id) => [id, 0]));
  let blockedOnLongest = false;
  for (let index = ids.length - 1; index >= 0; index -= 1) {
    const id = ids[index];
    for (const child of children.get(id)) {
      if ((suffix.get(child) ?? 0) > suffix.get(id)) {
        suffix.set(id, suffix.get(child));
      }
    }
    suffix.set(id, suffix.get(id) + weight(id));
    if (
      longest > 0 &&
      statusById[id] === "blocked" &&
      prefix.get(id) + suffix.get(id) - weight(id) === longest
    ) {
      blockedOnLongest = true;
    }
  }

  return {
    // Finished steps are on the path but are not work remaining, so they are
    // not what the reader is being pointed at.
    criticalPath: best.filter((id) => statusById[id] !== "done"),
    nextSteps,
    longest,
    blockedOnLongest,
  };
}

/** The longest remaining chain of work. See `walkPlan`. */
export function criticalPath(steps, statusById = {}) {
  return walkPlan(steps, statusById).criticalPath;
}

/** Steps that could be picked up right now. See `walkPlan`. */
export function nextActionableSteps(steps, statusById = {}) {
  return walkPlan(steps, statusById).nextSteps;
}

/**
 * Plan health, mirroring core: `blocked` when a blocked step sits on any
 * longest remaining path, else `stalled` once nothing has moved for the
 * threshold, else `on_track`.
 *
 * A plan with no work left — finished or empty — is `on_track` however long
 * ago it last moved. Nothing is waiting, so nothing is stale.
 *
 * @param {{ steps?: Array<object>, statusById?: Record<string, string>, lastMovedAt?: string|number|Date|null, now?: number, stalledDays?: number }} input
 * @returns {"on_track"|"stalled"|"blocked"}
 */
export function planHealth({
  steps = [],
  statusById = {},
  lastMovedAt = null,
  now = Date.now(),
  stalledDays = DEFAULT_STALLED_DAYS,
}) {
  const { longest, blockedOnLongest } = walkPlan(steps, statusById);
  if (blockedOnLongest) return "blocked";
  if (longest === 0) return "on_track";

  const movedAt = lastMovedAt ? new Date(lastMovedAt).getTime() : NaN;
  if (Number.isFinite(movedAt)) {
    const idleMs = Number(now) - movedAt;
    if (idleMs >= stalledDays * 86_400_000) return "stalled";
  }
  return "on_track";
}

/**
 * Core's computed plan state, as the renderer wants it.
 *
 * `plan_state` is authoritative: core sees source activity, access scoping and
 * movement history this module cannot, so when a card carries one it is used
 * whole rather than recomputed. The local walk is the fallback for a fixture,
 * an unsaved edit, or a `dependency-diagram` panel — which is why it is a port
 * of core's algorithm rather than an approximation of it.
 *
 * @param {{steps?: object[], progress?: object, critical_path?: string[], next_steps?: string[], shape?: string, health?: string, last_movement_at?: string}|null} planState
 */
function readPlanState(planState) {
  if (!planState || typeof planState !== "object") return null;
  const shape = asText(planState.shape);
  const health = asText(planState.health);
  const statusById = {};
  for (const step of Array.isArray(planState.steps) ? planState.steps : []) {
    const id = asText(step?.id);
    if (id && isStepStatus(asText(step?.status))) {
      statusById[id] = asText(step.status);
    }
  }
  return {
    shape: PLAN_SHAPES.includes(shape) ? shape : "",
    health: PLAN_HEALTH.includes(health) ? health : "",
    statusById,
    criticalPath: Array.isArray(planState.critical_path)
      ? planState.critical_path.map(asText)
      : null,
    nextSteps: Array.isArray(planState.next_steps)
      ? planState.next_steps.map(asText)
      : null,
    progress:
      Number.isFinite(Number(planState.progress?.total)) &&
      Number.isFinite(Number(planState.progress?.done))
        ? {
            done: Number(planState.progress.done),
            total: Number(planState.progress.total),
          }
        : null,
    lastMovementAt: asText(planState.last_movement_at),
  };
}

/**
 * Everything a plan view needs, in one pass: the shape that picks the view,
 * the layered geometry, the edges, the highlighted path and the summary a tile
 * shows.
 *
 * Pass core's `plan_state` as `options.planState` and it is used as-is; without
 * one everything is derived locally by the same rules.
 *
 * @param {{ steps?: unknown[] }|null|undefined} plan the authored plan
 * @param {{ planState?: object|null, resolved?: Record<string, object>, lastMovedAt?: string|number|Date|null, now?: number, stalledDays?: number }} [options]
 */
export function planLayout(plan, options = {}) {
  const { steps, issues } = normalizePlanSteps(plan);
  const { resolved = {}, lastMovedAt = null, now, stalledDays } = options;
  const server = readPlanState(options.planState);

  const statusById = {};
  for (const step of steps) {
    statusById[step.id] =
      server?.statusById[step.id] ?? effectiveStepStatus(step, resolved);
  }

  const { layers, cyclic } = layerSteps(steps);
  if (cyclic.length) {
    issues.push(
      `${cyclic.length} step${cyclic.length === 1 ? "" : "s"} depend on each other in a loop and are listed after the diagram.`,
    );
  }

  const known = new Set(steps.map((step) => step.id));
  const shape = server?.shape || classifyPlanShape(steps);
  const path = (server?.criticalPath ?? criticalPath(steps, statusById)).filter(
    (id) => known.has(id),
  );
  const done =
    server?.progress?.done ??
    steps.filter((step) => statusById[step.id] === "done").length;
  const total = server?.progress?.total ?? steps.length;

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

  const health =
    server?.health ||
    planHealth({
      steps,
      statusById,
      lastMovedAt: server?.lastMovementAt || lastMovedAt,
      now,
      stalledDays,
    });

  const next = (server?.nextSteps ?? nextActionableSteps(steps, statusById))
    .filter((id) => known.has(id))
    .map((id) => stepById.get(id).title);

  return {
    shape,
    health,
    nodes,
    edges,
    layers,
    cyclic,
    lanes: planComponents(steps),
    criticalPath: path,
    progress: { done, total },
    next,
    issues,
  };
}

/**
 * Pixel geometry for the tech tree.
 *
 * Node boxes are a fixed size and the gaps are constants, so positions come
 * straight from each node's layer and row with nothing measured. That keeps the
 * layout deterministic — the same plan always draws the same diagram, and the
 * geometry can be unit-tested without a browser — and it is why the tree needs
 * no layout library.
 *
 * Coordinates are a plain left-to-right grid; the caller scrolls horizontally
 * when `width` exceeds the viewport.
 *
 * @param {ReturnType<typeof planLayout>} layout
 * @param {{nodeWidth?: number, nodeHeight?: number, gapX?: number, gapY?: number}} [options]
 */
export function planTreeGeometry(layout, options = {}) {
  const nodeWidth = options.nodeWidth ?? 168;
  const nodeHeight = options.nodeHeight ?? 52;
  const gapX = options.gapX ?? 48;
  const gapY = options.gapY ?? 14;

  const nodes = (layout?.nodes ?? []).map((node) => ({
    id: node.id,
    x: node.layer * (nodeWidth + gapX),
    y: node.row * (nodeHeight + gapY),
    width: nodeWidth,
    height: nodeHeight,
    onCriticalPath: Boolean(node.onCriticalPath),
  }));
  const positionById = new Map(nodes.map((node) => [node.id, node]));

  const edges = [];
  for (const edge of layout?.edges ?? []) {
    const from = positionById.get(edge.from);
    const to = positionById.get(edge.to);
    if (!from || !to) continue;
    const startX = from.x + from.width;
    const startY = from.y + from.height / 2;
    const endX = to.x;
    const endY = to.y + to.height / 2;
    // A horizontal-tangent cubic: the control points sit halfway between the
    // boxes, so edges leave and enter level with the node they touch.
    const bend = Math.max(12, (endX - startX) / 2);
    edges.push({
      from: edge.from,
      to: edge.to,
      onCriticalPath: Boolean(edge.onCriticalPath),
      path: `M ${startX} ${startY} C ${startX + bend} ${startY}, ${endX - bend} ${endY}, ${endX} ${endY}`,
    });
  }

  const layerCount = layout?.layers?.length ?? 0;
  const widestLayer = (layout?.layers ?? []).reduce(
    (widest, layer) => Math.max(widest, layer.length),
    0,
  );
  // Cyclic steps are parked in one extra column after the layered ones.
  const columns = layerCount + (layout?.cyclic?.length ? 1 : 0);
  const rows = Math.max(widestLayer, layout?.cyclic?.length ?? 0);

  return {
    width: columns > 0 ? columns * nodeWidth + (columns - 1) * gapX : 0,
    height: rows > 0 ? rows * nodeHeight + (rows - 1) * gapY : 0,
    nodes,
    edges,
  };
}

/**
 * Where the today marker belongs in a timeline: before the first step whose due
 * date has not passed. A plan with no dates gets no marker rather than one
 * parked arbitrarily.
 *
 * @param {Array<{id: string, due?: string}>} orderedNodes
 * @param {number} [now]
 * @returns {number} index to insert before, or -1 for no marker
 */
export function todayMarkerIndex(orderedNodes = [], now = Date.now()) {
  const dated = orderedNodes.filter((node) => {
    const due = Date.parse(node?.due ?? "");
    return Number.isFinite(due);
  });
  if (!dated.length) return -1;

  for (let index = 0; index < orderedNodes.length; index += 1) {
    const due = Date.parse(orderedNodes[index]?.due ?? "");
    if (Number.isFinite(due) && due >= Number(now)) return index;
  }
  // Every dated step is in the past, so today sits after all of them.
  return orderedNodes.length;
}
