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

/** Step statuses, in the order a reader scans them. */
export const PLAN_STEP_STATUSES = Object.freeze([
  "done",
  "active",
  "blocked",
  "not_started",
]);

export const PLAN_SHAPES = Object.freeze(["chain", "dag", "lanes"]);

export const PLAN_HEALTH = Object.freeze(["on_track", "stalled", "blocked"]);

/**
 * The caps the plan contract states (`initiative_plan.max_steps`,
 * `max_dependencies_per_step`). Matching them matters in both directions: a
 * lower cap here would silently drop dependencies the server accepted.
 */
export const PLAN_LIMITS = Object.freeze({ steps: 200, afterPerStep: 50 });

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
