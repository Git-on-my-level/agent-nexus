export function cardResolutionLabel(resolution) {
  switch (String(resolution ?? "").trim()) {
    case "done":
    case "completed":
      return "Done";
    case "canceled":
    case "cancelled":
      return "Canceled";
    case "superseded":
      return "Superseded";
    default:
      return "Open";
  }
}

export function cardResolutionTone(resolution) {
  switch (String(resolution ?? "").trim()) {
    case "done":
    case "completed":
      return "text-emerald-400 bg-emerald-500/10";
    case "canceled":
    case "cancelled":
      return "text-slate-400 bg-slate-500/10";
    case "superseded":
      return "text-amber-400 bg-amber-500/10";
    default:
      return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }
}

export function priorityBadgeClasses(priority) {
  switch (String(priority ?? "").trim()) {
    case "p0":
      return "text-red-400 bg-red-500/10";
    case "p1":
      return "text-amber-400 bg-amber-500/10";
    case "p2":
      return "text-blue-400 bg-blue-500/10";
    default:
      return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }
}

/**
 * Priority chip for board cards / card detail (non-p0–p3 uses `getPriorityLabelFn`).
 * @param {string} threadPriorityRaw - e.g. thread?.priority
 * @param {(raw: string) => string} getPriorityLabelFn
 * @returns {{ label: string, class: string } | null}
 */
export function resolvePriorityBadge(threadPriorityRaw, getPriorityLabelFn) {
  const p = String(threadPriorityRaw ?? "")
    .trim()
    .toLowerCase();
  if (!p) return null;
  let label;
  switch (p) {
    case "p0":
      label = "P0";
      break;
    case "p1":
      label = "P1";
      break;
    case "p2":
      label = "P2";
      break;
    case "p3":
      label = "P3";
      break;
    default:
      label = getPriorityLabelFn(threadPriorityRaw);
      break;
  }
  return { label, class: priorityBadgeClasses(p) };
}

export function dueDateDisplay(dueAt) {
  const raw = String(dueAt ?? "").trim();
  if (!raw) return "";
  const d = new Date(raw);
  if (isNaN(d.getTime())) return "";
  return d.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

export function isOverdue(dueAt) {
  const raw = String(dueAt ?? "").trim();
  if (!raw) return false;
  const d = new Date(raw);
  if (isNaN(d.getTime())) return false;
  return d.getTime() < Date.now();
}
