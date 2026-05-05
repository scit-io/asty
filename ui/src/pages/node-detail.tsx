import { useNavigate, useParams } from 'react-router-dom'
import { useState, useEffect, useRef } from 'react'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
import { Cpu, MemoryStick, Clock, Activity, PlayCircle, StopCircle, Settings } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
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

export default function NodeDetail() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const navigate = useNavigate()
  const [node, setNode] = useState<Node | null>(null)
  const [allocations, setAllocations] = useState<Allocation[]>([])
  const [cpuMetrics, setCpuMetrics] = useState<MetricPoint[]>([])
  const [memoryMetrics, setMemoryMetrics] = useState<MetricPoint[]>([])
  const [error, setError] = useState<string | null>(null)
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

        const [allocsRes, metricsRes] = await Promise.all([
          api.getNodeAllocations(nodeId).catch(() => ({ allocations: [] })),
          api.getNodeMetrics(nodeId).catch(() => ({ cpu: [], memory: [], period: '1h' })),
        ])
        if (!cancelled) {
          setAllocations((allocsRes as { allocations: Allocation[] }).allocations || [])
          setCpuMetrics(metricsRes.cpu || [])
          setMemoryMetrics(metricsRes.memory || [])
        }
      } catch {
        // keep current state on error
      }
      if (!cancelled) timer = setTimeout(fetchData, 5000)
    }

    fetchData()
    return () => { cancelled = true; if (timer) clearTimeout(timer) }
  }, [nodeId])

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
        } catch {
          // ignore
        }
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
              <BreadcrumbLink to="/">Dashboard</BreadcrumbLink>
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
            <CardTitle className="text-sm font-medium">Actions</CardTitle>
            <Settings className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => api.pauseNode(nodeId!)}
            >
              <StopCircle className="h-4 w-4 mr-2" />
              Pause
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => api.drainNode(nodeId!)}
            >
              <PlayCircle className="h-4 w-4 mr-2" />
              Drain
            </Button>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <MetricsChart title="CPU Usage" data={cpuMetrics} color="hsl(var(--chart-1))" />
        <MetricsChart title="Memory Usage" data={memoryMetrics} color="hsl(var(--chart-2))" />
      </div>

      <Tabs defaultValue="services" className="space-y-4">
        <TabsList className="grid w-full grid-cols-2">
          <TabsTrigger value="services">Services</TabsTrigger>
          <TabsTrigger value="logs">Logs</TabsTrigger>
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
                                ? 'default'
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
                        <TableCell>{alloc.cpu_usage}%</TableCell>
                        <TableCell>{alloc.memory_usage} MB</TableCell>
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
    </div>
  )
}
