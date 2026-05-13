import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: '/',
  build: {
    outDir: path.resolve(__dirname, '../internal/webui/installer/dist'),
    emptyOutDir: true,
  },
  server: {
    port: 5174,
    proxy: {
      '/api/install': 'http://127.0.0.1:8404',
      '/health': 'http://127.0.0.1:8404',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
  },
})
