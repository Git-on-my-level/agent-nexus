import adapterAuto from "@sveltejs/adapter-auto";
import adapterNode from "@sveltejs/adapter-node";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";
import { resolveUiBuildConfig } from "./buildEnv.js";

const { basePath, useNodeAdapter } = resolveUiBuildConfig();

const config = {
  preprocess: vitePreprocess(),
  kit: {
    // Isolate standalone renderers from the active workspace dev server.
    outDir: process.env.ANX_REPORT_PREVIEW_OUT_DIR || ".svelte-kit",
    csp: { mode: "auto", directives: { "script-src": ["self"] } },
    adapter: useNodeAdapter ? adapterNode({ out: "build" }) : adapterAuto(),
    paths: {
      base: basePath,
    },
  },
};

export default config;
