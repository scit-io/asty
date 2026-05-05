import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from '@/components/theme-provider'
import { Header } from '@/components/header'
import Dashboard from '@/pages/dashboard'
import NodeDetail from '@/pages/node-detail'
import ServiceDetail from '@/pages/service-detail'
import Services from '@/pages/services'
import ServiceOverview from '@/pages/service-overview'
import Deploy from '@/pages/deploy'

export default function App() {
  return (
    <ThemeProvider defaultTheme="system" storageKey="astiui-theme">
      <BrowserRouter>
        <div className="min-h-screen bg-gradient-to-t from-muted to-muted/30">
          <Header />
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/nodes/:nodeId" element={<NodeDetail />} />
            <Route path="/nodes/:nodeId/alloc/:allocId" element={<ServiceDetail />} />
            <Route path="/services" element={<Services />} />
            <Route path="/services/:name" element={<ServiceOverview />} />
            <Route path="/deploy" element={<Deploy />} />
          </Routes>
        </div>
      </BrowserRouter>
    </ThemeProvider>
  )
}
