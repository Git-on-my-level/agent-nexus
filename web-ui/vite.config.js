import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig(() => {
  // `pnpm exec vite dev` / IDE runners skip `scripts/dev`. Universal
  // `[workspace]/+layout.js` schema checks need a core base URL for SSR.
  const hasCoreBaseUrl = String(process.env.ANX_CORE_BASE_URL ?? "").trim();
  const hasControlPlaneBaseUrl = String(
    process.env.ANX_CONTROL_BASE_URL ?? "",
  ).trim();
  if (!hasCoreBaseUrl && !hasControlPlaneBaseUrl) {
    process.env.ANX_CORE_BASE_URL = "http://127.0.0.1:8000";
  }

  return {
    plugins: [sveltekit()],
  };
});
