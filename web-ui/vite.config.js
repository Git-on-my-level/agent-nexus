import { sveltekit } from "@sveltejs/kit/vite";
import { realpathSync } from "node:fs";
import path from "node:path";
import { defineConfig, searchForWorkspaceRoot } from "vite";

function linkedPackageRoots() {
  const roots = new Set();
  for (const spec of ["@sveltejs/kit", "@fontsource/inter"]) {
    try {
      let dir = realpathSync(path.resolve("node_modules", spec));
      while (dir !== path.dirname(dir)) {
        if (path.basename(dir) === ".pnpm") {
          roots.add(path.dirname(dir));
          break;
        }
        dir = path.dirname(dir);
      }
    } catch {
      // Local install or missing optional package.
    }
  }
  return [...roots];
}

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
    server: {
      fs: {
        allow: [searchForWorkspaceRoot(process.cwd()), ...linkedPackageRoots()],
      },
    },
  };
});
