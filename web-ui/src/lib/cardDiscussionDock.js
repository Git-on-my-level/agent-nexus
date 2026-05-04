export function cardDiscussionDockPlacement(presentation) {
  return presentation === "page" ? "viewport" : "embedded";
}

export function cardDiscussionDockHostEnabled(presentation, threadId) {
  return cardDiscussionDockPlacement(presentation) === "embedded"
    ? Boolean(String(threadId ?? "").trim())
    : false;
}
