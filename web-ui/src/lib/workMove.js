/** Stored-parent moves. Plans and board layout are independent of this intent. */
export function workMoveDraft(work, destination = "under", parent = null) {
  return {
    verb: "move",
    work: {
      ref: work.ref,
      title: work.title || work.ref,
      parent_ref: work.parent_ref ?? work.parentRef ?? null,
      needs_home: work.needs_home ?? false,
      is_initiative: work.is_initiative ?? false,
    },
    destination,
    parent: parent
      ? { ref: parent.ref, title: parent.title || parent.ref }
      : null,
    reason: "",
  };
}

export function workMoveTitle(draft) {
  const card = `“${draft.work.title}”`;
  if (draft.destination === "root") return `Move ${card} to top level`;
  if (draft.destination === "unsorted") return `Move ${card} to Unsorted`;
  return `Move ${card} under “${draft.parent?.title || draft.parent?.ref || "a parent"}”`;
}

export function workMoveRequest(draft) {
  const body = {};
  if (draft.destination === "root") body.root = true;
  else if (draft.destination === "unsorted") body.unsorted = true;
  else if (draft.destination === "under" && draft.parent?.ref)
    body.under = draft.parent.ref;
  else throw new Error("Choose a parent, top level or Unsorted.");
  const reason = String(draft.reason || "").trim();
  if ([...reason].length > 2000)
    throw new Error("Keep the reason within 2000 characters.");
  if ((body.root || (body.under && draft.work.is_initiative)) && !reason)
    throw new Error("Give a reason for moving this initiative.");
  if (reason) body.reason = reason;
  return { operation: "work.move", request: { ref: draft.work.ref, body } };
}
