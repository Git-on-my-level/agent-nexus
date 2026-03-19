import { fileURLToPath } from "node:url";

import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    alias: {
      "$app/paths": fileURLToPath(
        new URL("./tests/mocks/app-paths.js", import.meta.url),
      ),
      "$env/dynamic/private": fileURLToPath(
        new URL("./tests/mocks/env-dynamic-private.js", import.meta.url),
      ),
      $lib: fileURLToPath(new URL("./src/lib", import.meta.url)),
    },
  },
  test: {
    include: ["tests/unit/**/*.test.js"],
    environment: "node",
  },
});
