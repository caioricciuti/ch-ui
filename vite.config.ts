import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";
import tailwindcss from "@tailwindcss/vite"
// version
import pkg from "./package.json";

// https://vitejs.dev/config/
export default defineConfig({
  base: process.env.VITE_BASE_PATH || '/',
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  define: {
    __CH_UI_VERSION__: JSON.stringify(pkg.version),
  },
  worker: {
    format: 'es',
  },
  optimizeDeps: {
    include: [
      'monaco-editor/esm/vs/editor/editor.worker'
    ]
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          monaco: ['monaco-editor']
        }
      }
    }
  },
});
