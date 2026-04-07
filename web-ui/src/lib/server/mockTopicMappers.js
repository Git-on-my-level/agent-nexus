export function mapTopicStatusToThreadStatus(status) {
  switch (String(status ?? "").trim()) {
    case "blocked":
    case "paused":
      return "paused";
    case "resolved":
    case "closed":
    case "archived":
      return "closed";
    case "proposed":
    case "active":
    default:
      return "active";
  }
}

export function mapTopicTypeToThreadType(type) {
  const t = String(type ?? "").trim();
  if (["incident", "initiative", "case", "process"].includes(t)) {
    return t;
  }
  if (t === "decision") {
    return "case";
  }
  if (t === "objective") {
    return "process";
  }
  return "process";
}

export function mapTopicPatchToThreadPatch(patch) {
  if (!patch || typeof patch !== "object") {
    return {};
  }
  const out = {};
  if (patch.title !== undefined) {
    out.title = patch.title;
  }
  if (patch.summary !== undefined) {
    out.current_summary = patch.summary;
  }
  if (patch.type !== undefined) {
    out.type = mapTopicTypeToThreadType(patch.type);
  }
  if (patch.status !== undefined) {
    out.status = mapTopicStatusToThreadStatus(patch.status);
  }
  if (patch.provenance !== undefined) {
    out.provenance = patch.provenance;
  }
  return out;
}
