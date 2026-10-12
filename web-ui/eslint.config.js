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
  ...svelte.configs["flat/base"],
  {
    files: ["**/*.svelte"],
    // Preserve the 2.46.1 recommended policy while upgrading the parser
    // to understand Svelte 5 error boundaries. New v3 rules are a separate migration.
    rules: {
      "no-inner-declarations": "off",
      "no-self-assign": "off",
      "svelte/comment-directive": "error",
      "svelte/system": "error",
      "svelte/no-at-debug-tags": "warn",
      "svelte/no-at-html-tags": "error",
      "svelte/no-dupe-else-if-blocks": "error",
      "svelte/no-dupe-style-properties": "error",
      "svelte/no-dynamic-slot-name": "error",
      "svelte/no-inner-declarations": "error",
      "svelte/no-not-function-handler": "error",
      "svelte/no-object-in-text-mustaches": "error",
      "svelte/no-shorthand-style-property-overrides": "error",
      "svelte/no-unknown-style-directive-property": "error",
      "svelte/no-unused-svelte-ignore": "error",
      "svelte/valid-compile": "error",
    },
  },
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
