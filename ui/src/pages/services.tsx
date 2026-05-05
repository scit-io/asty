import { useNavigate } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Package, Activity } from 'lucide-react'
import type { ServiceDefinition, AutoscalerServiceStatus } from '@/types'

export default function Services() {
  const navigate = useNavigate()
  const [services, setServices] = useState<ServiceDefinition[]>([])
  const [autoscalerStatus, setAutoscalerStatus] = useState<Record<string, AutoscalerServiceStatus>>({})

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false

    const fetchData = async () => {
      try {
        const [svcRes, asRes] = await Promise.all([
          api.getServices(),
          api.getAutoscalerStatus().catch(() => ({ services: {} })),
        ])
        if (cancelled) return
        setServices(svcRes.services || [])
        setAutoscalerStatus(asRes.services || {})
      } catch {
        // keep current state
      }
      if (!cancelled) timer = setTimeout(fetchData, 5000)
    }

    fetchData()
    return () => { cancelled = true; if (timer) clearTimeout(timer) }
  }, [])

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
      <Card>
        <CardHeader className="text-right">
          <CardTitle>Services</CardTitle>
        </CardHeader>
        <CardContent className="overflow-x-auto">
          {services.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <Package className="h-12 w-12 mb-4" />
              <p>No services loaded</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Service</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Copies</TableHead>
                  <TableHead>CPU Limit</TableHead>
                  <TableHead>Memory Limit</TableHead>
                  <TableHead>Health</TableHead>
                  <TableHead className="text-right">Autoscaler</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {services.map((svc) => {
                  const as = autoscalerStatus[svc.Name]
                  return (
                    <TableRow
                      key={svc.Name}
                      className="cursor-pointer hover:bg-muted/50"
                      onClick={() => navigate(`/services/${svc.Name}`)}
                    >
                      <TableCell className="font-medium">
                        <div className="flex items-center gap-2">
                          <Activity className="h-4 w-4 text-muted-foreground" />
                          {svc.Name}
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant={svc.Type === 'system' ? 'secondary' : 'default'}>
                          {svc.Type}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        {as ? as.current_copies : '-'}
                      </TableCell>
                      <TableCell>{svc.Resources.CPU} MHz</TableCell>
                      <TableCell>{svc.Resources.Memory} MB</TableCell>
                      <TableCell>
                        <Badge variant="outline">
                          {svc.Health.Type || 'none'}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        {svc.Type === 'service' && as ? (
                          <div className="flex items-center gap-2 justify-end">
                            {(as.cooldown_up_active || as.cooldown_down_active) && (
                              <Badge variant="secondary">cooldown</Badge>
                            )}
                            <span className="text-sm text-muted-foreground">
                              min {as.min_copies}
                            </span>
                          </div>
                        ) : (
                          <span className="text-sm text-muted-foreground">-</span>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
