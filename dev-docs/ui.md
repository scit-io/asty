# UI — Web UI for Asty

## Overview

Complete rewrite of Asty Web UI using **shadcn/ui** component library. Focus on proper UX with hierarchical navigation: Dashboard → Node Detail → Service Detail.

**Key principles:**
- Use only shadcn/ui library components (no custom components)
- Ready dashboard blocks and layouts from shadcn/ui
- Built-in charts (recharts-based)
- Light/Dark theme with toggle
- Default color scheme from shadcn/ui

## Technology Stack

- **Framework:** Vite + React 18 + TypeScript
- **UI Library:** shadcn/ui (CLI-based copy-paste components)
- **Charts:** shadcn/ui charts (built on recharts)
- **Routing:** React Router v6
- **Data Fetching:** TanStack Query (React Query)
- **Styling:** Tailwind CSS (via shadcn/ui)
- **Icons:** lucide-react
- **Theme:** Custom ThemeProvider (light/dark/system)

## Navigation Structure

```
/                                → Dashboard (cluster overview + nodes table)
/nodes/:nodeId                   → Node Detail (tabs: Overview, Services, Logs, Actions)
/nodes/:nodeId/alloc/:allocId    → Service Detail in Node (tabs: Overview, Health, Logs, Actions)
```

### Page Hierarchy

1. **Dashboard** — cluster health + nodes table with clickable rows
2. **Node Detail** — detailed view of single node, list of services running on it (clickable)
3. **Service Detail** — detailed view of single allocation (service instance on specific node)

## Installation & Setup

### 1. Project Initialization

```bash
cd /Volumes/SSD/dev/UPWAY\ LC/up.mt/asty
pnpm create vite@latest ui -- --template react-ts
cd ui
pnpm install
```

### 2. Install Dependencies

```bash
# Tailwind CSS v3 (v4 not compatible with shadcn/ui CSS variables)
pnpm add -D tailwindcss@3.4.17 postcss@8.4.49 autoprefixer@10.4.20 @types/node

# React and utilities
pnpm add react react-dom
pnpm add -D @types/react @types/react-dom

# Core dependencies
pnpm add react-router-dom @tanstack/react-query date-fns lucide-react
pnpm add class-variance-authority clsx tailwind-merge

# Vite plugin
pnpm add -D @vitejs/plugin-react tailwindcss-animate
```

### 3. Manual Configuration

