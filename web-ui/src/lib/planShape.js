/**
 * Layout for initiative plans. Geometry only.
 *
 * A plan is a list of steps with `after[]` dependencies. Agents never choose a
 * view: the shape of what they wrote picks it — but *core* decides that shape,
 * along with step status, the critical path, next steps, progress and health.
 * This module reads those and turns them into positions on a page.
 *
 * It used to compute them too, as a port of `plans.Compute`. A second
 * implementation of the same rules cannot be kept honest: an independent review
 * found six places where the copy had already drifted — status mapping, shape,
 * critical path, next-step ordering, progress and the stall threshold, which
 * depends on movement facts the browser cannot see at all. So the copy is gone.
 * Where core has not computed something, the view says so rather than filling
 * the gap with a guess that would disagree with the next page load.
 *
 * What stays here is what core has no opinion about: which column and row a
 * step occupies, where its edges run, and where today falls on a timeline.
 */

import { planStateHealth } from "./healthState.js";

/** Step statuses, in the order a reader scans them. */
export const PLAN_STEP_STATUSES = Object.freeze([
  "done",
  "active",
  "blocked",
  "not_started",
]);

export const PLAN_SHAPES = Object.freeze(["chain", "dag", "lanes"]);

export const PLAN_HEALTH = Object.freeze([
  "no_plan",
  "stale",
  "blocked",
  "at_risk",
  "on_track",
  "done",
]);

/**
 * The caps the plan contract states (`initiative_plan.max_steps`,
 * `max_dependencies_per_step`). Matching them matters in both directions: a
 * lower cap here would silently drop dependencies the server accepted.
 */
export const PLAN_LIMITS = Object.freeze({ steps: 200, afterPerStep: 50 });

/**
 * How many finished steps it takes before folding them is worth it.
 *
 * Folding one step replaces a box with a box: no width saved, and a reader
 * loses a step they can see. Two is where the summary starts paying.
 */
export const PLAN_COLLAPSE_MIN = 2;

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
 * Core's computed plan state, as the renderer wants it.
 *
 * `plan_state` is authoritative: core sees source activity, access scoping and
 * movement history the browser cannot. It is the only source of plan meaning
 * here: this module reads it and draws it, and computes nothing of its own.
 *
 * A second implementation of the same rules in JavaScript cannot be kept
 * honest — an independent review found six places where ours had already
 * drifted from core's — so the duplicate is gone. When a field is missing the
 * view shows nothing for it rather than guessing.
 *
 * @param {{steps?: object[], progress?: object, critical_path?: string[], next_steps?: string[], shape?: string, health?: string, last_movement_at?: string}|null} planState
 */
