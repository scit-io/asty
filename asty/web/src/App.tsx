import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { lazy, Suspense, useEffect, useRef } from 'react'
import { ThemeProvider, useTheme } from '@/components/theme-provider'
import { LocaleProvider } from '@/lib/i18n'
import { Header } from '@/components/header'
import { Footer } from '@/components/footer'
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
  const shellRef = useRef<HTMLDivElement>(null)
  const mainRef = useRef<HTMLElement>(null)

  // Expose <main>'s live vertical-scrollbar width as --scrollbar-width on
  // the shell so the footer can dock just left of the scrollbar instead
  // of under it. 0 with overlay scrollbars (macOS default), ~15px with
  // classic ones; recomputed when content makes the bar appear or vanish.
  useEffect(() => {
    const main = mainRef.current
    const shell = shellRef.current
    if (!main || !shell) return
    const sync = () =>
      shell.style.setProperty('--scrollbar-width', `${main.offsetWidth - main.clientWidth}px`)
    sync()
    const ro = new ResizeObserver(sync)
    ro.observe(main)
    return () => ro.disconnect()
  }, [])

  return (
    <ThemeProvider defaultTheme="system" storageKey="asty-theme">
      <LocaleProvider storageKey="asty-locale">
      <BrowserRouter>
        {/* Shell layout: h-screen + overflow-hidden makes the body
            non-scrolling. The <main> below owns the scroll context.
            Pages with natural content (lists, dashboards) scroll
            inside main; pages with their own viewport sizing (logs)
            use h-full + overflow-hidden so main never overflows. */}
        <div ref={shellRef} className="relative flex h-screen flex-col overflow-hidden bg-linear-to-t from-muted to-muted/30">
          <Header />
          <main ref={mainRef} className="min-h-0 flex-1 overflow-y-auto">
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
          <Footer />
        </div>
        <ThemedToaster />
      </BrowserRouter>
      </LocaleProvider>
    </ThemeProvider>
  )
}
