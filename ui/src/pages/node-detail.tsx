import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
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
import { Cpu, MemoryStick, Clock, Activity, PlayCircle, StopCircle, Settings, ArrowLeft } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'

export default function NodeDetail() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const navigate = useNavigate()

  // Fallback: get node from nodes list if detail endpoint doesn't exist
  const { data: nodesData } = useQuery({
    queryKey: ['nodes'],
    queryFn: api.getNodes,
    refetchInterval: 5000,
  })

  const { data: node, isLoading: nodeLoading, error: nodeError } = useQuery({
    queryKey: ['node', nodeId],
    queryFn: async () => {
      try {
        return await api.getNode(nodeId!)
      } catch (error) {
        // Fallback to nodes list
        const nodeFromList = nodesData?.nodes.find(n => n.id === nodeId)
        if (nodeFromList) return nodeFromList
        throw error
      }
    },
    enabled: !!nodeId,
    refetchInterval: 5000,
    retry: false,
  })

  const { data: allocsData, isLoading: allocsLoading } = useQuery({
    queryKey: ['node-allocations', nodeId],
    queryFn: () => api.getNodeAllocations(nodeId!),
    enabled: !!nodeId,
    refetchInterval: 5000,
    retry: false,
  })

  const { data: logs } = useQuery({
    queryKey: ['node-logs', nodeId],
    queryFn: () => api.getNodeLogs(nodeId!),
    enabled: !!nodeId,
    retry: false,
  })

  if (nodeLoading) {
    return (
      <div className="container mx-auto p-6 space-y-6">
        <Skeleton className="h-12 w-64" />
        <Skeleton className="h-96" />
      </div>
    )
  }

  if (nodeError) {
    return (
      <div className="container mx-auto p-6 space-y-6">
        <div className="flex items-center gap-4">
          <Button variant="ghost" size="icon" onClick={() => navigate('/')}>
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <h1 className="text-3xl font-bold">Node Not Available</h1>
        </div>
        <Alert variant="destructive">
          <AlertDescription>
            Unable to load node details. API endpoint may not be implemented yet.
            <br />
            <span className="text-xs mt-2 block">Error: {String(nodeError)}</span>
          </AlertDescription>
        </Alert>
        <Alert>
          <AlertDescription>
            <strong>Required API endpoint:</strong> GET /api/v1/nodes/{nodeId}
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  if (!node) {
    return (
      <div className="container mx-auto p-6">
        <Alert variant="destructive">
          <AlertDescription>Node not found</AlertDescription>
        </Alert>
      </div>
    )
  }

  const allocations = allocsData?.allocations || []
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
              {formatDistanceToNow(new Date(node.created_at), { addSuffix: true })}
            </div>
            <p className="text-xs text-muted-foreground">
              {new Date(node.created_at).toLocaleString()}
            </p>
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
              {allocsLoading ? (
                <Skeleton className="h-64" />
              ) : allocations.length === 0 ? (
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
            <CardHeader>
              <CardTitle>Node Logs</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="bg-muted rounded-md p-4 h-96 overflow-auto font-mono text-sm">
                {logs?.logs && logs.logs.length > 0 ? (
                  logs.logs.map((line, idx) => (
                    <div key={idx} className="text-foreground">
                      {line}
                    </div>
                  ))
                ) : (
                  <div className="text-muted-foreground">No logs available</div>
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
