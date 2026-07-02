import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// Build output goes into the Go embed directory so `go build` picks it
// up via //go:embed. Keep the outDir aligned with internal/panel/frontend.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: '../internal/panel/frontend/dist',
    emptyOutDir: true,
    // Assets need to work under an arbitrary URL prefix (the panel picks
    // a random one at install time). Using relative base + a runtime
    // <base href="..."> in index.html lets the same bundle serve at any
    // depth without a rebuild.
    assetsDir: 'assets',
  },
  base: './',
  server: {
    // Dev proxy so `pnpm dev` on :5173 can hit the running Go panel
    // on :2053 for /api and /login. Adjust PANEL_URL when needed.
    proxy: {
      '/api': process.env.PANEL_URL ?? 'http://localhost:2053',
      '/login': process.env.PANEL_URL ?? 'http://localhost:2053',
      '/logout': process.env.PANEL_URL ?? 'http://localhost:2053',
      '/sub': process.env.PANEL_URL ?? 'http://localhost:2053',
    },
  },
})
