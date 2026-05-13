import { useNavigate } from 'react-router-dom'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Package, Activity } from 'lucide-react'
import { useClusterStore } from '@/store/cluster'

export default function Services() {
  const navigate = useNavigate()
  const services = useClusterStore((s) => s.services)

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
                  <TableHead>CPU (avg)</TableHead>
                  <TableHead>Memory (avg)</TableHead>
                  <TableHead>Health</TableHead>
                  <TableHead className="text-right">Autoscaler</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {services.map((svc) => {
                  const hasUsage = svc.current_copies !== undefined && svc.current_copies > 0
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
                      <TableCell>{svc.current_copies ?? '-'}</TableCell>
                      <TableCell>
                        {hasUsage ? (
                          <>
                            <div>{Math.round(svc.avg_cpu_percent ?? 0)}%</div>
                            <div className="text-xs text-muted-foreground">
                              {Math.round(svc.avg_cpu_mhz ?? 0)} / {svc.Resources.CPU} MHz
                            </div>
                          </>
                        ) : (
                          <div className="text-xs text-muted-foreground">
                            {svc.Resources.CPU} MHz
                          </div>
                        )}
                      </TableCell>
                      <TableCell>
                        {hasUsage ? (
                          <>
                            <div>{Math.round(svc.avg_memory_percent ?? 0)}%</div>
                            <div className="text-xs text-muted-foreground">
                              {Math.round(svc.avg_memory_mb ?? 0)} / {svc.Resources.Memory} MB
                            </div>
                          </>
                        ) : (
                          <div className="text-xs text-muted-foreground">
                            {svc.Resources.Memory} MB
                          </div>
                        )}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline">
                          {svc.Health.Type || 'none'}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        {svc.Type === 'service' && svc.min_copies !== undefined ? (
                          <div className="flex items-center gap-2 justify-end">
                            {(svc.cooldown_up_active || svc.cooldown_down_active) && (
                              <Badge variant="secondary">cooldown</Badge>
                            )}
                            <span className="text-sm text-muted-foreground">
                              min {svc.min_copies}
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
