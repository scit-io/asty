import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { install } from '@asty-web-app/compass'
import './index.css'

// compass picks the lowest-latency Asty origin and wires its `fetch`
// and `EventSource` exports for transparent failover. Prod relies on
// the library defaults (bootstrap='/api/v1', fallback=location.origin,
// https + no port); dev needs the overrides below because the gateway
// answers over http on a non-standard port via a /etc/hosts name.
const env = import.meta.env
await install({
  bootstrapUrl: env.VITE_ASTY_BOOTSTRAP_URL,
  fallbackOrigin: env.VITE_ASTY_ORIGIN,
  scheme: env.VITE_ASTY_SCHEME,
  port: env.VITE_ASTY_PORT ? Number(env.VITE_ASTY_PORT) : undefined,
  debug: env.VITE_COMPASS_DEBUG === 'true',
})

// Dynamic import — App's module graph evaluates AFTER install resolves.
const { default: App } = await import('./App.tsx')

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
