import { error } from "@sveltejs/kit";
import { dev } from "$app/environment";

export const load = () => {
  if (!dev) {
    throw error(404, "Not Found");
  }
  return {};
};
