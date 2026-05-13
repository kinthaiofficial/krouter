import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

const __dirname = dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  base: '/ui/',
  plugins: [react(), tailwindcss()],
  build: {
    outDir: resolve(__dirname, '../internal/webui/dist'),
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
