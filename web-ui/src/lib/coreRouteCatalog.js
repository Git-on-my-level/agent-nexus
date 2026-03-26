import { commandRegistry } from "../../../contracts/gen/ts/dist/client.js";

let catalogByPath;

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

function normalizeCatalogPath(pathname) {
  return pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
}

function catalogKey(method, pathname) {
  return `${method.toUpperCase()}:${normalizeCatalogPath(pathname)}`;
}

function getCatalogByPath() {
  if (!catalogByPath) {
    catalogByPath = new Map(
      commandRegistry.map((command) => [
        catalogKey(command.method, command.path),
        {
          commandId: command.command_id,
          method: command.method,
          path: command.path,
          group: command.group,
          stability: command.stability,
        },
      ]),
    );
  }

  return catalogByPath;
}

function findCatalogEntry(method, pathname) {
  const catalog = getCatalogByPath();
  const key = catalogKey(method, pathname);
  if (catalog.has(key)) {
    return catalog.get(key);
  }

  const methodPrefix = `${String(method).toUpperCase()}:`;
  const normalizedPath = normalizeCatalogPath(pathname);
  for (const [entryKey, info] of catalog) {
    if (!entryKey.startsWith(methodPrefix)) {
      continue;
    }
    if (matchPath(normalizeCatalogPath(info.path), normalizedPath)) {
      return info;
    }
  }

  return null;
}

export function isProxyableCommand(method, pathname) {
  return Boolean(findCatalogEntry(method, pathname));
}

export function getCommandInfo(method, pathname) {
  return findCatalogEntry(method, pathname);
}

export function getAllProxyablePaths() {
  return Array.from(getCatalogByPath().values()).map((info) => ({
    method: info.method,
    path: info.path,
  }));
}

export function getCatalogEntries() {
  return getCatalogByPath();
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
