import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig(() => {
  // `pnpm exec vite dev` / IDE runners skip `scripts/dev`. Universal
  // `[workspace]/+layout.js` schema checks need a core base URL for SSR.
  if (!String(process.env.OAR_CORE_BASE_URL ?? "").trim()) {
    process.env.OAR_CORE_BASE_URL = "http://127.0.0.1:8000";
  }

  return {
    plugins: [sveltekit()],
  };
});
