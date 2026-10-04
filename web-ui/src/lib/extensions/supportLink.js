export function resolveHostedSupportUrl(configured) {
  try {
    const parsed = new URL(String(configured ?? "").trim());
    if (parsed.protocol === "https:" || parsed.protocol === "mailto:") {
      return parsed.toString();
    }
  } catch {
    // A standalone workspace has no external support contact by default.
  }
  return "";
}
export function supportLinkOpensInNewTab(href) {
  return String(href ?? "").startsWith("https://");
}
