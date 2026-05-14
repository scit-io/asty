import { useEffect } from 'react'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from '@/components/theme-provider'
import { Header } from '@/components/header'
import { Toaster } from 'sonner'
import { useClusterStore } from '@/store/cluster'
import Cluster from '@/pages/cluster'
import NodeDetail from '@/pages/node-detail'
import ServiceDetail from '@/pages/service-detail'
import Services from '@/pages/services'
import ServiceOverview from '@/pages/service-overview'
import Placeholder from '@/pages/placeholder'

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
            <Route path="/nodes" element={<Placeholder title="Nodes" phase="D.3" />} />
            <Route path="/logs" element={<Placeholder title="Cluster Logs" phase="D.3" />} />

            {/* Node section */}
            <Route path="/nodes/:nodeId" element={<NodeDetail />} />
            <Route path="/nodes/:nodeId/allocations" element={<Placeholder title="Node Allocations" phase="D.4" />} />
            <Route path="/nodes/:nodeId/logs" element={<Placeholder title="Node Logs" phase="D.4" />} />

            {/* Allocation section (URL parent is /nodes/...) */}
            <Route path="/nodes/:nodeId/allocations/:allocId" element={<ServiceDetail />} />
            <Route path="/nodes/:nodeId/allocations/:allocId/logs" element={<Placeholder title="Allocation Logs" phase="D.5" />} />

            {/* Services section */}
            <Route path="/services" element={<Services />} />
            <Route path="/services/:name" element={<ServiceOverview />} />
            <Route path="/services/:name/allocations" element={<Placeholder title="Service Allocations" phase="D.6" />} />
            <Route path="/services/:name/autoscaler" element={<Placeholder title="Service Autoscaler" phase="D.6" />} />
            <Route path="/services/:name/deploy" element={<Placeholder title="Service Deploy" phase="D.6" />} />
          </Routes>
        </div>
        <Toaster position="top-right" />
      </BrowserRouter>
    </ThemeProvider>
  )
}
