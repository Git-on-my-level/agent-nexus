/**
 * Primary navigation: Inbox (the only attention surface), Agents (presence:
 * who is working, waiting or stale), then the product primitives Tasks and
 * Docs. The PM conversation is an action ("Ask PM" in the sidebar header and the
 * mobile bottom bar), not a destination category. Settings (Access, Secrets,
 * Integrations) and Diagnostics (Audit, Threads) live in the account menu in
 * the sidebar footer and in the mobile More hub.
 */
export const navigationItems = [
  {
    label: "Inbox",
    href: "/inbox",
    icon: "inbox",
    hint: "Needs attention",
    // The shell shows how many rows sit in Needs you beside this item.
    count: "inbox-needs-you",
  },
  {
    label: "Agents",
    href: "/agents",
    icon: "agents",
    hint: "Who is working",
    // The shell shows how many agents are working beside this item.
    count: "agents-working",
  },
  {
    label: "Tasks",
    href: "/tasks",
    icon: "tasks",
    hint: "Table and board",
  },
  {
    label: "Docs",
    href: "/docs",
    icon: "docs",
    hint: "Shared knowledge",
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
        hint: "Hosts, people and invites",
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
        icon: "integrations",
        hint: "Source freshness and coverage",
      },
    ],
  },
  {
    // Infrastructure surfaces. These expose core primitives that are
    // deliberately not operator nouns (see anx-ui-spec.md "Canonical operator
    // vocabulary"). They belong behind an explicit Diagnostics label rather
    // than in primary nav — and rather than being reachable only by typing a
    // URL, which is how `/threads` was orphaned after the Tasks refactor.
    label: "Diagnostics",
    items: [
      {
        label: "Audit",
        href: "/events",
        icon: "audit",
        hint: "Workspace history",
      },
      {
        label: "Threads",
        href: "/threads",
        icon: "threads",
        hint: "Backing timelines",
      },
    ],
  },
];

/** Flat view of the secondary destinations, in group order. */
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
    match: /^\/(tasks|inbox|integrations)(\/|$)/,
    mode: "fluid",
    maxWidth: "112rem",
  },
  {
    match: /^\/agents(\/|$)/,
    mode: "wide",
    maxWidth: "80rem",
  },
  {
    match: /^\/access$/,
    mode: "wide",
    maxWidth: "84rem",
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
    match: /^\/more$/,
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

/** Live routes that are not nav items: the Ask PM action target. */
const NON_NAV_ROUTES = ["/pm"];

export function isKnownSection(pathname) {
  const normalizedPathname = normalizePathname(pathname);
  return (
    NON_NAV_ROUTES.includes(normalizedPathname) ||
    navigationItems.some((item) => normalizedPathname === item.href) ||
    settingsNavItems.some((item) => normalizedPathname === item.href)
  );
}

/** When true, the mobile bottom "More" tab should read as active (hub + settings destinations). */
export function isMoreHubActivePath(pathname) {
  const p = normalizePathname(pathname);
  if (p === "/more" || p.startsWith("/more/")) {
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
