/**
 * Primary navigation is three destinations: Decide (every decision waiting
 * on the reader), Watch (outcomes per workspace) and Ask (the PM
 * conversation). Ask is flagged `requiresPm`: the shell hides it entirely
 * while no PM is connected, and PM setup lives one hop down under Settings.
 * Everything else (Initiatives, Docs, Agents, Archive, Access, Secrets,
 * Integrations, Audit, Threads) is secondary: it sits in the account menu in
 * the sidebar footer, in the mobile More hub and in the command palette.
 */
export const navigationItems = [
  {
    label: "Decide",
    href: "/decide",
    icon: "inbox",
    hint: "Waiting on you",
    // The shell shows how many decisions wait on the reader beside this item.
    count: "decide-waiting",
  },
  {
    label: "Watch",
    href: "/watch",
    icon: "overview",
    hint: "Outcomes",
  },
  {
    label: "Ask",
    href: "/pm",
    icon: "askPm",
    hint: "Your PM",
    requiresPm: true,
  },
];

/** Secondary destinations, grouped. Rendered in the sidebar footer and on /more. */
export const settingsNavGroups = [
  {
    label: "Workspace",
    items: [
      {
        label: "Initiatives",
        href: "/initiatives",
        icon: "tasks",
        hint: "All initiatives",
      },
      {
        label: "Docs",
        href: "/docs",
        icon: "docs",
        hint: "Shared knowledge",
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
        label: "Archive",
        href: "/archive",
        icon: "docs",
        hint: "Archived work and context",
      },
    ],
  },
  {
    label: "Settings",
    items: [
      {
        label: "Access",
        href: "/access",
        icon: "access",
        hint: "Hosts, people and invites",
        // The shell shows how many access requests wait for a decision
        // beside this item, and on the account trigger that hides it.
        count: "access-pending",
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
      {
        label: "PM setup",
        href: "/pm/setup",
        icon: "askPm",
        hint: "Connect or manage your PM",
      },
    ],
  },
  {
    // Infrastructure surfaces. These expose core primitives that are
    // deliberately not operator nouns (see anx-ui-spec.md "Canonical operator
    // vocabulary"). They belong behind an explicit Diagnostics label rather
    // than in primary nav, and rather than being reachable only by typing a
    // URL.
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
    // Setup is a short read, not a thread; it does not want the chat column.
    match: /^\/pm\/setup$/,
    mode: "standard",
    maxWidth: "42rem",
  },
  {
    match: /^\/pm(\/|$)/,
    mode: "standard",
    maxWidth: "56rem",
  },
  {
    match: /^\/(decide|watch|initiatives|integrations)(\/|$)/,
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

/** Live routes that are in neither list: the more hub. */
const NON_NAV_ROUTES = ["/more"];

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

/**
 * What the first paint of a list destination looks like before its rows
 * arrive: the page's own name, and placeholders where the rows will be.
 *
 * A cold load used to paint "Loading workspace…" over the whole screen for a
 * second or two, so the reader could not see where they had landed until the
 * data did. These titles are deliberately the same strings the pages render,
 * so nothing moves when the real header replaces the placeholder.
 */
const DESTINATION_SKELETONS = [
  { match: /^\/initiatives$/, title: "Initiatives", rows: 8 },
  { match: /^\/docs$/, title: "Docs", rows: 8 },
  { match: /^\/threads$/, title: "Threads", rows: 6 },
];

/**
 * `{ title, rows }` for a list destination, or null where the shape of the
 * page is not a list of rows and a guess would be worse than a blank.
 */
export function getDestinationSkeleton(pathname) {
  const normalizedPathname = normalizePathname(pathname);
  const matched = DESTINATION_SKELETONS.find((rule) =>
    rule.match.test(normalizedPathname),
  );
  return matched ? { title: matched.title, rows: matched.rows } : null;
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
