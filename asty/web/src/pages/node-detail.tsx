import { useNavigate, useParams } from 'react-router-dom'
import { useState, useEffect, useRef } from 'react'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
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
import { useClusterStore } from '@/store/cluster'

interface ServiceResources {
  CPU: number
  Memory: number
}

export default function NodeDetail() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const navigate = useNavigate()
  const { nodeCache, subscribeNode, updateNodeStatus, services: servicesList } = useClusterStore()
  const cached = nodeId ? nodeCache[nodeId] : undefined
  const node = cached?.node || null
  const allocations = cached?.allocations || []
  const cpuMetrics = cached?.cpuMetrics || []
  const memoryMetrics = cached?.memoryMetrics || []
  const rpsMetrics = cached?.rpsMetrics || []
  const [services, setServices] = useState<Map<string, ServiceResources>>(new Map())
  const [showDrainDialog, setShowDrainDialog] = useState(false)
  const [logLines, setLogLines] = useState<string[]>([])
  const [isStreaming, setIsStreaming] = useState(false)
  const logsEndRef = useRef<HTMLDivElement>(null)

  // Subscribe to node detail SSE (allocations + metrics) for as long as page is mounted
  useEffect(() => {
    if (!nodeId) return
    return subscribeNode(nodeId)
  }, [nodeId, subscribeNode])

  useEffect(() => {
    const svcMap = new Map<string, ServiceResources>()
    servicesList.forEach(svc => {
      svcMap.set(svc.Name, { CPU: svc.Resources.CPU, Memory: svc.Resources.Memory })
    })
    setServices(svcMap)
  }, [servicesList])

  // Toast notifications when drain status changes via global SSE
  const prevStatusRef = useRef<string | undefined>(undefined)
  const initializedRef = useRef(false)
  const nodeStatus = node?.status
  useEffect(() => {
    if (!nodeId || !nodeStatus) return
    if (!initializedRef.current) {
      initializedRef.current = true
      prevStatusRef.current = nodeStatus
      return
    }
    const prev = prevStatusRef.current
    prevStatusRef.current = nodeStatus
    if (prev === nodeStatus) return

    if (nodeStatus === 'drained') {
      toast.success('Node drained', {
        id: `drain-${nodeId}`,
        description: 'All allocations migrated successfully',
      })
    }
  }, [nodeId, nodeStatus])

  // Log streaming
  useEffect(() => {
    if (!nodeId) return
    let retryCount = 0
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false
    let eventSource: EventSource | null = null

    const startStreaming = () => {
      if (cancelled) return
      eventSource = new EventSource(`/nodes/${nodeId}/logs`)

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
        } catch { /* ignore */ }
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
  }, [nodeId])

  const handleDrainToggle = async (enable: boolean) => {
    if (!nodeId) return
    try {
      const result = await api.drainNode(nodeId, enable) as { status: string; total_allocations?: number }
      if (enable) {
        updateNodeStatus(nodeId, 'draining')
        toast.loading('Draining node...', {
          id: `drain-${nodeId}`,
          description: `Migrating ${result.total_allocations || 0} allocations`,
        })
      } else {
        updateNodeStatus(nodeId, 'ready')
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

  const cpuUsed = node ? node.cpu_total - node.cpu_available : 0
  const cpuPercent = node && node.cpu_total > 0 ? Math.round((cpuUsed / node.cpu_total) * 100) : 0
  const memUsed = node ? node.memory_total - node.memory_available : 0
  const memPercent = node && node.memory_total > 0 ? Math.round((memUsed / node.memory_total) * 100) : 0

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
              <BreadcrumbPage>Node {node?.id || nodeId}</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
        <div className="space-y-2 w-full sm:w-auto">
          <div className="flex items-center gap-3 sm:gap-4 justify-end">
            {node ? (
              <h1 className="text-2xl sm:text-3xl font-bold font-mono">{node.id}</h1>
            ) : (
              <Skeleton className="h-9 w-32" />
            )}
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  {node ? (
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
                  ) : (
                    <Skeleton className="w-3 h-3 rounded-full" />
                  )}
                </TooltipTrigger>
                <TooltipContent>
                  <p className="capitalize">{node?.status || 'loading'}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
          <div className="text-sm sm:text-base text-muted-foreground text-right">
            {node ? (
              <>
                <span className="font-mono">{node.ip}</span> / {node.datacenter}
              </>
            ) : (
              <Skeleton className="h-4 w-40 ml-auto" />
            )}
          </div>
        </div>
      </div>

      <div className="grid gap-4 grid-cols-2 sm:grid-cols-3 lg:grid-cols-5">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">CPU Usage</CardTitle>
            <Cpu className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {node ? (
              <>
                <div className="text-2xl font-bold">{cpuPercent}%</div>
                <p className="text-xs text-muted-foreground">
                  {cpuUsed} / {node.cpu_total} MHz
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
            <CardTitle className="text-sm font-medium">Memory Usage</CardTitle>
            <MemoryStick className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {node ? (
              <>
                <div className="text-2xl font-bold">{memPercent}%</div>
                <p className="text-xs text-muted-foreground">
                  {memUsed} / {node.memory_total} MB
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
            <CardTitle className="text-sm font-medium">Allocations</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {node ? (
              <>
                <div className="text-2xl font-bold">
                  {node.allocations_running || 0} / {node.allocations_planned || 0}
                </div>
                <p className="text-xs text-muted-foreground">Running / Planned</p>
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
            <CardTitle className="text-sm font-medium">Created At</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {node ? (
              <>
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
              </>
            ) : (
              <>
                <Skeleton className="h-5 w-24 mt-1 mb-2" />
                <Skeleton className="h-3 w-32" />
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Maintenance</CardTitle>
            <Wrench className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {node ? (
              <>
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
                        <p>Gracefully migrate all allocations to other nodes.</p>
                        <p>Node remains in cluster but won't receive new allocations.</p>
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                </div>
                <p className="text-xs text-muted-foreground">
                  {node.status === 'ready' ? 'Normal' : node.status === 'draining' ? 'Migrating...' : node.status === 'drained' ? 'Drained' : node.status}
                </p>
              </>
            ) : (
              <>
                <Skeleton className="h-5 w-32 mt-1 mb-2" />
                <Skeleton className="h-3 w-24" />
              </>
            )}
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <MetricsChart title="CPU Usage" data={cpuMetrics} color="hsl(var(--chart-1))" />
        <MetricsChart title="Memory Usage" data={memoryMetrics} color="hsl(var(--chart-2))" />
        <MetricsChart title="Gateway RPS" data={rpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />
      </div>

      <Tabs defaultValue="allocations" className="space-y-4">
        <TabsList className="grid w-full grid-cols-2">
          <TabsTrigger value="allocations">
            <Activity className="h-4 w-4 mr-2" />
            Node Allocations
          </TabsTrigger>
          <TabsTrigger value="logs">
            <FileText className="h-4 w-4 mr-2" />
            Node Logs
          </TabsTrigger>
        </TabsList>

        <TabsContent value="allocations" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Running Allocations</CardTitle>
            </CardHeader>
            <CardContent className="overflow-x-auto">
              {allocations.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                  <Activity className="h-12 w-12 mb-4" />
                  <p>No allocations running on this node</p>
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Allocation</TableHead>
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
                        onClick={() => navigate(`/nodes/${nodeId}/allocations/${alloc.id}`)}
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
                {isStreaming && (
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
                    {isStreaming ? 'Waiting for logs...' : 'Connecting to log stream...'}
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
              This will gracefully migrate all running allocations from{' '}
              <code className="font-mono">{node?.id || nodeId}</code> to other nodes.
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
