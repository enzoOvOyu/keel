import {fileURLToPath, URL} from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { tanstackRouter } from '@tanstack/router-plugin/vite'

export default defineConfig({
  resolve: {alias: {"@": fileURLToPath(new URL("./src",import.meta.url))}},
  plugins: [tailwindcss(),tanstackRouter({ target: 'react', autoCodeSplitting: true }), react()],
  server: {
    host: '127.0.0.1', port: 5173, strictPort: true,
    proxy: {
      '/api': 'http://127.0.0.1:3001',
      '/healthz': 'http://127.0.0.1:3001',
    },
  },
  preview: { host: '127.0.0.1', port: 4173, strictPort: true },
})
