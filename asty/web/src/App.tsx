import { useEffect } from 'react'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from '@/components/theme-provider'
import { Header } from '@/components/header'
import { Toaster } from 'sonner'
import { useClusterStore } from '@/store/cluster'
import Cluster from '@/pages/cluster'
import NodeDetail from '@/pages/node-detail'
import AllocationDetail from '@/pages/allocation-detail'
import AllocationLogs from '@/pages/allocation-logs'
import Services from '@/pages/services'
import ServiceOverview from '@/pages/service-overview'
import ServiceAllocations from '@/pages/service-allocations'
import ServiceAutoscaler from '@/pages/service-autoscaler'
import ServiceDeploy from '@/pages/service-deploy'
import Nodes from '@/pages/nodes'
import NodeAllocations from '@/pages/node-allocations'
import NodeLogs from '@/pages/node-logs'
import ClusterLogs from '@/pages/logs'

export default function App() {
  const initSSE = useClusterStore((s) => s.initSSE)

  useEffect(() => {
    return initSSE()
  }, [initSSE])

  return (
    <ThemeProvider defaultTheme="system" storageKey="astiui-theme">
      <BrowserRouter>
        <div className="min-h-screen bg-linear-to-t from-muted to-muted/30">
          <Header />
          <Routes>
            {/* Cluster section */}
            <Route path="/" element={<Cluster />} />
            <Route path="/nodes" element={<Nodes />} />
            <Route path="/logs" element={<ClusterLogs />} />

            {/* Node section */}
            <Route path="/nodes/:nodeId" element={<NodeDetail />} />
            <Route path="/nodes/:nodeId/allocations" element={<NodeAllocations />} />
            <Route path="/nodes/:nodeId/logs" element={<NodeLogs />} />

            {/* Allocation section (URL parent is /nodes/...) */}
            <Route path="/nodes/:nodeId/allocations/:allocId" element={<AllocationDetail />} />
            <Route path="/nodes/:nodeId/allocations/:allocId/logs" element={<AllocationLogs />} />

            {/* Services section */}
            <Route path="/services" element={<Services />} />
            <Route path="/services/:name" element={<ServiceOverview />} />
            <Route path="/services/:name/allocations" element={<ServiceAllocations />} />
            <Route path="/services/:name/autoscaler" element={<ServiceAutoscaler />} />
            <Route path="/services/:name/deploy" element={<ServiceDeploy />} />
          </Routes>
        </div>
        <Toaster position="top-right" />
      </BrowserRouter>
    </ThemeProvider>
  )
}
