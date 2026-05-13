import { useNavigate, useParams } from 'react-router-dom'
import { useState, useEffect, useMemo } from 'react'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { MetricsChart } from '@/components/metrics-chart'
import { Cpu, MemoryStick, Activity, Layers } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { useClusterStore } from '@/store/cluster'
import type { ScalingEvent } from '@/types'

export default function ServiceOverview() {
  const { name } = useParams<{ name: string }>()
  const navigate = useNavigate()
  const { serviceCache, subscribeService, services } = useClusterStore()
  const cached = name ? serviceCache[name] : undefined
  const service = cached?.service || null
  const allocations = cached?.allocations || []
  const cpuMetrics = cached?.cpuMetrics || []
  const memoryMetrics = cached?.memoryMetrics || []
  const allocCountMetrics = cached?.allocCountMetrics || []
  const [events, setEvents] = useState<ScalingEvent[]>([])

  // Autoscaler status is part of the global services SSE event (runtime fields).
  const autoscalerStatus = useMemo(() => {
    if (!name) return null
    return services.find((s) => s.Name === name) || null
  }, [name, services])

  // Subscribe to service detail SSE (definition + allocations + metrics)
  useEffect(() => {
    if (!name) return
    return subscribeService(name)
  }, [name, subscribeService])

  // Scaling events history is the only piece not in SSE — light polling.
  useEffect(() => {
    if (!name) return
    let timer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false

    const poll = async () => {
      try {
        const eventsRes = await api.getAutoscalerEvents(name, 50)
        if (!cancelled) setEvents(eventsRes.events || [])
      } catch { /* keep current */ }
      if (!cancelled) timer = setTimeout(poll, 15000)
    }

    poll()
    return () => { cancelled = true; if (timer) clearTimeout(timer) }
  }, [name])

  const runningCount = allocations.filter((a) => a.status === 'running').length

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink to="/services">Services</BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>{name}</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
        <div className="flex items-center gap-3">
          {service ? (
            <>
              <h1 className="text-2xl sm:text-3xl font-bold">{name}</h1>
              <Badge variant={service.Type === 'system' ? 'secondary' : 'default'}>
                {service.Type}
              </Badge>
            </>
          ) : (
            <>
              <Skeleton className="h-9 w-32" />
              <Skeleton className="h-6 w-16" />
            </>
          )}
        </div>
      </div>

      <div className="grid gap-4 grid-cols-2 sm:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Running</CardTitle>
            <Layers className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {service ? (
              <>
                <div className="text-2xl font-bold">{runningCount}</div>
                <p className="text-xs text-muted-foreground">
                  of {allocations.length} total
                </p>
              </>
            ) : (
              <>
                <Skeleton className="h-8 w-8 mb-2" />
                <Skeleton className="h-3 w-20" />
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">CPU Limit</CardTitle>
            <Cpu className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {service ? (
              <>
                <div className="text-2xl font-bold">{service.Resources.CPU}</div>
                <p className="text-xs text-muted-foreground">MHz per instance</p>
              </>
            ) : (
              <>
                <Skeleton className="h-8 w-16 mb-2" />
                <Skeleton className="h-3 w-24" />
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Memory Limit</CardTitle>
            <MemoryStick className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {service ? (
              <>
                <div className="text-2xl font-bold">{service.Resources.Memory}</div>
                <p className="text-xs text-muted-foreground">MB per instance</p>
              </>
            ) : (
              <>
                <Skeleton className="h-8 w-16 mb-2" />
                <Skeleton className="h-3 w-24" />
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Health Check</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {service ? (
              <>
                <div className="text-2xl font-bold">{service.Health.Type || 'none'}</div>
                {service.Health.Path && (
                  <p className="text-xs text-muted-foreground font-mono">{service.Health.Path}</p>
                )}
              </>
            ) : (
              <>
                <Skeleton className="h-8 w-16 mb-2" />
                <Skeleton className="h-3 w-24" />
              </>
            )}
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="metrics" className="space-y-4">
        <TabsList className="grid w-full grid-cols-3">
          <TabsTrigger value="metrics">Metrics</TabsTrigger>
          <TabsTrigger value="allocations">Allocations</TabsTrigger>
          <TabsTrigger value="autoscaler">Autoscaler</TabsTrigger>
        </TabsList>

        <TabsContent value="metrics" className="space-y-4">
          <div className="grid gap-4 md:grid-cols-2">
            <MetricsChart title="CPU Usage (aggregate)" data={cpuMetrics} color="hsl(var(--chart-1))" />
            <MetricsChart title="Memory Usage (aggregate)" data={memoryMetrics} color="hsl(var(--chart-2))" unit=" MB" />
          </div>
          <MetricsChart title="Running Allocations" data={allocCountMetrics} color="hsl(var(--chart-3))" unit="" />
        </TabsContent>

        <TabsContent value="allocations" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Allocations</CardTitle>
            </CardHeader>
            <CardContent className="overflow-x-auto">
              {allocations.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                  <Activity className="h-12 w-12 mb-4" />
                  <p>No allocations</p>
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>ID</TableHead>
                      <TableHead>Node</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Health</TableHead>
                      <TableHead>CPU</TableHead>
                      <TableHead>Memory</TableHead>
                      <TableHead>Restarts</TableHead>
                      <TableHead>Started</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {allocations.map((alloc) => (
                      <TableRow
                        key={alloc.id}
                        className="cursor-pointer hover:bg-muted/50"
                        onClick={() => navigate(`/nodes/${alloc.node_id}/alloc/${alloc.id}`)}
                      >
                        <TableCell className="font-mono text-xs">{alloc.id.slice(0, 8)}</TableCell>
                        <TableCell className="font-mono">{alloc.node_id}</TableCell>
                        <TableCell>
                          <Badge
                            variant={
                              alloc.status === 'running' ? 'success'
                                : alloc.status === 'failed' ? 'destructive'
                                : 'secondary'
                            }
                          >
                            {alloc.status}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant={
                              alloc.health_status === 'healthy' ? 'default'
                                : alloc.health_status === 'unhealthy' ? 'destructive'
                                : 'secondary'
                            }
                          >
                            {alloc.health_status}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <div>{alloc.cpu_usage}%</div>
                          {service && (
                            <div className="text-xs text-muted-foreground">
                              {Math.round((alloc.cpu_usage / 100) * service.Resources.CPU)} / {service.Resources.CPU} MHz
                            </div>
                          )}
                        </TableCell>
                        <TableCell>
                          <div>{service ? Math.round((alloc.memory_usage / service.Resources.Memory) * 100) : '?'}%</div>
                          {service && (
                            <div className="text-xs text-muted-foreground">
                              {alloc.memory_usage} / {service.Resources.Memory} MB
                            </div>
                          )}
                        </TableCell>
                        <TableCell>{alloc.restarts}</TableCell>
                        <TableCell className="text-sm">
                          {alloc.started_at
                            ? formatDistanceToNow(new Date(alloc.started_at), { addSuffix: true })
                            : '-'}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="autoscaler" className="space-y-4">
          {autoscalerStatus && (
            <Card>
              <CardHeader>
                <CardTitle>Autoscaler Configuration</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Current Copies</span>
                  <span className="font-bold">{autoscalerStatus.current_copies}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Min Copies</span>
                  <span>{autoscalerStatus.min_copies}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Target CPU</span>
                  <span>{autoscalerStatus.target_cpu}%</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Target Memory</span>
                  <span>{autoscalerStatus.target_memory}%</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Traffic Threshold</span>
                  <span>{autoscalerStatus.traffic_threshold} rps</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Cooldown</span>
                  <div className="flex gap-2">
                    {autoscalerStatus.cooldown_up_active && <Badge variant="secondary">up</Badge>}
                    {autoscalerStatus.cooldown_down_active && <Badge variant="secondary">down</Badge>}
                    {!autoscalerStatus.cooldown_up_active && !autoscalerStatus.cooldown_down_active && (
                      <span className="text-sm">inactive</span>
                    )}
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          <Card>
            <CardHeader>
              <CardTitle>Scaling Events</CardTitle>
            </CardHeader>
            <CardContent className="overflow-x-auto">
              {events.length === 0 ? (
                <div className="text-muted-foreground text-center py-8">
                  No scaling events yet
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Time</TableHead>
                      <TableHead>Action</TableHead>
                      <TableHead>Reason</TableHead>
                      <TableHead>Copies</TableHead>
                      <TableHead>Node</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {events.map((event, idx) => (
                      <TableRow key={idx}>
                        <TableCell className="text-sm">
                          {new Date(event.timestamp * 1000).toLocaleString()}
                        </TableCell>
                        <TableCell>
                          <Badge variant={event.action === 'scale_up' ? 'default' : 'secondary'}>
                            {event.action === 'scale_up' ? 'Scale Up' : 'Scale Down'}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-sm">{event.reason}</TableCell>
                        <TableCell>
                          {event.from_count} → {event.to_count}
                        </TableCell>
                        <TableCell className="font-mono text-xs">
                          {event.node_id || '-'}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
