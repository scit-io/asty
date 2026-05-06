import { useParams } from 'react-router-dom'
import { useState, useEffect, useRef } from 'react'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import { Cpu, MemoryStick, Clock, Activity, RotateCw, StopCircle } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import type { MetricPoint } from '@/types'

interface Allocation {
  id: string
  service_name: string
  node_id: string
  status: string
  health_status: string
  cpu_usage: number
  memory_usage: number
  restarts: number
  started_at: string
  version: string
  pid: number
}

interface ServiceResources {
  CPU: number
  Memory: number
}

export default function ServiceDetail() {
  const { nodeId, allocId } = useParams<{ nodeId: string; allocId: string }>()
  const [allocation, setAllocation] = useState<Allocation | null>(null)
  const [serviceResources, setServiceResources] = useState<ServiceResources | null>(null)
  const [cpuMetrics, setCpuMetrics] = useState<MetricPoint[]>([])
  const [memoryMetrics, setMemoryMetrics] = useState<MetricPoint[]>([])
  const [error, setError] = useState<string | null>(null)
  const [logLines, setLogLines] = useState<string[]>([])
  const isStreamingRef = useRef(false)
  const eventSourceRef = useRef<EventSource | null>(null)
  const logsEndRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!allocId || !nodeId) return
    let timer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false

    const fetchData = async () => {
      try {
        let allocData: Allocation
        try {
          allocData = await api.getAllocation(allocId)
        } catch {
          const allocsRes = await api.getNodeAllocations(nodeId)
          const found = allocsRes.allocations.find(a => a.id === allocId)
          if (found) {
            allocData = found as Allocation
          } else {
            if (!cancelled) setError('Allocation not found')
            return
          }
        }
        if (cancelled) return
        setAllocation(allocData)
        setError(null)

        const [metricsRes, serviceRes] = await Promise.all([
          api.getAllocationMetrics(allocId).catch(() => ({
            allocation_id: allocId,
            cpu: [],
            memory: [],
            period: '1h',
          })),
          api.getService(allocData.service_name).catch(() => null),
        ])
        if (!cancelled) {
          setCpuMetrics(metricsRes.cpu || [])
          setMemoryMetrics(metricsRes.memory || [])
          if (serviceRes) {
            setServiceResources({
              CPU: serviceRes.service.Resources.CPU,
              Memory: serviceRes.service.Resources.Memory,
            })
          }
        }
      } catch {
        // keep current state
      }
      if (!cancelled) timer = setTimeout(fetchData, 5000)
    }

    fetchData()
    return () => { cancelled = true; if (timer) clearTimeout(timer) }
  }, [nodeId, allocId])

  useEffect(() => {
    if (!allocId) return
    let retryCount = 0
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false

    const startStreaming = () => {
      if (cancelled) return
      const eventSource = new EventSource(`/api/v1/logs/allocation/${allocId}?follow=true&lines=100`)

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
  }, [allocId])

  const handleRestart = async () => {
    if (!allocId) return
    try {
      await api.restartAllocation(allocId)
    } catch (error) {
      console.error('Failed to restart allocation:', error)
    }
  }

  const handleStop = async () => {
    if (!allocId) return
    try {
      await api.stopAllocation(allocId)
    } catch (error) {
      console.error('Failed to stop allocation:', error)
    }
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

  if (!allocation) {
    return (
      <div className="container mx-auto p-6">
        <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
          <Activity className="h-12 w-12 mb-4" />
          <p>Loading service...</p>
        </div>
      </div>
    )
  }

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
              <BreadcrumbLink to={`/nodes/${nodeId}`}>Node {allocation.node_id}</BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>{allocation.service_name}</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
        <div className="space-y-2 w-full sm:w-auto">
          <div className="flex items-center gap-3 sm:gap-4 justify-end">
            <h1 className="text-2xl sm:text-3xl font-bold">{allocation.service_name}</h1>
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div
                    className={`w-3 h-3 rounded-full ${
                      allocation.status === 'running'
                        ? 'bg-green-500'
                        : allocation.status === 'failed'
                        ? 'bg-red-500'
                        : 'bg-gray-400'
                    }`}
                  />
                </TooltipTrigger>
                <TooltipContent>
                  <p className="capitalize">{allocation.status}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
          <p className="text-muted-foreground font-mono text-xs sm:text-sm text-right">
            {allocation.id}
          </p>
        </div>
      </div>

      <Tabs defaultValue="overview" className="space-y-4">
        <TabsList className="grid w-full grid-cols-4">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="metrics">Metrics</TabsTrigger>
          <TabsTrigger value="logs">Logs</TabsTrigger>
          <TabsTrigger value="actions">Actions</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-4">
          <div className="grid gap-4 grid-cols-2 sm:grid-cols-2 lg:grid-cols-4">
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">CPU Usage</CardTitle>
                <Cpu className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{allocation.cpu_usage}%</div>
                {serviceResources && (
                  <p className="text-xs text-muted-foreground">
                    {Math.round((allocation.cpu_usage / 100) * serviceResources.CPU)} / {serviceResources.CPU} MHz
                  </p>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Memory Usage</CardTitle>
                <MemoryStick className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {serviceResources ? Math.round((allocation.memory_usage / serviceResources.Memory) * 100) : '?'}%
                </div>
                <p className="text-xs text-muted-foreground">
                  {allocation.memory_usage} / {serviceResources?.Memory || '?'} MB
                </p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Restarts</CardTitle>
                <Activity className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{allocation.restarts}</div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Uptime</CardTitle>
                <Clock className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-sm font-bold mt-1 mb-2">
                  {allocation.started_at
                    ? formatDistanceToNow(new Date(allocation.started_at), { addSuffix: true })
                    : '-'}
                </div>
                {allocation.started_at && (
                  <p className="text-xs text-muted-foreground">
                    {new Date(allocation.started_at).toLocaleString()}
                  </p>
                )}
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Details</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              <div className="flex justify-between">
                <span className="text-muted-foreground">Version</span>
                <span className="font-mono">{allocation.version}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-muted-foreground">PID</span>
                <span className="font-mono">{allocation.pid}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-muted-foreground">Health</span>
                <Badge
                  variant={
                    allocation.health_status === 'healthy'
                      ? 'default'
                      : allocation.health_status === 'unhealthy'
                      ? 'destructive'
                      : 'secondary'
                  }
                >
                  {allocation.health_status}
                </Badge>
              </div>
              <div className="flex justify-between">
                <span className="text-muted-foreground">Started</span>
                <span>{allocation.started_at ? new Date(allocation.started_at).toLocaleString() : '-'}</span>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="metrics" className="space-y-4">
          <div className="grid gap-4 md:grid-cols-2">
            <MetricsChart title="CPU Usage" data={cpuMetrics} color="hsl(var(--chart-1))" />
            <MetricsChart title="Memory Usage" data={memoryMetrics} color="hsl(var(--chart-2))" unit=" MB" />
          </div>
        </TabsContent>

        <TabsContent value="logs" className="space-y-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle>Service Logs</CardTitle>
              <div className="flex items-center gap-2">
                {isStreamingRef.current && (
                  <Badge variant="default" className="animate-pulse">
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
                      <div key={idx} className="text-foreground whitespace-pre-wrap break-all">
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

        <TabsContent value="actions" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Service Actions</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex gap-4">
                <Button variant="outline" onClick={handleRestart}>
                  <RotateCw className="h-4 w-4 mr-2" />
                  Restart Service
                </Button>
                <Button variant="destructive" onClick={handleStop}>
                  <StopCircle className="h-4 w-4 mr-2" />
                  Stop Service
                </Button>
              </div>
              <Alert>
                <AlertDescription>
                  Stopping the service will terminate the process. Use restart for graceful reload.
                </AlertDescription>
              </Alert>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
