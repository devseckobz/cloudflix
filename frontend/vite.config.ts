import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [tailwindcss(), svelte()],
  resolve: { alias: { $lib: fileURLToPath(new URL('./src/lib', import.meta.url)) } },
  build: { outDir: '../static/dist', emptyOutDir: true },
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:3000', changeOrigin: false }
    }
  }
})
