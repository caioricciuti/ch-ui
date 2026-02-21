import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  appType: 'spa',
  plugins: [svelte(), tailwindcss()],
  base: '/',
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:3488',
      '/connect': { target: 'ws://127.0.0.1:3488', ws: true },
      '/health': 'http://127.0.0.1:3488',
      '/install': 'http://127.0.0.1:3488',
      '/download': 'http://127.0.0.1:3488',
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
