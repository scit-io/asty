import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';

// Dev-proxy to the Gateway: the front-end uses relative paths (/api/v1, /health),
// and Vite forwards them to the Gateway. This avoids CORS and lets the
// browser send HttpOnly cookies (same-origin).
//
// The Asty dev environment runs the gateway on 127.0.0.1:80 (dev-node-1).
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const gatewayUrl = env.VITE_GATEWAY_URL || 'http://127.0.0.1';

  return {
    plugins: [react()],
    server: {
      port: 3000,
      proxy: {
        '/api/v1': {
          target: gatewayUrl,
          changeOrigin: true,
          ws: true,
        },
        '/health': {
          target: gatewayUrl,
          changeOrigin: true,
        },
      },
    },
  };
});
