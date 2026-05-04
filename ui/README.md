# Astiui — Web UI for Asty Orchestrator

Modern dashboard for monitoring and managing Asty clusters, built with shadcn/ui.

## Features

- **Dashboard** — cluster overview with nodes table
- **Node Detail** — detailed node view with services, logs, and actions
- **Service Detail** — individual service allocation management
- **Dark/Light Theme** — automatic system detection with manual toggle
- **Real-time Updates** — auto-refresh every 5 seconds via React Query

## Tech Stack

- **React 19** + **TypeScript**
- **Vite** — build tool
- **shadcn/ui** — component library (Tailwind CSS + Radix UI)
- **TanStack Query** — data fetching and caching
- **React Router** — navigation
- **lucide-react** — icons
- **date-fns** — date formatting

## Development

```bash
# Install dependencies
pnpm install

# Start dev server (http://localhost:5173)
pnpm dev

# Build for production
pnpm build

# Preview production build
pnpm preview
```

## Navigation Structure

```
/                           → Dashboard
/nodes/:nodeId              → Node Detail
/nodes/:nodeId/alloc/:allocId → Service Detail
```

## API Integration

The UI connects to Asty API at `http://localhost:4747` (proxied via Vite config).

Required endpoints:
- `GET /api/v1/status` — cluster status
- `GET /api/v1/nodes` — list nodes
- `GET /api/v1/nodes/:id` — node detail
- `GET /api/v1/allocations?node_id=:id` — node allocations
- `GET /api/v1/allocations/:id` — allocation detail
- `GET /api/v1/logs/node/:id` — node logs
- `GET /api/v1/logs/allocation/:id` — allocation logs
- `POST /api/v1/nodes/:id/drain` — drain node
- `POST /api/v1/nodes/:id/pause` — pause node
- `POST /api/v1/allocations/:id/restart` — restart allocation
- `POST /api/v1/allocations/:id/stop` — stop allocation

## Component Library

All components are from shadcn/ui. To add new components:

```bash
pnpm dlx shadcn@latest add <component-name>
```

Available components: https://ui.shadcn.com/docs/components

## Theme

The UI supports light/dark theme with system preference detection. Theme is persisted to localStorage.

Colors are defined in `src/index.css` using CSS variables.

## Project Structure

```
src/
├── api/
│   └── client.ts         # API client
├── components/
│   ├── ui/               # shadcn/ui components
│   ├── theme-provider.tsx
│   └── theme-toggle.tsx
├── pages/
│   ├── dashboard.tsx
│   ├── node-detail.tsx
│   └── service-detail.tsx
├── types/
│   └── index.ts          # TypeScript types
├── lib/
│   └── utils.ts          # Utilities
├── App.tsx
├── main.tsx
└── index.css
```

## Building for Production

```bash
pnpm build
```

Output is in `dist/` directory. Serve with any static file server or embed in Asty binary.

## License

Part of Asty project.
