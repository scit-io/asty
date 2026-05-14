import { useParams } from 'react-router-dom'
import { useState, useEffect, useRef, useMemo } from 'react'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
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
import { Cpu, MemoryStick, Clock, Activity, RotateCw, StopCircle } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { useClusterStore } from '@/store/cluster'

export default function ServiceDetail() {
  const { nodeId, allocId } = useParams<{ nodeId: string; allocId: string }>()
  const { allocationCache, subscribeAllocation, services } = useClusterStore()
  const cached = allocId ? allocationCache[allocId] : undefined
  const allocation = cached?.allocation || null
  const [logLines, setLogLines] = useState<string[]>([])
  const [isStreaming, setIsStreaming] = useState(false)
  const logsEndRef = useRef<HTMLDivElement>(null)

  // Resolve service resource limits from the global services list (SSE-backed).
  const serviceResources = useMemo(() => {
    if (!allocation?.service_name) return null
    const svc = services.find((s) => s.Name === allocation.service_name)
    return svc ? { CPU: svc.Resources.CPU, Memory: svc.Resources.Memory } : null
  }, [allocation?.service_name, services])

  // Subscribe to allocation detail SSE
  useEffect(() => {
    if (!nodeId || !allocId) return
    return subscribeAllocation(nodeId, allocId)
  }, [nodeId, allocId, subscribeAllocation])

  useEffect(() => {
    if (!allocId) return
    let retryCount = 0
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false
    let eventSource: EventSource | null = null

    const startStreaming = () => {
      if (cancelled) return
      eventSource = new EventSource(`/nodes/${nodeId}/allocations/${allocId}/logs`)

      eventSource.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)
          setLogLines((prev) => {
            const next = prev.length >= 500
              ? prev.slice(prev.length - 499).concat(data.line)
              : prev.concat(data.line)
            return next
          })
          setTimeout(() => logsEndRef.current?.scrollIntoView({ behavior: 'smooth' }), 100)
        } catch {
          // ignore
        }
      }

      eventSource.onerror = () => {
        eventSource?.close()
        setIsStreaming(false)
        if (cancelled) return
        retryCount++
        retryTimer = setTimeout(startStreaming, Math.min(5000 * Math.pow(2, retryCount - 1), 60000))
      }

      eventSource.onopen = () => {
        retryCount = 0
        setIsStreaming(true)
      }
    }

    startStreaming()
    return () => {
      cancelled = true
      if (retryTimer) clearTimeout(retryTimer)
      eventSource?.close()
    }
  }, [nodeId, allocId])

  const handleRestart = async () => {
    if (!nodeId || !allocId) return
    try {
      await api.restartAllocation(nodeId, allocId)
    } catch (error) {
      console.error('Failed to restart allocation:', error)
    }
  }

  const handleStop = async () => {
    if (!nodeId || !allocId) return
    try {
      await api.stopAllocation(nodeId, allocId)
    } catch (error) {
      console.error('Failed to stop allocation:', error)
    }
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
              <BreadcrumbLink to={`/nodes/${nodeId}`}>Node {allocation?.node_id || nodeId}</BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>{allocation?.service_name || allocId}</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
        <div className="space-y-2 w-full sm:w-auto">
          <div className="flex items-center gap-3 sm:gap-4 justify-end">
            {allocation ? (
              <h1 className="text-2xl sm:text-3xl font-bold">{allocation.service_name}</h1>
            ) : (
              <Skeleton className="h-9 w-32" />
            )}
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  {allocation ? (
                    <div
                      className={`w-3 h-3 rounded-full ${
                        allocation.status === 'running'
                          ? 'bg-green-500'
                          : allocation.status === 'failed'
                          ? 'bg-red-500'
                          : 'bg-gray-400'
                      }`}
                    />
                  ) : (
                    <Skeleton className="w-3 h-3 rounded-full" />
                  )}
                </TooltipTrigger>
                <TooltipContent>
                  <p className="capitalize">{allocation?.status || 'loading'}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
          <p className="text-muted-foreground font-mono text-xs sm:text-sm text-right">
            {allocation ? allocation.id : <Skeleton className="h-4 w-32 ml-auto" />}
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
                {allocation ? (
                  <>
                    <div className="text-2xl font-bold">{allocation.cpu_usage}%</div>
                    {serviceResources && (
                      <p className="text-xs text-muted-foreground">
                        {Math.round((allocation.cpu_usage / 100) * serviceResources.CPU)} / {serviceResources.CPU} MHz
                      </p>
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

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Memory Usage</CardTitle>
                <MemoryStick className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                {allocation ? (
                  <>
                    <div className="text-2xl font-bold">
                      {serviceResources ? Math.round((allocation.memory_usage / serviceResources.Memory) * 100) : '?'}%
                    </div>
                    <p className="text-xs text-muted-foreground">
                      {allocation.memory_usage} / {serviceResources?.Memory || '?'} MB
                    </p>
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
                <CardTitle className="text-sm font-medium">Restarts</CardTitle>
                <Activity className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                {allocation ? (
                  <div className="text-2xl font-bold">{allocation.restarts}</div>
                ) : (
                  <Skeleton className="h-8 w-8" />
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Uptime</CardTitle>
                <Clock className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                {allocation ? (
                  <>
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
                  </>
                ) : (
                  <>
                    <Skeleton className="h-5 w-24 mt-1 mb-2" />
                    <Skeleton className="h-3 w-32" />
                  </>
                )}
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Details</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              {allocation ? (
                <>
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
                </>
              ) : (
                <>
                  <Skeleton className="h-5 w-full" />
                  <Skeleton className="h-5 w-full" />
                  <Skeleton className="h-5 w-full" />
                  <Skeleton className="h-5 w-full" />
                </>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="metrics" className="space-y-4">
          <Card>
            <CardContent className="pt-6">
              <p className="text-sm text-muted-foreground">
                Per-allocation metrics are available in the parent service overview.
                Current CPU: <span className="font-mono font-medium">{allocation?.cpu_usage ?? '-'}%</span>{' '}
                / Memory: <span className="font-mono font-medium">{allocation?.memory_usage ?? '-'} MB</span>
              </p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="logs" className="space-y-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle>Service Logs</CardTitle>
              <div className="flex items-center gap-2">
                {isStreaming && (
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
                    {isStreaming ? 'Waiting for logs...' : 'Connecting to log stream...'}
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
