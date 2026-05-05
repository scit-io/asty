import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from '@/components/theme-provider'
import { Header } from '@/components/header'
import Dashboard from '@/pages/dashboard'
import NodeDetail from '@/pages/node-detail'
import ServiceDetail from '@/pages/service-detail'

export default function App() {
  return (
    <ThemeProvider defaultTheme="system" storageKey="astiui-theme">
      <BrowserRouter>
        <Header />
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/nodes/:nodeId" element={<NodeDetail />} />
          <Route path="/nodes/:nodeId/alloc/:allocId" element={<ServiceDetail />} />
        </Routes>
      </BrowserRouter>
    </ThemeProvider>
  )
}
