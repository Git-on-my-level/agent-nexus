import { sveltekit } from "@sveltejs/kit/vite";
import { realpathSync } from "node:fs";
import { createHash } from "node:crypto";
import path from "node:path";
import { defineConfig, searchForWorkspaceRoot } from "vite";

// Dev/preview only: lets the e2e harness prove the server it is about to drive
// was started from THIS checkout. A run pointed at another worktree's server
// (`PLAYWRIGHT_REUSE_EXISTING_*`, a pinned `PLAYWRIGHT_PORT`) otherwise fails
// as "element(s) not found" and reads as an app bug. See
// tests/serverIdentity.js. No build hook, so it never reaches the bundle.
export const E2E_IDENTITY_PATH = "/__anx/e2e-identity";

function e2eIdentityPlugin() {
  let root = process.cwd();
  const handler = (_req, res) => {
    res.setHeader("content-type", "application/json");
    res.end(
      JSON.stringify({
        root: realpathSync(root),
        pid: process.pid,
        basePath: process.env.ANX_UI_BASE_PATH ?? "",
      }),
    );
  };
  return {
    name: "anx-e2e-identity",
    configResolved(config) {
      root = config.root;
    },
    configureServer(server) {
      server.middlewares.use(E2E_IDENTITY_PATH, handler);
    },
    configurePreviewServer(server) {
      server.middlewares.use(E2E_IDENTITY_PATH, handler);
    },
  };
}

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
    plugins: [sveltekit(), e2eIdentityPlugin()],
    // Playwright starts root and base-path Vite servers together. Their
    // optimizer hashes differ; sharing a cache makes one server reject the
    // other's dependencies with 504 Outdated Optimize Dep before hydration.
    cacheDir: path.resolve(
      "node_modules",
      `.vite-${createHash("sha256")
        .update(process.env.ANX_UI_BASE_PATH ?? "")
        .digest("hex")
        .slice(0, 12)}`,
    ),
    server: {
      fs: {
        allow: [
          searchForWorkspaceRoot(process.cwd()),
          ...linkedPackageRoots(),
          ...(process.env.ANX_UI_SHARED_ROOT
            ? [path.resolve(process.env.ANX_UI_SHARED_ROOT)]
            : []),
        ],
      },
    },
  };
});
