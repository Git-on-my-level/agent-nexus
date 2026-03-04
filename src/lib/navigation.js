export const navigationItems = [
  { label: "Inbox", href: "/inbox" },
  { label: "Threads", href: "/threads" },
  { label: "Artifacts", href: "/artifacts" },
];

export function isKnownSection(pathname) {
  return navigationItems.some((item) => pathname === item.href);
}
