import { error } from "@sveltejs/kit";
import { dev } from "$app/environment";

/**
 * Simulated Stripe customer portal; only the local dev server should serve it.
 * Production and preview builds return 404.
 */
export const load = () => {
  if (!dev) {
    throw error(404, "Not Found");
  }
  return {};
};
