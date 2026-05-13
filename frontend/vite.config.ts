import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig({
  base: '/ui/',
  plugins: [react(), tailwindcss()],
  build: {
    outDir: path.resolve(__dirname, '../internal/webui/dist'),
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/internal': 'http://127.0.0.1:8403',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
  },
})
