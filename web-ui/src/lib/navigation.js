/**
 * Primary navigation: Inbox, Tasks, Docs, and the PM conversation surface.
 * Settings (Access, Secrets, Integrations, Audit) live in the sidebar footer
 * and the mobile More hub.
 */
export const navigationItems = [
  {
    label: "Inbox",
    href: "/inbox",
    icon: "inbox",
    hint: "Needs attention",
  },
  {
    label: "Tasks",
    href: "/tasks",
    icon: "boards",
    hint: "Table and board",
  },
  {
    label: "Docs",
    href: "/docs",
    icon: "docs",
    hint: "Shared knowledge",
  },
  {
    label: "PM",
    href: "/pm",
    icon: "topics",
    hint: "Ask, decide, follow through",
  },
];

/** Secondary destinations, grouped. Rendered in the sidebar footer and on /more. */
export const settingsNavGroups = [
  {
    label: "Settings",
    items: [
      {
        label: "Access",
        href: "/access",
        icon: "access",
        hint: "Principals and invites",
      },
      {
        label: "Secrets",
        href: "/secrets",
        icon: "secrets",
        hint: "Workspace credentials",
      },
      {
        label: "Integrations",
        href: "/integrations",
        icon: "events",
        hint: "Source freshness and coverage",
      },
      {
        label: "Audit",
        href: "/events",
        icon: "events",
        hint: "Workspace history",
      },
    ],
  },
];

/** Flat view of the secondary destinations (kept for existing consumers). */
export const settingsNavItems = settingsNavGroups.flatMap(
  (group) => group.items,
);

const SHELL_CONTENT_RULES = [
  {
    match: /^\/pm(\/|$)/,
    mode: "standard",
    maxWidth: "56rem",
  },
  {
    match: /^\/(work|tasks|inbox|integrations)(\/|$)/,
    mode: "fluid",
    maxWidth: "112rem",
  },
  {
    match: /^\/access$/,
    mode: "wide",
    maxWidth: "84rem",
  },
  {
    match: /^\/topics\/[^/]+/,
    mode: "fluid",
    maxWidth: "112rem",
  },
  {
    match: /^\/threads\/[^/]+/,
    mode: "fluid",
    maxWidth: "112rem",
  },
  {
    match: /^\/docs\/[^/]+/,
    mode: "fluid",
    maxWidth: "112rem",
  },
  {
    match: /^\/docs$/,
    mode: "wide",
    maxWidth: "88rem",
  },
  {
    match: /^\/(more|settings)$/,
    mode: "standard",
    maxWidth: "42rem",
  },
];

const DEFAULT_SHELL_CONTENT = {
  mode: "standard",
  maxWidth: "72rem",
};

function normalizePathname(pathname) {
  if (!pathname) {
    return "/";
  }

  if (pathname.length > 1 && pathname.endsWith("/")) {
    return pathname.slice(0, -1);
  }

  return pathname;
}

export function isKnownSection(pathname) {
  const normalizedPathname = normalizePathname(pathname);
  return (
    navigationItems.some((item) => normalizedPathname === item.href) ||
    settingsNavItems.some((item) => normalizedPathname === item.href)
  );
}

/** When true, the mobile bottom "More" tab should read as active (hub + settings destinations). */
export function isMoreHubActivePath(pathname) {
  const p = normalizePathname(pathname);
  if (p === "/more" || p.startsWith("/more/") || p === "/settings") {
    return true;
  }
  return settingsNavItems.some(
    (item) => p === item.href || p.startsWith(`${item.href}/`),
  );
}

export function getShellContentConfig(pathname) {
  const normalizedPathname = normalizePathname(pathname);

  const matchedRule = SHELL_CONTENT_RULES.find((rule) =>
    rule.match.test(normalizedPathname),
  );

  if (!matchedRule) {
    return DEFAULT_SHELL_CONTENT;
  }

  return {
    mode: matchedRule.mode,
    maxWidth: matchedRule.maxWidth,
  };
}
