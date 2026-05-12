import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';

// Dev-proxy на Gateway: фронт обращается к относительным путям (/v1, /health),
// Vite форвардит их на Gateway. Это избавляет от CORS и позволяет браузеру
// отправлять HttpOnly-куки (same-origin).
//
// Asty dev-окружение запускает gateway на 127.0.0.1:80 (dev-node-1).
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const gatewayUrl = env.VITE_GATEWAY_URL || 'http://127.0.0.1';

  return {
    plugins: [react()],
    server: {
      port: 3000,
      proxy: {
        '/v1': {
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
