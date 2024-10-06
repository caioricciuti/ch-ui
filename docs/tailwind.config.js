module.exports = {
  content: [
    "./src/components/**/*.{js,jsx,ts,tsx}",
    "./docusaurus.config.js",
    "./src/pages/**/*.{js,jsx,ts,tsx}",
  ],
  theme: {
    extend: {},
  },
  plugins: [],
  corePlugins: { preflight: false },
  darkMode: ["class", '[data-theme="dark"]'],
};
