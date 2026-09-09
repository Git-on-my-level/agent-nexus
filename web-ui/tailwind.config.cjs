/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./src/**/*.{html,js,svelte}"],
  theme: {
    colors: {
      bg: "var(--bg)",
      "bg-soft": "var(--bg-soft)",
      panel: "var(--panel)",
      "panel-hover": "var(--panel-hover)",
      line: "var(--line)",
      "line-subtle": "var(--line-subtle)",
      "line-strong": "var(--line-strong)",
      fg: "var(--fg)",
      "fg-muted": "var(--fg-muted)",
      "fg-subtle": "var(--fg-subtle)",
      accent: "var(--accent)",
      "accent-hover": "var(--accent-hover)",
      "accent-solid": "var(--accent-solid)",
      "accent-text": "var(--accent-text)",
      "accent-soft": "var(--accent-soft)",
      danger: "var(--danger)",
      "danger-text": "var(--danger-text)",
      "danger-soft": "var(--danger-soft)",
      warn: "var(--warn)",
      "warn-text": "var(--warn-text)",
      "warn-soft": "var(--warn-soft)",
      ok: "var(--ok)",
      "ok-text": "var(--ok-text)",
      "ok-soft": "var(--ok-soft)",
      info: "var(--info)",
      blue: {
        400: "#60a5fa",
      },
      // Decorative identity tints for deterministic avatars (avatarModel.js).
      // theme.colors replaces Tailwind's defaults, so these must be declared
      // explicitly or the avatar palette slots render with no color.
      sky: { 300: "#7dd3fc", 500: "#0ea5e9" },
      rose: { 300: "#fda4af", 500: "#f43f5e" },
      violet: { 300: "#c4b5fd", 500: "#8b5cf6" },
      teal: { 300: "#5eead4", 500: "#14b8a6" },
      fuchsia: { 300: "#f0abfc", 500: "#d946ef" },
      transparent: "transparent",
      current: "currentColor",
      white: "#fff",
      black: "#000",
    },
    // One type scale. Sizes carry no baked-in weight except the two heading
    // steps: a size token that also sets weight cannot be reused for a label
    // and a value, and every caller then fights it with `font-medium`.
    fontSize: {
      micro: ["11px", { lineHeight: "16px" }],
      meta: ["13px", { lineHeight: "18px" }],
      mono: ["12px", { lineHeight: "18px" }],
      body: ["15px", { lineHeight: "22px" }],
      subtitle: ["17px", { lineHeight: "24px", fontWeight: "600" }],
      title: ["20px", { lineHeight: "26px", fontWeight: "600" }],
      display: ["28px", { lineHeight: "34px", fontWeight: "600" }],
    },
    borderRadius: {
      sm: "4px",
      DEFAULT: "6px",
      md: "8px",
      lg: "10px",
      full: "9999px",
    },
  },
  plugins: [],
};
