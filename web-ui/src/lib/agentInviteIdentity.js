export function suggestAgentUsername(agentName) {
  let username = String(agentName ?? "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-+|-+$/g, "");

  if (!username) {
    return "";
  }
  if (username.length < 3) {
    username = `${username}-agent`;
  }
  return username.slice(0, 64).replace(/-+$/g, "");
}
