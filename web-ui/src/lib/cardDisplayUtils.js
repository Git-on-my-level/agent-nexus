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
      return "text-[var(--fg-muted)] bg-[var(--line)]";
  }
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
