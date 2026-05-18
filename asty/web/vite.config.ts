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
      // Dashboard listener (REST + SSE) — default :7060 with prefix
      // /dashboard/v1. Match the orchestrator's A_DASHBOARD_PORT and
      // A_DASHBOARD_PREFIX defaults; if you change them on the
      // backend, mirror here.
      '/dashboard': 'http://localhost:7060',
      // Prometheus exposition shares the same listener by default.
      '/metrics': 'http://localhost:7060',
      // Health probe.
      '/health': 'http://localhost:7060',
    }
  }
})