function readPlanState(planState) {
  if (!planState || typeof planState !== "object") return null;
  const shape = asText(planState.shape);
  const health = planStateHealth(planState);
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
 * What a plan view draws: the shape core computed, the layered geometry for it,
 * the edges, the highlighted path and the summary a tile shows.
 *
 * Geometry is the only thing computed here. Shape, step status, critical path,
 * next steps, progress and health all come from `options.planState` — core's
 * `plan_state` — and a plan rendered without one shows its steps and its
 * dependency layout and nothing else. That is deliberate: a status this module
 * invented would disagree with the one core computed from source activity and
 * principal-scoped movement facts the browser cannot see.
 *
 * @param {{ steps?: unknown[] }|null|undefined} plan the authored plan
 * @param {{ planState?: object|null }} [options]
 */
export function planLayout(plan, options = {}) {
  const { steps, issues } = normalizePlanSteps(plan);
  const server = readPlanState(options.planState);

  // No status without a computed one. `""` renders as "status unknown" rather
  // than as a confident "not started" the server never said.
  const statusById = {};
  for (const step of steps) {
    statusById[step.id] = server?.statusById[step.id] ?? "";
  }

  const { layers, cyclic } = layerSteps(steps);
  if (cyclic.length) {
    issues.push(
      `${cyclic.length} step${cyclic.length === 1 ? "" : "s"} depend on each other in a loop and are listed after the diagram.`,
    );
  }

  const known = new Set(steps.map((step) => step.id));
  // Shape picks the view. Without a computed one, fall back to the timeline:
  // it is the view that assumes least about a graph nobody has classified.
  const shape = server?.shape || "";
  const path = (server?.criticalPath ?? []).filter((id) => known.has(id));
  const progress = server?.progress ?? null;

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

  const next = (server?.nextSteps ?? [])
    .filter((id) => known.has(id))
    .map((id) => stepById.get(id).title);

  return {
    // `""` where core said nothing, so a caller can tell "not computed" from a
    // computed value and render accordingly.
    shape,
    health: server?.health ?? "",
    hasState: Boolean(server),
    nodes,
    edges,
    layers,
    cyclic,
    lanes: planComponents(steps),
    criticalPath: path,
    progress,
    lastMovementAt: server?.lastMovementAt ?? "",
    next,
    issues,
  };
}

/**
 * Pixel geometry for the tech tree.
 *
 * Node boxes and gaps come from constants and from how much room the caller
 * has, so positions come straight from each node's layer and row with nothing
 * measured per node. That keeps the layout deterministic — the same plan in
 * the same width always draws the same diagram, and the geometry can be
 * unit-tested without a browser — and it is why the tree needs no layout
 * library.
 *
 * Two things it does to keep the remaining work on screen:
 *
 * - **Done layers collapse.** A plan's finished prefix is the part the reader
 *   is least interested in and the part that takes the most width: a 10-step
 *   plan with 7 steps done used to be 70% history. A leading run of
 *   all-done layers becomes one narrow summary column, so what is left is
 *   what is shown.
 * - **Columns shrink to fit.** Given the width the caller actually has, nodes
 *   and gaps scale down to a legible floor before anything scrolls. Past that
 *   floor the diagram does scroll sideways, which is the one deliberate
 *   sideways scroller on the page.
 *
 * @param {ReturnType<typeof planLayout>} layout
 * @param {{
 *   nodeWidth?: number, nodeHeight?: number, gapX?: number, gapY?: number,
 *   minNodeWidth?: number, minGapX?: number, collapsedWidth?: number,
 *   availableWidth?: number, collapseDone?: boolean,
 * }} [options]
 * @returns {{
 *   width: number, height: number, nodes: object[], edges: object[],
 *   collapsed: {count: number, x: number, y: number, width: number, height: number}|null,
 * }}
 */
export function planTreeGeometry(layout, options = {}) {
  const preferredNodeWidth = options.nodeWidth ?? 176;
  const nodeHeight = options.nodeHeight ?? 60;
  const preferredGapX = options.gapX ?? 44;
  const gapY = options.gapY ?? 12;
  const minNodeWidth = options.minNodeWidth ?? 108;
  const minGapX = options.minGapX ?? 18;
  const collapsedWidth = options.collapsedWidth ?? 68;
  const availableWidth = Number(options.availableWidth) || 0;
  const collapseDone = options.collapseDone !== false;

  const nodesById = new Map(
    (layout?.nodes ?? []).map((node) => [node.id, node]),
  );
  const layers = (layout?.layers ?? []).map((layer) => layer ?? []);
  const cyclic = layout?.cyclic ?? [];

  // Columns, in reading order: the dependency layers, then one extra for any
  // step the layering could not place.
  const columns = layers.map((ids) => ({ ids, cyclic: false }));
  if (cyclic.length) columns.push({ ids: cyclic, cyclic: true });

  /*
   * The finished prefix. Only a *leading* run collapses: a done layer in the
   * middle of a plan is between two live ones, and hiding it would break the
   * reader's sense of what depends on what.
   */
  let collapsedCount = 0;
  let visibleColumns = columns;
  if (collapseDone) {
    let prefix = 0;
    while (prefix < columns.length) {
      const column = columns[prefix];
      if (column.cyclic || !column.ids.length) break;
      const allDone = column.ids.every(
        (id) => nodesById.get(id)?.status === "done",
      );
      if (!allDone) break;
      prefix += 1;
    }
    // Collapsing every column would leave nothing to look at; a plan that is
    // entirely done draws in full instead. And folding a single step is not a
    // fold — it swaps one box for another box — so it takes two to be worth it.
    if (prefix > 0 && prefix < columns.length) {
      const folded = columns
        .slice(0, prefix)
        .reduce((total, column) => total + column.ids.length, 0);
      if (folded >= PLAN_COLLAPSE_MIN) {
        collapsedCount = folded;
        visibleColumns = columns.slice(prefix);
      }
    }
  }

  const columnCount = visibleColumns.length;
  const collapsedLane = collapsedCount > 0 ? 1 : 0;

  /*
   * Fit. The gaps give first — they carry no information — and then the nodes,
   * down to the width a step title is still readable in.
   */
  let nodeWidth = preferredNodeWidth;
  let gapX = preferredGapX;
  const gapCount = Math.max(0, columnCount + collapsedLane - 1);
  const fixedWidth = collapsedLane ? collapsedWidth : 0;
  const totalWidth = (node, gap) =>
    fixedWidth + columnCount * node + gapCount * gap;
  if (availableWidth > 0 && columnCount > 0) {
    if (totalWidth(nodeWidth, gapX) > availableWidth) {
      const gapRoom = availableWidth - totalWidth(nodeWidth, 0);
      gapX = Math.max(minGapX, Math.floor(gapRoom / Math.max(1, gapCount)));
    }
    if (totalWidth(nodeWidth, gapX) > availableWidth) {
      const nodeRoom = availableWidth - fixedWidth - gapCount * gapX;
      nodeWidth = Math.max(minNodeWidth, Math.floor(nodeRoom / columnCount));
    }
  }

  const laneX = [];
  let cursor = 0;
  if (collapsedLane) {
    laneX.push({ x: cursor, width: collapsedWidth });
    cursor += collapsedWidth + gapX;
  }
  for (let index = 0; index < columnCount; index += 1) {
    laneX.push({ x: cursor, width: nodeWidth });
    cursor += nodeWidth + gapX;
  }
  const width = cursor > 0 ? cursor - gapX : 0;

  const nodes = [];
  const positionById = new Map();
  visibleColumns.forEach((column, columnIndex) => {
    const lane = laneX[columnIndex + collapsedLane];
    column.ids.forEach((id, row) => {
      const box = {
        id,
        x: lane.x,
        y: row * (nodeHeight + gapY),
        width: lane.width,
        height: nodeHeight,
        onCriticalPath: Boolean(nodesById.get(id)?.onCriticalPath),
      };
      nodes.push(box);
      positionById.set(id, box);
    });
  });

  const rows = visibleColumns.reduce(
    (widest, column) => Math.max(widest, column.ids.length),
    0,
  );
  const height = rows > 0 ? rows * nodeHeight + (rows - 1) * gapY : 0;

  const collapsed = collapsedLane
    ? {
        count: collapsedCount,
        x: laneX[0].x,
        y: 0,
        width: collapsedWidth,
        height: Math.max(nodeHeight, height),
      }
    : null;

  const edges = [];
  for (const edge of layout?.edges ?? []) {
    const to = positionById.get(edge.to);
    // An edge into the collapsed prefix has nothing on screen to point at.
    if (!to) continue;
    const from = positionById.get(edge.from);
    const start = from
      ? { x: from.x + from.width, y: from.y + from.height / 2 }
      : collapsed
        ? // A step that depends on finished work: the edge comes out of the
          // summary column, so the dependency is still visible as an edge.
          { x: collapsed.x + collapsed.width, y: to.y + to.height / 2 }
        : null;
    if (!start) continue;
    const endX = to.x;
    const endY = to.y + to.height / 2;
    // A horizontal-tangent cubic: the control points sit halfway between the
    // boxes, so edges leave and enter level with the node they touch.
    const bend = Math.max(12, (endX - start.x) / 2);
    edges.push({
      from: edge.from,
      to: edge.to,
      fromCollapsed: !from,
      onCriticalPath: Boolean(edge.onCriticalPath),
      path: `M ${start.x} ${start.y} C ${start.x + bend} ${start.y}, ${endX - bend} ${endY}, ${endX} ${endY}`,
    });
  }

  return { width, height, nodes, edges, collapsed };
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
