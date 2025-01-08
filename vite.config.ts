import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";
// version
import pkg from "./package.json";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  define: {
    __CH_UI_VERSION__: JSON.stringify(pkg.version),
  },
});
