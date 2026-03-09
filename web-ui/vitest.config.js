import { fileURLToPath } from "node:url";

import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    alias: {
      "$app/paths": fileURLToPath(
        new URL("./tests/mocks/app-paths.js", import.meta.url),
      ),
    },
  },
  test: {
    include: ["tests/unit/**/*.test.js"],
    environment: "node",
  },
});
