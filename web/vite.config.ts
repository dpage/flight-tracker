import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Vite dev server proxies /api, /auth, /healthz to the Go server on :8080.
// In production the Go binary serves the built SPA directly, so the proxy is
// only used during `npm run dev`.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/auth': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: true,
    // Skip the "computing gzip size" step. It gzips the entire bundle in
    // memory just to print one number in the build log, and on small VPS
    // boxes (e.g. Hetzner CX11) the gzip buffer is enough to push Node
    // past available RAM and trip the OOM killer mid-build. Doesn't affect
    // the output — assets are served gzip-compressed at runtime regardless.
    reportCompressedSize: false,
  },
});
