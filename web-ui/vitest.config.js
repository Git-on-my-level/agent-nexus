import { fileURLToPath } from "node:url";

import { svelte } from "@sveltejs/vite-plugin-svelte";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    conditions: ["browser"],
    alias: {
      "$app/paths": fileURLToPath(
        new URL("./tests/mocks/app-paths.js", import.meta.url),
      ),
      "$env/dynamic/private": fileURLToPath(
        new URL("./tests/mocks/env-dynamic-private.js", import.meta.url),
      ),
      "$app/environment": fileURLToPath(
        new URL("./tests/mocks/app-environment.js", import.meta.url),
      ),
      "$app/stores": fileURLToPath(
        new URL("./tests/mocks/app-stores.js", import.meta.url),
      ),
      "$app/navigation": fileURLToPath(
        new URL("./tests/mocks/app-navigation.js", import.meta.url),
      ),
      $lib: fileURLToPath(new URL("./src/lib", import.meta.url)),
    },
  },
  test: {
    include: ["tests/unit/**/*.test.js", "src/**/__tests__/**/*.test.js"],
    environment: "node",
  },
});
