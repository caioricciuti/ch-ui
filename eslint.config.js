import globals from "globals";
import pluginJs from "@eslint/js";
import tseslint from "typescript-eslint";
//import pluginReact from "eslint-plugin-react";


export default [
  pluginJs.configs.recommended,
  ...tseslint.configs.recommended,
  //pluginReact.configs.flat.recommended,
  {
    ignores: ["tailwind.config.js", ".prettierrc.js"],
    "rules": {
      "@typescript-eslint/triple-slash-reference": "off",
      "react/react-in-jsx-scope": "off",
      "react/jsx-uses-react": "off",
      "@typescript-eslint/no-explicit-any": 0,
      "@typescript-eslint/no-unused-vars": 1
    },
    "files": ["**/*.{js,mjs,cjs,ts,jsx,tsx}"],
    "languageOptions": { globals: globals.browser }
  },
];