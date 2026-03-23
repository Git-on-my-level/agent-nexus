import js from "@eslint/js";
import globals from "globals";
import svelte from "eslint-plugin-svelte";

export default [
  {
    ignores: [
      ".codex-autorunner/**",
      ".svelte-kit/**",
      "build/**",
      "coverage/**",
      "playwright-report/**",
      "test-results/**",
    ],
  },
  js.configs.recommended,
  ...svelte.configs["flat/recommended"],
  {
    files: ["**/*.{js,mjs,cjs,svelte}"],
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
  },
  {
    files: ["src/**/*.{js,svelte}"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          paths: [
            {
              name: "jsdom",
              message:
                "jsdom is Node-only and must not be imported from UI code that ships to the browser. Use isomorphic-dompurify or a server-only module under src/lib/server/.",
            },
          ],
        },
      ],
    },
  },
];