Create configuration files (shadcn/ui init doesn't work with custom Vite setup):

- `tsconfig.json` — add path aliases and `ignoreDeprecations: "6.0"`
- `vite.config.ts` — add path resolver and API proxy
- `components.json` — shadcn/ui configuration
- `tailwind.config.js` — Tailwind configuration
- `postcss.config.js` — PostCSS with Tailwind and Autoprefixer
- `src/index.css` — Tailwind directives + CSS variables
- `src/lib/utils.ts` — cn() utility

(See project structure below for complete file contents)

### 4. Add shadcn/ui Components

After configuration is complete:

```bash
pnpm dlx shadcn@latest add card table badge button tabs alert progress skeleton chart
```

### 5. Dark Mode Setup

Create `src/components/theme-provider.tsx`:

```tsx
import { createContext, useContext, useEffect, useState } from "react"

type Theme = "dark" | "light" | "system"

type ThemeProviderProps = {
  children: React.ReactNode
  defaultTheme?: Theme
  storageKey?: string
}

type ThemeProviderState = {
  theme: Theme
  setTheme: (theme: Theme) => void
}

const ThemeProviderContext = createContext<ThemeProviderState | undefined>(undefined)

export function ThemeProvider({
  children,
  defaultTheme = "system",
  storageKey = "ui-theme",
  ...props
}: ThemeProviderProps) {
  const [theme, setTheme] = useState<Theme>(
    () => (localStorage.getItem(storageKey) as Theme) || defaultTheme
  )

  useEffect(() => {
    const root = window.document.documentElement
    root.classList.remove("light", "dark")

    if (theme === "system") {
      const systemTheme = window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light"
      root.classList.add(systemTheme)
      return
    }

    root.classList.add(theme)
  }, [theme])

  const value = {
    theme,
    setTheme: (theme: Theme) => {
      localStorage.setItem(storageKey, theme)
      setTheme(theme)
    },
  }

  return (
    <ThemeProviderContext.Provider {...props} value={value}>
      {children}
    </ThemeProviderContext.Provider>
  )
}

export const useTheme = () => {
  const context = useContext(ThemeProviderContext)
  if (context === undefined)
    throw new Error("useTheme must be used within a ThemeProvider")
  return context
}
```

Add theme toggle button component `src/components/theme-toggle.tsx`:

```tsx
import { Moon, Sun } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useTheme } from "@/components/theme-provider"

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()

  return (
    <Button
      variant="ghost"
      size="icon"
      onClick={() => setTheme(theme === "light" ? "dark" : "light")}
    >
      <Sun className="h-5 w-5 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
      <Moon className="absolute h-5 w-5 rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100" />
      <span className="sr-only">Toggle theme</span>
    </Button>
  )
}
```

Wrap app in `src/main.tsx`:

```tsx
import { ThemeProvider } from "@/components/theme-provider"

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ThemeProvider defaultTheme="system" storageKey="ui-theme">
      <App />
    </ThemeProvider>
  </React.StrictMode>
)
```

## Project Structure

```
ui/
├── src/
│   ├── components/
│   │   ├── ui/              # shadcn/ui components (auto-generated)
│   │   ├── theme-provider.tsx
│   │   └── theme-toggle.tsx
│   ├── pages/
│   │   ├── dashboard.tsx    # Main dashboard
│   │   ├── node-detail.tsx  # Node detail page
│   │   └── service-detail.tsx # Service detail page
│   ├── api/
│   │   └── client.ts        # API client
│   ├── types/
│   │   └── index.ts         # TypeScript types
│   ├── lib/
│   │   └── utils.ts         # shadcn/ui utils
│   ├── App.tsx
│   ├── main.tsx
│   └── index.css
├── package.json
├── vite.config.ts
├── tailwind.config.js
└── tsconfig.json
```

## API Client

Reuse existing API logic from `./ui/src/api/client.ts`:

```typescript
// src/api/client.ts
const API_BASE = '/api/v1'

async function fetchJSON<T>(url: string): Promise<T> {
  const response = await fetch(url)
  if (!response.ok) throw new Error(`HTTP ${response.status}`)
  return response.json()
}

export const api = {
  // Existing
  getStatus: () => fetchJSON<ClusterStatus>(`${API_BASE}/status`),
  getNodes: () => fetchJSON<NodesResponse>(`${API_BASE}/nodes`),
  getServices: () => fetchJSON<ServicesResponse>(`${API_BASE}/services`),
  
  // New
  getNode: (id: string) => fetchJSON<NodeDetail>(`${API_BASE}/nodes/${id}`),
  getNodeAllocations: (id: string) => fetchJSON<AllocationsResponse>(`${API_BASE}/nodes/${id}/allocations`),
  getAllocation: (id: string) => fetchJSON<AllocationDetail>(`${API_BASE}/allocations/${id}`),
  getAllocationLogs: (id: string) => fetchJSON<LogsResponse>(`${API_BASE}/logs/allocation/${id}`),
  getNodeLogs: (id: string) => fetchJSON<LogsResponse>(`${API_BASE}/logs/node/${id}`),
  
  // Actions
  drainNode: (id: string) => fetchJSON(`${API_BASE}/nodes/${id}/drain`, { method: 'POST' }),
  pauseNode: (id: string) => fetchJSON(`${API_BASE}/nodes/${id}/pause`, { method: 'POST' }),
  restartAllocation: (id: string) => fetchJSON(`${API_BASE}/allocations/${id}/restart`, { method: 'POST' }),
  stopAllocation: (id: string) => fetchJSON(`${API_BASE}/allocations/${id}/stop`, { method: 'POST' }),
}
```

## TypeScript Types

Extend existing types from `./ui/src/types/index.ts`:

```typescript
// src/types/index.ts
export interface Node {
  id: string
  datacenter: string
  status: 'ready' | 'down' | 'initializing'
  cpu_total: number
  cpu_available: number
  memory_total: number
  memory_available: number
  processes: string[]
  last_seen: string
}

export interface NodeDetail extends Node {
  uptime: number
  metrics?: {
    cpu: MetricPoint[]
    memory: MetricPoint[]
  }
}

export interface Allocation {
  id: string
  service: string
  version: string
  node_id: string
  status: 'pending' | 'running' | 'stopping' | 'stopped' | 'failed'
  pid: number
  started_at: string
  health_status: 'healthy' | 'unhealthy' | 'unknown'
  cpu_usage: number
  memory_usage: number
  restarts: number
}

export interface AllocationDetail extends Allocation {
  logs?: string[]
  metrics?: {
    cpu: MetricPoint[]
    memory: MetricPoint[]
  }
}

export interface MetricPoint {
  timestamp: number
  value: number
}

export interface ClusterStatus {
  cluster: {
    leader: string
    is_leader: boolean
    nodes_total: number
    nodes_healthy: number
  }
  services: {
    loaded: number
  }
}

export interface ServiceDefinition {
  Name: string
  Type: 'system' | 'service'
  Resources: {
    CPU: number
    Memory: number
  }
  Health: {
    Type: string
    Path: string
    Interval: string
    Timeout: string
  }
}
```

## Pages Specification

### 1. Dashboard (`/`)

**Purpose:** Cluster overview with nodes table

**Layout:**
- Top: 3 cards with cluster stats (Total Nodes, Healthy Nodes, Services)
- Bottom: Nodes table (clickable rows)

**Components:**
- `Card` for stats
- `Badge` for status indicators
- `Table` for nodes list
- Area charts for cluster CPU/Memory (shadcn/ui chart component)

**Data:**
- `GET /api/v1/status` — cluster stats
- `GET /api/v1/nodes` — nodes list
- Auto-refresh every 5 seconds

**Table Columns:**
- Node ID (with icon)
- Datacenter
- Status (badge: ready=default, down=destructive, initializing=secondary)
- CPU % (progress bar + text)
- Memory % (progress bar + text)
- Services Count
- Last Seen (relative time)

**Action:** Click row → navigate to `/nodes/:nodeId`

### 2. Node Detail (`/nodes/:nodeId`)

**Purpose:** Detailed view of single node

**Layout:**
- Header: Node ID, Datacenter, Status badge, "Back to Dashboard" button, Theme Toggle
- Tabs: Overview | Services | Logs | Actions

**Tab 1: Overview**
- 4 cards: CPU Usage, Memory Usage, Uptime, Status
- Area charts: CPU over time, Memory over time (shadcn/ui charts)
- Data: `GET /api/v1/nodes/:id`

**Tab 2: Services**
- Table: Service Name, Status, CPU Used, Memory Used, Health, Actions
- Columns:
  - Service Name (clickable → `/nodes/:nodeId/alloc/:allocId`)
  - Status (badge)
  - CPU (progress bar)
  - Memory (progress bar)
  - Health (badge: healthy=default, unhealthy=destructive)
  - Quick Actions (restart/stop buttons)
- Data: `GET /api/v1/nodes/:id/allocations`

**Tab 3: Logs**
- Scrollable log viewer
- Monospace font
- Auto-refresh toggle
- Data: `GET /api/v1/logs/node/:id`

**Tab 4: Actions**
- Buttons: Drain Node, Pause Node, Resume Node
- Each button has confirmation dialog (shadcn/ui alert-dialog)
- Actions: `POST /api/v1/nodes/:id/drain`, etc.

### 3. Service Detail (`/nodes/:nodeId/alloc/:allocId`)

**Purpose:** Detailed view of single service allocation

**Layout:**
- Header: Service Name, Allocation ID, Node ID (link back), Status badge, Theme Toggle
- Tabs: Overview | Health | Logs | Actions

**Tab 1: Overview**
- Cards: Service Type, CPU Limit, Memory Limit, Uptime, Version, Restarts
- Metrics: CPU/Memory usage over time (area charts)
- Data: `GET /api/v1/allocations/:id`

**Tab 2: Health**
- Health check status (card)
- Last check time, interval, timeout
- Health endpoint path
- Data: from allocation detail response

**Tab 3: Logs**
- Scrollable log viewer (same as node logs)
- Monospace font
- Auto-refresh toggle
- Data: `GET /api/v1/logs/allocation/:id`

**Tab 4: Actions**
- Buttons: Restart, Stop, Start
- `variant="destructive"` for Stop button
- Confirmation dialogs
- Actions: `POST /api/v1/allocations/:id/restart`, etc.

## Component Usage Examples

### Stats Cards (Dashboard)

```tsx
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Server, CheckCircle, Package } from "lucide-react"

<div className="grid gap-4 md:grid-cols-3">
  <Card>
    <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
      <CardTitle className="text-sm font-medium">Total Nodes</CardTitle>
      <Server className="h-4 w-4 text-muted-foreground" />
    </CardHeader>
    <CardContent>
      <div className="text-2xl font-bold">{data.cluster.nodes_total}</div>
    </CardContent>
  </Card>
  {/* ... more cards */}
</div>
```

### Nodes Table (Dashboard)

```tsx
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"

<Table>
  <TableHeader>
    <TableRow>
      <TableHead>Node ID</TableHead>
      <TableHead>Datacenter</TableHead>
      <TableHead>Status</TableHead>
      <TableHead>CPU</TableHead>
      <TableHead>Memory</TableHead>
      <TableHead>Services</TableHead>
      <TableHead>Last Seen</TableHead>
    </TableRow>
  </TableHeader>
  <TableBody>
    {nodes.map((node) => (
      <TableRow key={node.id} onClick={() => navigate(`/nodes/${node.id}`)} className="cursor-pointer">
        <TableCell className="font-mono">{node.id}</TableCell>
        <TableCell>{node.datacenter}</TableCell>
        <TableCell>
          <Badge variant={node.status === 'ready' ? 'default' : 'destructive'}>
            {node.status}
          </Badge>
        </TableCell>
        {/* ... more cells */}
      </TableRow>
    ))}
  </TableBody>
</Table>
```

### Tabs (Node Detail)

```tsx
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

<Tabs defaultValue="overview">
  <TabsList className="grid w-full grid-cols-4">
    <TabsTrigger value="overview">Overview</TabsTrigger>
    <TabsTrigger value="services">Services</TabsTrigger>
    <TabsTrigger value="logs">Logs</TabsTrigger>
    <TabsTrigger value="actions">Actions</TabsTrigger>
  </TabsList>
  <TabsContent value="overview">
    {/* Overview content */}
  </TabsContent>
  {/* ... more tabs */}
</Tabs>
```

### Charts (shadcn/ui)

```tsx
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"

<Card>
  <CardHeader>
    <CardTitle>CPU Usage</CardTitle>
  </CardHeader>
  <CardContent>
    <ChartContainer config={{ cpu: { label: "CPU %", color: "hsl(var(--primary))" } }}>
      <AreaChart data={metrics.cpu}>
        <CartesianGrid strokeDasharray="3 3" />
        <XAxis dataKey="timestamp" tickFormatter={(ts) => new Date(ts * 1000).toLocaleTimeString()} />
        <ChartTooltip content={<ChartTooltipContent />} />
        <Area type="monotone" dataKey="value" stroke="var(--color-cpu)" fill="var(--color-cpu)" fillOpacity={0.2} />
      </AreaChart>
    </ChartContainer>
  </CardContent>
</Card>
```

## Backend API Extensions

Add these endpoints to `internal/platform/asty/api.go`:

### New Endpoints Required

```go
// GET /api/v1/nodes/:id — node detail
func (api *API) handleNodeDetail(w http.ResponseWriter, r *http.Request) { ... }

// GET /api/v1/nodes/:id/allocations — allocations on specific node
func (api *API) handleNodeAllocations(w http.ResponseWriter, r *http.Request) { ... }

// GET /api/v1/allocations/:id — allocation detail
func (api *API) handleAllocationDetail(w http.ResponseWriter, r *http.Request) { ... }

// GET /api/v1/logs/node/:id — node logs
func (api *API) handleNodeLogs(w http.ResponseWriter, r *http.Request) { ... }

// POST /api/v1/nodes/:id/drain — drain node
func (api *API) handleNodeDrain(w http.ResponseWriter, r *http.Request) { ... }

// POST /api/v1/nodes/:id/pause — pause node
func (api *API) handleNodePause(w http.ResponseWriter, r *http.Request) { ... }
```

## Development Workflow

1. **Dev server:**
   ```bash
   cd ui
   pnpm dev
   ```

2. **Vite proxy config** (`vite.config.ts`):
   ```ts
   export default defineConfig({
     server: {
       proxy: {
         '/api': 'http://localhost:4747',
         '/health': 'http://localhost:4747',
       }
     }
   })
   ```

3. **Build:**
   ```bash
   pnpm build  # → dist/
   ```

4. **Serve from Asty:** Update `ui.go` to serve from `ui/dist/`

## Implementation Phases

### Phase 1: Project Setup (Day 1)
- Create Vite project
- Install shadcn/ui
- Add all required components
- Setup dark mode
- Configure routing
- Create API client

### Phase 2: Dashboard (Day 2)
- Cluster stats cards
- Nodes table
- Navigation to node detail
- Auto-refresh with React Query

### Phase 3: Node Detail (Day 3)
- Overview tab with metrics cards
- Services tab with allocations table
- Logs tab with viewer
- Actions tab with buttons

### Phase 4: Service Detail (Day 4)
- Overview tab with allocation info
- Health tab
- Logs tab
- Actions tab

### Phase 5: Backend API (Day 5)
- Add missing endpoints
- Test all API integrations
- Handle errors properly

### Phase 6: Polish (Day 6)
- Loading states
- Error boundaries
- Confirmation dialogs
- Responsive design
- Test dark/light themes

## Notes

- **No custom components** — only shadcn/ui library components
- **No CSS-in-JS** — only Tailwind via shadcn/ui
- **No external chart libraries** — use shadcn/ui charts (recharts wrapper)
- **Color scheme** — default from shadcn/ui (slate base color)
- **Icons** — lucide-react (comes with shadcn/ui)
- **Mobile** — responsive by default (Tailwind breakpoints)

## Reference Links

- shadcn/ui docs: https://ui.shadcn.com/docs
- Vite installation: https://ui.shadcn.com/docs/installation/vite
- Chart components: https://ui.shadcn.com/charts
- Dashboard blocks: https://ui.shadcn.com/blocks
- Dark mode: https://ui.shadcn.com/docs/dark-mode/vite
