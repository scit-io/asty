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
  // No dev proxy: the SPA calls the cluster directly on the absolute
  // origin from VITE_ASTY_ORIGIN (e.g. http://asty.test:7060), which
  // resolves to every node's loopback alias via /etc/hosts — so the
  // browser fails over across nodes at the DNS layer, exactly like the
  // multi-A-record cluster domain does in prod. The dashboard's CORS
  // layer permits the cross-origin call. See src/lib/backend.ts.
})
