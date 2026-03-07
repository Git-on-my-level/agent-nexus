export const navigationItems = [
  {
    label: "Inbox",
    href: "/inbox",
    icon: "inbox",
  },
  {
    label: "Threads",
    href: "/threads",
    icon: "threads",
  },
  {
    label: "Artifacts",
    href: "/artifacts",
    icon: "artifacts",
  },
];

const SHELL_CONTENT_RULES = [
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
    match: /^\/(threads|artifacts)$/,
    mode: "wide",
    maxWidth: "88rem",
  },
  {
    match: /^\/inbox$/,
    mode: "wide",
    maxWidth: "84rem",
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
  return navigationItems.some((item) => normalizedPathname === item.href);
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
