import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { lazy, Suspense } from 'react'
import { ThemeProvider, useTheme } from '@/components/theme-provider'
import { Header } from '@/components/header'
import { LoadingBlock } from '@/components/loading-block'
import { Toaster } from 'sonner'

// Per-route code splitting: each page becomes its own chunk so the
// initial bundle only ships the route the user actually lands on.
// Vite emits one async chunk per import() below; the rest are fetched
// on navigation. LoadingBlock matches the in-page skeleton used while
// SSE warms up, so the swap is visually consistent.
const ClusterOverview = lazy(() => import('@/pages/cluster'))
const ClusterLogs = lazy(() => import('@/pages/cluster/logs'))
const Nodes = lazy(() => import('@/pages/cluster/nodes'))
const NodeOverview = lazy(() => import('@/pages/cluster/nodes/[nodeId]/overview'))
const NodeLogs = lazy(() => import('@/pages/cluster/nodes/[nodeId]/logs'))
const NodeAllocations = lazy(() => import('@/pages/cluster/nodes/[nodeId]/allocations'))
const AllocationOverview = lazy(() => import('@/pages/cluster/nodes/[nodeId]/allocations/[allocId]/overview'))
const AllocationLogs = lazy(() => import('@/pages/cluster/nodes/[nodeId]/allocations/[allocId]/logs'))

const Services = lazy(() => import('@/pages/services'))
const ServiceOverview = lazy(() => import('@/pages/services/[name]/overview'))
const ServiceAllocations = lazy(() => import('@/pages/services/[name]/allocations'))
const ServiceAutoscaler = lazy(() => import('@/pages/services/[name]/autoscaler'))
const ServiceDeploy = lazy(() => import('@/pages/services/[name]/deploy'))

function ThemedToaster() {
  const { theme } = useTheme()
  return <Toaster position="top-right" theme={theme} />
}

export default function App() {
  return (
    <ThemeProvider defaultTheme="system" storageKey="asty-theme">
      <BrowserRouter>
        {/* Shell layout: h-screen + overflow-hidden makes the body
            non-scrolling. The <main> below owns the scroll context.
            Pages with natural content (lists, dashboards) scroll
            inside main; pages with their own viewport sizing (logs)
            use h-full + overflow-hidden so main never overflows. */}
        <div className="flex h-screen flex-col overflow-hidden bg-linear-to-t from-muted to-muted/30">
          <Header />
          <main className="min-h-0 flex-1 overflow-y-auto">
            <Suspense fallback={<div className="p-4"><LoadingBlock /></div>}>
              <Routes>
                {/* Cluster section */}
                <Route path="/" element={<ClusterOverview />} />
                <Route path="/nodes" element={<Nodes />} />
                <Route path="/logs" element={<ClusterLogs />} />

                {/* Node section */}
                <Route path="/nodes/:nodeId" element={<NodeOverview />} />
                <Route path="/nodes/:nodeId/allocations" element={<NodeAllocations />} />
                <Route path="/nodes/:nodeId/logs" element={<NodeLogs />} />

                {/* Allocation section */}
                <Route path="/nodes/:nodeId/allocations/:allocId" element={<AllocationOverview />} />
                <Route path="/nodes/:nodeId/allocations/:allocId/logs" element={<AllocationLogs />} />

                {/* Services section */}
                <Route path="/services" element={<Services />} />
                <Route path="/services/:name" element={<ServiceOverview />} />
                <Route path="/services/:name/allocations" element={<ServiceAllocations />} />
                <Route path="/services/:name/autoscaler" element={<ServiceAutoscaler />} />
                <Route path="/services/:name/deploy" element={<ServiceDeploy />} />
              </Routes>
            </Suspense>
          </main>
        </div>
        <ThemedToaster />
      </BrowserRouter>
    </ThemeProvider>
  )
}
