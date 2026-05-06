import { useNavigate, useParams } from 'react-router-dom'
import { useState, useEffect, useRef } from 'react'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { MetricsChart } from '@/components/metrics-chart'
import { Cpu, MemoryStick, Clock, Activity, HelpCircle, Wrench, FileText } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { toast } from 'sonner'
import type { MetricPoint } from '@/types'

interface Node {
  id: string
  ip: string
  datacenter: string
  status: string
  cpu_total: number
  cpu_available: number
  memory_total: number
  memory_available: number
  allocations_running: number
  allocations_planned: number
  created_at: string
}

interface Allocation {
  id: string
  service_name: string
  node_id: string
  status: string
  health_status: string
  cpu_usage: number
  memory_usage: number
  restarts: number
}

interface ServiceResources {
  CPU: number
  Memory: number
}

export default function NodeDetail() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const navigate = useNavigate()
  const [node, setNode] = useState<Node | null>(null)
  const [allocations, setAllocations] = useState<Allocation[]>([])
  const [services, setServices] = useState<Map<string, ServiceResources>>(new Map())
  const [cpuMetrics, setCpuMetrics] = useState<MetricPoint[]>([])
  const [memoryMetrics, setMemoryMetrics] = useState<MetricPoint[]>([])
  const [rpsMetrics, setRpsMetrics] = useState<MetricPoint[]>([])
  const [error, setError] = useState<string | null>(null)
  const [showDrainDialog, setShowDrainDialog] = useState(false)
  const [logLines, setLogLines] = useState<string[]>([])
  const isStreamingRef = useRef(false)
  const eventSourceRef = useRef<EventSource | null>(null)
  const logsEndRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!nodeId) return
    let timer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false

    const fetchData = async () => {
      try {
        let nodeData: Node
        try {
          nodeData = await api.getNode(nodeId)
        } catch {
          const nodesRes = await api.getNodes()
          const found = nodesRes.nodes.find(n => n.id === nodeId)
          if (found) {
            nodeData = found as Node
          } else {
            if (!cancelled) setError('Node not found')
            return
          }
        }
        if (cancelled) return
        setNode(nodeData)
        setError(null)

        const [allocsRes, metricsRes, servicesRes] = await Promise.all([
          api.getNodeAllocations(nodeId).catch(() => ({ allocations: [] })),
          api.getNodeMetrics(nodeId).catch(() => ({ cpu: [], memory: [], rps: [], period: '1h' })),
          api.getServices().catch(() => ({ services: [], count: 0 })),
        ])
        if (!cancelled) {
          setAllocations((allocsRes as { allocations: Allocation[] }).allocations || [])
          setCpuMetrics(metricsRes.cpu || [])
          setMemoryMetrics(metricsRes.memory || [])

          const svcMap = new Map<string, ServiceResources>()
          servicesRes.services.forEach(svc => {
            svcMap.set(svc.Name, { CPU: svc.Resources.CPU, Memory: svc.Resources.Memory })
          })
          setServices(svcMap)
          setRpsMetrics(metricsRes.rps || [])
        }
      } catch {
        // keep current state on error
      }
      if (!cancelled) timer = setTimeout(fetchData, 5000)
    }

    fetchData()
    return () => { cancelled = true; if (timer) clearTimeout(timer) }
  }, [nodeId])

  // SSE for drain progress
  useEffect(() => {
    if (!nodeId) return
    const eventSource = new EventSource('/api/v1/stream')
    eventSource.addEventListener('drain_progress', (event) => {
      try {
        const data = JSON.parse(event.data)
        if (data.node_id !== nodeId) return

        if (data.status === 'draining') {
          toast.loading('Draining node...', {
            id: `drain-${nodeId}`,
            description: `Migrated ${data.migrated}/${data.total_allocations} allocations`,
          })
          setNode(prev => prev ? { ...prev, status: 'draining' } : prev)
        } else if (data.status === 'drained') {
          toast.success('Node drained', {
            id: `drain-${nodeId}`,
            description: 'All allocations migrated successfully',
          })
          setNode(prev => prev ? { ...prev, status: 'drained' } : prev)
        }
      } catch { /* ignore */ }
    })
    return () => eventSource.close()
  }, [nodeId])

  // Log streaming
  useEffect(() => {
    if (!nodeId) return
    let retryCount = 0
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false

    const startStreaming = () => {
      if (cancelled) return
      const eventSource = new EventSource(`/api/v1/logs/node/${nodeId}?follow=true&lines=100`)

      eventSource.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)
          setLogLines((prev) => [...prev, data.line])
          setTimeout(() => logsEndRef.current?.scrollIntoView({ behavior: 'smooth' }), 100)
        } catch { /* ignore */ }
      }

      eventSource.onerror = () => {
        eventSource.close()
        isStreamingRef.current = false
        if (cancelled) return
        retryCount++
        retryTimer = setTimeout(startStreaming, Math.min(5000 * Math.pow(2, retryCount - 1), 60000))
      }

      eventSource.onopen = () => {
        retryCount = 0
        isStreamingRef.current = true
      }

      eventSourceRef.current = eventSource
    }

    startStreaming()
    return () => {
      cancelled = true
      if (retryTimer) clearTimeout(retryTimer)
      eventSourceRef.current?.close()
      eventSourceRef.current = null
    }
  }, [nodeId])

  const handleDrainToggle = async (enable: boolean) => {
    if (!nodeId) return
    try {
      const result = await api.drainNode(nodeId, enable) as { status: string; total_allocations?: number }
      if (enable) {
        setNode(prev => prev ? { ...prev, status: 'draining' } : prev)
        toast.loading('Draining node...', {
          id: `drain-${nodeId}`,
          description: `Migrating ${result.total_allocations || 0} allocations`,
        })
      } else {
        setNode(prev => prev ? { ...prev, status: 'ready' } : prev)
        toast.dismiss(`drain-${nodeId}`)
        toast.success('Node resumed', { description: 'Node is ready for allocations' })
      }
    } catch (err) {
      toast.error('Drain failed', {
        id: `drain-${nodeId}`,
        description: err instanceof Error ? err.message : 'Unknown error',
      })
    }
  }

  const handleSwitchChange = (checked: boolean) => {
    if (checked) {
      setShowDrainDialog(true)
    } else {
      handleDrainToggle(false)
    }
  }

  const confirmDrain = () => {
    setShowDrainDialog(false)
    handleDrainToggle(true)
  }

  if (error) {
    return (
      <div className="container mx-auto p-6 space-y-6">
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      </div>
    )
  }

  if (!node) {
    return (
      <div className="container mx-auto p-6">
        <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
          <Activity className="h-12 w-12 mb-4" />
          <p>Loading node...</p>
        </div>
      </div>
    )
  }

  const cpuUsed = node.cpu_total - node.cpu_available
  const cpuPercent = node.cpu_total > 0 ? Math.round((cpuUsed / node.cpu_total) * 100) : 0
  const memUsed = node.memory_total - node.memory_available
  const memPercent = node.memory_total > 0 ? Math.round((memUsed / node.memory_total) * 100) : 0

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 sm:gap-8">
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink to="/">Cluster</BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>Node {node.id}</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
        <div className="space-y-2 w-full sm:w-auto">
          <div className="flex items-center gap-3 sm:gap-4 justify-end">
            <h1 className="text-2xl sm:text-3xl font-bold font-mono">{node.id}</h1>
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div
                    className={`w-3 h-3 rounded-full ${
                      node.status === 'ready'
                        ? 'bg-green-500'
                        : node.status === 'draining'
                        ? 'bg-yellow-500 animate-pulse'
                        : node.status === 'drained'
                        ? 'bg-yellow-500'
                        : node.status === 'down'
                        ? 'bg-red-500'
                        : 'bg-gray-400'
                    }`}
                  />
                </TooltipTrigger>
                <TooltipContent>
                  <p className="capitalize">{node.status}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
          <p className="text-sm sm:text-base text-muted-foreground text-right">
            <span className="font-mono">{node.ip}</span> / {node.datacenter}
          </p>
        </div>
      </div>

      <div className="grid gap-4 grid-cols-2 sm:grid-cols-3 lg:grid-cols-5">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">CPU Usage</CardTitle>
            <Cpu className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{cpuPercent}%</div>
            <p className="text-xs text-muted-foreground">
              {cpuUsed} / {node.cpu_total} MHz
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Memory Usage</CardTitle>
            <MemoryStick className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{memPercent}%</div>
            <p className="text-xs text-muted-foreground">
              {memUsed} / {node.memory_total} MB
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Services</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {node.allocations_running || 0} / {node.allocations_planned || 0}
            </div>
            <p className="text-xs text-muted-foreground">Running / Planned</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Created At</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-sm font-bold mt-1 mb-2">
              {node.created_at
                ? formatDistanceToNow(new Date(node.created_at), { addSuffix: true })
                : '-'}
            </div>
            {node.created_at && (
              <p className="text-xs text-muted-foreground">
                {new Date(node.created_at).toLocaleString()}
              </p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Maintenance</CardTitle>
            <Wrench className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2 mt-1 mb-2">
              <div className="text-sm font-bold">Drain</div>
              <Switch
                checked={node.status === 'draining' || node.status === 'drained'}
                onCheckedChange={handleSwitchChange}
                disabled={node.status === 'draining'}
              />
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger>
                    <HelpCircle className="h-4 w-4 text-muted-foreground" />
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Gracefully migrate all services to other nodes.</p>
                    <p>Node remains in cluster but won't receive new allocations.</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </div>
            <p className="text-xs text-muted-foreground">
              {node.status === 'ready' ? 'Normal' : node.status === 'draining' ? 'Migrating...' : node.status === 'drained' ? 'Drained' : node.status}
            </p>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <MetricsChart title="CPU Usage" data={cpuMetrics} color="hsl(var(--chart-1))" />
        <MetricsChart title="Memory Usage" data={memoryMetrics} color="hsl(var(--chart-2))" />
        <MetricsChart title="Gateway RPS" data={rpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />
      </div>

      <Tabs defaultValue="services" className="space-y-4">
        <TabsList className="grid w-full grid-cols-2">
          <TabsTrigger value="services">
            <Activity className="h-4 w-4 mr-2" />
            Node Services
          </TabsTrigger>
          <TabsTrigger value="logs">
            <FileText className="h-4 w-4 mr-2" />
            Node Logs
          </TabsTrigger>
        </TabsList>

        <TabsContent value="services" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Running Services</CardTitle>
            </CardHeader>
            <CardContent className="overflow-x-auto">
              {allocations.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                  <Activity className="h-12 w-12 mb-4" />
                  <p>No services running on this node</p>
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Service</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Health</TableHead>
                      <TableHead>CPU</TableHead>
                      <TableHead>Memory</TableHead>
                      <TableHead>Restarts</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {allocations.map((alloc) => (
                      <TableRow
                        key={alloc.id}
                        className="cursor-pointer hover:bg-muted/50"
                        onClick={() => navigate(`/nodes/${nodeId}/alloc/${alloc.id}`)}
                      >
                        <TableCell className="font-medium">{alloc.service_name}</TableCell>
                        <TableCell>
                          <Badge
                            variant={
                              alloc.status === 'running'
                                ? 'success'
                                : alloc.status === 'failed'
                                ? 'destructive'
                                : 'secondary'
                            }
                          >
                            {alloc.status}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant={
                              alloc.health_status === 'healthy'
                                ? 'default'
                                : alloc.health_status === 'unhealthy'
                                ? 'destructive'
                                : 'secondary'
                            }
                          >
                            {alloc.health_status}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <div>{alloc.cpu_usage}%</div>
                          {services.get(alloc.service_name) && (
                            <div className="text-xs text-muted-foreground">
                              {Math.round((alloc.cpu_usage / 100) * services.get(alloc.service_name)!.CPU)} / {services.get(alloc.service_name)!.CPU} MHz
                            </div>
                          )}
                        </TableCell>
                        <TableCell>
                          <div>{Math.round((alloc.memory_usage / (services.get(alloc.service_name)?.Memory || 100)) * 100)}%</div>
                          <div className="text-xs text-muted-foreground">
                            {alloc.memory_usage} / {services.get(alloc.service_name)?.Memory || '?'} MB
                          </div>
                        </TableCell>
                        <TableCell>{alloc.restarts}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="logs" className="space-y-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle>Node Logs</CardTitle>
              <div className="flex items-center gap-2">
                {isStreamingRef.current && (
                  <Badge variant="outline" className="animate-pulse">
                    Live
                  </Badge>
                )}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setLogLines([])}
                >
                  Clear
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              <div className="bg-muted rounded-md p-4 h-96 overflow-auto font-mono text-sm">
                {logLines.length > 0 ? (
                  <>
                    {logLines.map((line, idx) => (
                      <div key={idx} className="text-foreground whitespace-pre-wrap">
                        {line}
                      </div>
                    ))}
                    <div ref={logsEndRef} />
                  </>
                ) : (
                  <div className="text-muted-foreground">
                    {isStreamingRef.current ? 'Waiting for logs...' : 'Connecting to log stream...'}
                  </div>
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      <AlertDialog open={showDrainDialog} onOpenChange={setShowDrainDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Drain Node</AlertDialogTitle>
            <AlertDialogDescription>
              This will gracefully migrate all running services from{' '}
              <code className="font-mono">{node.id}</code> to other nodes.
              The node will remain in the cluster but won't receive new allocations.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDrain}>
              Start Drain
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
