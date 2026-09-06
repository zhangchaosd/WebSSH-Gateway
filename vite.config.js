import { defineConfig } from 'vite'

export default defineConfig({
  build: {
    outDir: 'internal/httpapi/dist',
    emptyOutDir: true,
    target: 'es2020'
  }
})
