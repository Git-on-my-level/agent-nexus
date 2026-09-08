/**
 * Primary navigation: the four places a person actually goes.
 * Everything else lives in the secondary groups below (sidebar footer and
 * the mobile More hub).
 */
export const navigationItems = [
  {
    label: "Home",
    href: "/",
    icon: "home",
    hint: "What changed",
  },
  {
    label: "Inbox",
    href: "/inbox",
    icon: "inbox",
    hint: "Needs attention",
  },
  {
    label: "Work",
    href: "/work",
    icon: "boards",
    hint: "Table and board",
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
    label: "Follow-through",
    items: [
      {
        label: "Decisions",
        href: "/decisions",
        icon: "inbox",
        hint: "Answers, delivery, receipts",
      },
      {
        label: "Integrations",
        href: "/integrations",
        icon: "events",
        hint: "Source freshness and coverage",
      },
    ],
  },
  {
    label: "Workspace",
    items: [
      {
        label: "Topics",
        href: "/topics",
        icon: "topics",
        hint: "Projects and discussions",
      },
      {
        label: "Boards",
        href: "/boards",
        icon: "boards",
        hint: "Card boards",
      },
      {
        label: "Docs",
        href: "/docs",
        icon: "docs",
        hint: "Versioned documents",
      },
      {
        label: "Events",
        href: "/events",
        icon: "events",
        hint: "Full workspace history",
      },
      {
        label: "Artifacts",
        href: "/artifacts",
        icon: "artifacts",
        hint: "Revision artifacts and payloads",
      },
    ],
  },
  {
    label: "Admin",
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
        label: "Trash",
        href: "/trash",
        icon: "trash",
        hint: "Trashed and restorable items",
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
    match: /^\/(work|decisions|integrations)(\/|$)/,
    mode: "fluid",
    maxWidth: "112rem",
  },
  {
    match: /^\/$/,
    mode: "wide",
    maxWidth: "92rem",
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
    match: /^\/artifacts\/[^/]+/,
    mode: "wide",
    maxWidth: "96rem",
  },
  {
    match: /^\/docs\/[^/]+/,
    mode: "fluid",
    maxWidth: "112rem",
  },
  {
    match: /^\/trash$/,
    mode: "wide",
    maxWidth: "88rem",
  },
  {
    match: /^\/(threads|topics|events|artifacts|docs|boards)$/,
    mode: "wide",
    maxWidth: "88rem",
  },
  {
    match: /^\/boards\/[^/]+/,
    mode: "fluid",
    maxWidth: "112rem",
  },
  {
    match: /^\/inbox$/,
    mode: "wide",
    maxWidth: "84rem",
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
