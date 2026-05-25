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
  build: {
    // Vendor split: per-route chunks come from React.lazy in App.tsx;
    // here we only carve up node_modules. React + router go in one
    // chunk (always needed), Radix primitives in another (heaviest
    // single group), uplot stays alone, everything else lands in a
    // generic vendor chunk.
    rolldownOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (id.includes('react-router')) return 'react'
          if (id.includes('/react/') || id.includes('/react-dom/') || id.includes('/scheduler/')) return 'react'
          if (id.includes('@radix-ui')) return 'radix'
          if (id.includes('uplot')) return 'charts'
          return 'vendor'
        },
      },
    },
  },
  server: {
    proxy: {
      // Dashboard listener (REST + SSE) — default :7060 with prefix
      // /dashboard/v1. Match the orchestrator's A_DASHBOARD_PORT and
      // A_DASHBOARD_PREFIX defaults; if you change them on the
      // backend, mirror here.
      '/dashboard': 'http://localhost:7060',
    }
  }
})
