export const proxyOnlyCommands = new Set([
  "agents.me.get",
  "agents.me.keys.rotate",
  "agents.me.patch",
  "agents.me.revoke",
  "auth.agents.register",
  "auth.token",
  "events.stream",
  "inbox.stream",
  "meta.commands.get",
  "meta.commands.list",
  "meta.concepts.get",
  "meta.concepts.list",
  "derived.rebuild",
]);

export const mockSupportedCommands = new Set([
  "actors.list",
  "actors.register",
  "artifacts.content.get",
  "artifacts.create",
  "artifacts.get",
  "artifacts.list",
  "commitments.create",
  "commitments.get",
  "commitments.list",
  "commitments.patch",
  "events.create",
  "events.get",
  "inbox.ack",
  "inbox.list",
  "meta.handshake",
  "meta.health",
  "meta.version",
  "packets.receipts.create",
  "packets.reviews.create",
  "packets.work-orders.create",
  "snapshots.get",
  "threads.create",
  "threads.get",
  "threads.list",
  "threads.patch",
  "threads.timeline",
]);

export function isProxyOnly(commandId) {
  return proxyOnlyCommands.has(commandId);
}

export function isMockSupported(commandId) {
  return mockSupportedCommands.has(commandId);
}

export function getParityStatus(commandId) {
  if (isProxyOnly(commandId)) {
    return "proxy_only";
  }
  if (isMockSupported(commandId)) {
    return "mock_supported";
  }
  return "unsupported";
}

export const clientCommandIds = [
  "meta.version",
  "meta.handshake",
  "actors.register",
  "actors.list",
  "threads.create",
  "threads.list",
  "threads.get",
  "threads.patch",
  "threads.timeline",
  "snapshots.get",
  "commitments.create",
  "commitments.list",
  "commitments.get",
  "commitments.patch",
  "artifacts.create",
  "artifacts.list",
  "artifacts.get",
  "artifacts.content.get",
  "events.create",
  "events.get",
  "packets.work-orders.create",
  "packets.receipts.create",
  "packets.reviews.create",
  "inbox.list",
  "inbox.ack",
];
