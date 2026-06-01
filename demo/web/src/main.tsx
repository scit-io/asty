import React from 'react';
import ReactDOM from 'react-dom/client';
import { install } from '@asty-web-app/compass';
import './App.css';

const env = import.meta.env;
await install({
  bootstrapUrl: env.VITE_ASTY_BOOTSTRAP_URL,
  fallbackOrigin: env.VITE_GATEWAY_URL,
  scheme: env.VITE_ASTY_SCHEME,
  port: env.VITE_ASTY_PORT ? Number(env.VITE_ASTY_PORT) : undefined,
  debug: env.VITE_COMPASS_DEBUG === 'true',
});

const { default: App } = await import('./App');
ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
