import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from '@/components/theme-provider'
import { Header } from '@/components/header'
import { Toaster } from 'sonner'

import ClusterOverview from '@/pages/cluster/overview'
import ClusterLogs from '@/pages/cluster/logs'
import Nodes from '@/pages/cluster/nodes'
import NodeOverview from '@/pages/cluster/nodes/[nodeId]/overview'
import NodeLogs from '@/pages/cluster/nodes/[nodeId]/logs'
import NodeAllocations from '@/pages/cluster/nodes/[nodeId]/allocations'
import AllocationOverview from '@/pages/cluster/nodes/[nodeId]/allocations/[allocId]/overview'
import AllocationLogs from '@/pages/cluster/nodes/[nodeId]/allocations/[allocId]/logs'

import Services from '@/pages/services'
import ServiceOverview from '@/pages/services/[name]/overview'
import ServiceAllocations from '@/pages/services/[name]/allocations'
import ServiceAutoscaler from '@/pages/services/[name]/autoscaler'
import ServiceDeploy from '@/pages/services/[name]/deploy'

export default function App() {
  return (
    <ThemeProvider defaultTheme="system" storageKey="astiui-theme">
      <BrowserRouter>
        <div className="min-h-screen bg-linear-to-t from-muted to-muted/30">
          <Header />
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
        </div>
        <Toaster position="top-right" />
      </BrowserRouter>
    </ThemeProvider>
  )
}
