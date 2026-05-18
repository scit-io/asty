import path from "path"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

export default defineConfig({
  plugins: [react()],
  base: './',
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      // /metrics is the Prometheus exposition path; SPA does not call
      // it, but proxy it through so local browsing of /metrics works.
      '/metrics': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    }
  }
})
