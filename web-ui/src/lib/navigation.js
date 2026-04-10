export const navigationItems = [
  {
    label: "Home",
    href: "/",
    icon: "home",
    hint: "Overview",
  },
  {
    label: "Inbox",
    href: "/inbox",
    icon: "inbox",
    hint: "Needs attention",
  },
  {
    label: "Topics",
    href: "/topics",
    icon: "topics",
    hint: "Ongoing work",
  },
  {
    label: "Boards",
    href: "/boards",
    icon: "boards",
    hint: "Kanban boards",
  },
  {
    label: "Docs",
    href: "/docs",
    icon: "docs",
    hint: "Docs and versioned content",
  },
];

/** Secondary destinations grouped with the identity panel (sidebar bottom). */
export const settingsNavItems = [
  {
    label: "Artifacts",
    href: "/artifacts",
    icon: "artifacts",
    hint: "Receipts, reviews, and evidence",
  },
  {
    label: "Trash",
    href: "/trash",
    icon: "trash",
    hint: "Trashed and restorable items",
  },
  {
    label: "Access",
    href: "/access",
    icon: "access",
    hint: "Manage principals and invites",
  },
];

const SHELL_CONTENT_RULES = [
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
    match: /^\/(threads|topics|artifacts|docs|boards)$/,
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
