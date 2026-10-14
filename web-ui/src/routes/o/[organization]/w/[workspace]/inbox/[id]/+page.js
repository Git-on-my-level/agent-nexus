import { redirectTo } from "$lib/legacyRedirect.js";

export function load({ params, url }) {
  redirectTo(params, url, `/decide/${encodeURIComponent(params.id)}`);
}
