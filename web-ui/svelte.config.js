import adapterAuto from "@sveltejs/adapter-auto";
import adapterNode from "@sveltejs/adapter-node";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";
import { resolveUiBuildConfig } from "./buildEnv.js";
import path from "node:path";

const { basePath, useNodeAdapter } = resolveUiBuildConfig();

const config = {
  preprocess: vitePreprocess(),
  kit: {
    ...(process.env.ANX_REPORT_PREVIEW_WORK_DIR
      ? {
          outDir: path.join(
            process.env.ANX_REPORT_PREVIEW_WORK_DIR,
            "svelte-kit",
          ),
        }
      : {}),
    csp: { mode: "auto", directives: { "script-src": ["self"] } },
    adapter: useNodeAdapter ? adapterNode({ out: "build" }) : adapterAuto(),
    paths: {
      base: basePath,
    },
  },
};

export default config;
