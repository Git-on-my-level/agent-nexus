/** Persisted refs win over navigation context; legacy work_ref stays readable. */
export function pinnedRefs(conversation, searchParams = new URLSearchParams()) {
  const refs = conversation
    ? [conversation.work_ref, ...(conversation.context_refs || [])]
    : [searchParams.get("work_ref"), ...searchParams.getAll("ref")];
  return [...new Set(refs.filter(Boolean))].slice(0, 8);
}

export function activityLabel(turn) {
  return turn?.activity?.at(-1)?.label || "Thinking";
}
