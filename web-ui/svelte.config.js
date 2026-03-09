import adapterAuto from "@sveltejs/adapter-auto";
import adapterNode from "@sveltejs/adapter-node";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

const useNodeAdapter = process.env.ADAPTER === "node";
const basePath = normalizeBasePath(process.env.OAR_UI_BASE_PATH);

function normalizeBasePath(value = "") {
  const trimmed = String(value ?? "").trim();
  if (!trimmed || trimmed === "/") {
    return "";
  }

  const normalized = trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
  return normalized.replace(/\/+$/, "");
}

const config = {
  preprocess: vitePreprocess(),
  kit: {
    adapter: useNodeAdapter ? adapterNode({ out: "build" }) : adapterAuto(),
    paths: {
      base: basePath,
    },
  },
};

export default config;
