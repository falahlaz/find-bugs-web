import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The Go server serves the production build; in dev, Vite proxies API calls to it.
const backend = process.env.BACKEND_URL ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(__dirname, 'src') },
  },
  server: {
    proxy: {
      '/api': backend,
      '/saml-login': backend,
      '/healthz': backend,
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
