import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig(() => {
  // `pnpm exec vite dev` / IDE runners skip `scripts/dev`. Universal
  // `[workspace]/+layout.js` schema checks need a core base URL for SSR.
  if (!String(process.env.ANX_CORE_BASE_URL ?? "").trim()) {
    const saasPacked =
      process.env.ANX_SAAS_PACKED_HOST_DEV === "1" ||
      process.env.ANX_SAAS_PACKED_HOST_DEV === "true";
    if (!saasPacked) {
      process.env.ANX_CORE_BASE_URL = "http://127.0.0.1:8000";
    }
  }

  return {
    plugins: [sveltekit()],
  };
});
