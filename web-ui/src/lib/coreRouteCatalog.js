import { commandRegistry } from "../../../contracts/gen/ts/dist/client.js";

export const catalogByPath = new Map();

for (const command of commandRegistry) {
  const normalizedPath = command.path.endsWith("/")
    ? command.path.slice(0, -1)
    : command.path;
  const key = `${command.method}:${normalizedPath}`;
  catalogByPath.set(key, {
    commandId: command.command_id,
    method: command.method,
    path: command.path,
    group: command.group,
    stability: command.stability,
  });
}

function matchPath(pattern, pathname) {
  if (pattern === pathname) {
    return true;
  }

  const patternParts = pattern.split("/");
  const pathParts = pathname.split("/");

  if (patternParts.length !== pathParts.length) {
    return false;
  }

  for (let i = 0; i < patternParts.length; i++) {
    if (patternParts[i].startsWith("{")) {
      continue;
    }
    if (patternParts[i] !== pathParts[i]) {
      return false;
    }
  }

  return true;
}

export function isProxyableCommand(method, pathname) {
  const normalizedPath = pathname.endsWith("/")
    ? pathname.slice(0, -1)
    : pathname;

  const key = `${method.toUpperCase()}:${normalizedPath}`;
  if (catalogByPath.has(key)) {
    return true;
  }

  for (const [catalogKey, info] of catalogByPath) {
    if (!catalogKey.startsWith(`${method.toUpperCase()}:`)) {
      continue;
    }
    const pattern = info.path.endsWith("/")
      ? info.path.slice(0, -1)
      : info.path;
    if (matchPath(pattern, normalizedPath)) {
      return true;
    }
  }

  return false;
}

export function getCommandInfo(method, pathname) {
  const normalizedPath = pathname.endsWith("/")
    ? pathname.slice(0, -1)
    : pathname;

  const key = `${method.toUpperCase()}:${normalizedPath}`;
  if (catalogByPath.has(key)) {
    return catalogByPath.get(key);
  }

  for (const [catalogKey, info] of catalogByPath) {
    if (!catalogKey.startsWith(`${method.toUpperCase()}:`)) {
      continue;
    }
    const pattern = info.path.endsWith("/")
      ? info.path.slice(0, -1)
      : info.path;
    if (matchPath(pattern, normalizedPath)) {
      return info;
    }
  }

  return null;
}

export function getAllProxyablePaths() {
  return Array.from(catalogByPath.values()).map((info) => ({
    method: info.method,
    path: info.path,
  }));
}

export const proxyOnlyCommands = [
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
];

export const mockSupportedCommands = commandRegistry
  .map((c) => c.command_id)
  .filter((id) => !proxyOnlyCommands.includes(id));
