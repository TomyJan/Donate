import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';
export default defineConfig({
  plugins: [react()],
  server: { proxy: { '/api': 'http://127.0.0.1:8080', '/health': 'http://127.0.0.1:8080' } },
  build: {
    outDir: '../internal/webassets/dist',
    emptyOutDir: true,
    // Preserve native light-dark(): transpiled CSS cannot observe React's inline color-scheme updates.
    cssTarget: ['chrome123', 'firefox120', 'safari17.5'],
  },
});
