import { redirectTo } from "$lib/legacyRedirect.js";

/**
 * The swimlane board moved to Watch, so one board serves both links. Table
 * and By placement stay here.
 */
export function load({ params, url }) {
  if (url.searchParams.get("view") !== "lanes") return;
  redirectTo(params, url, "/watch");
}
