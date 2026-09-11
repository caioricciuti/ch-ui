import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'

// Backend the dev server proxies to. Defaults to a local `ch-ui server --dev`;
// point it at any running CH-UI (e.g. CHUI_DEV_PROXY=https://ch-ui.example.com)
// to develop the UI against real data without running a backend here.
const backend = process.env.CHUI_DEV_PROXY?.replace(/\/+$/, '') || 'http://127.0.0.1:3488'
const wsBackend = backend.replace(/^http/, 'ws')
const proxyOpts = { target: backend, changeOrigin: true, secure: false }

export default defineConfig({
  appType: 'spa',
  plugins: [svelte(), tailwindcss()],
  base: process.env.VITE_BASE_PATH ? process.env.VITE_BASE_PATH + '/' : '/',
  resolve: {
    dedupe: [
      '@codemirror/state',
      '@codemirror/view',
      '@codemirror/language',
      '@codemirror/autocomplete',
      '@codemirror/commands',
      '@codemirror/search',
      '@lezer/common',
      '@lezer/highlight',
      '@lezer/lr',
    ],
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': proxyOpts,
      '/connect': { target: wsBackend, ws: true, changeOrigin: true, secure: false },
      '/health': proxyOpts,
      '/install': proxyOpts,
      '/download': proxyOpts,
      '/mcp': proxyOpts,
      '/.well-known': proxyOpts,
      '/oauth/authorize': proxyOpts,
      '/oauth/token': proxyOpts,
      '/oauth/register': proxyOpts,
    },
  },
  preview: {
    host: '127.0.0.1',
  },
  build: {
    target: 'es2022',
    minify: process.env.CHUI_VITE_MINIFY !== '0',
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined
          if (id.includes('@codemirror') || id.includes('@lezer')) return 'codemirror'
          if (id.includes('lucide-svelte')) return 'icons'
          if (id.includes('uplot')) return 'charts'
          return 'vendor'
        },
      },
    },
  },
})
