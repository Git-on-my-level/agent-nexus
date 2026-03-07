/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./src/**/*.{html,js,svelte}"],
  theme: {
    extend: {
      colors: {
        gray: {
          50: "#0e1015",
          100: "#181a21",
          200: "#262a33",
          300: "#353a45",
          400: "#565b66",
          500: "#7d828e",
          600: "#9ca1ab",
          700: "#b4b9c2",
          800: "#d0d4db",
          900: "#e2e5eb",
          950: "#f0f2f5",
        },
      },
    },
  },
  plugins: [],
};
