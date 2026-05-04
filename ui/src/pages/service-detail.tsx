import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { ArrowLeft, Cpu, MemoryStick, Clock, Activity, RotateCw, StopCircle } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { ThemeToggle } from '@/components/theme-toggle'

export default function ServiceDetail() {
  const { nodeId, allocId } = useParams<{ nodeId: string; allocId: string }>()
  const navigate = useNavigate()

  // Fallback: get allocation from node allocations if detail endpoint doesn't exist
  const { data: allocsData } = useQuery({
    queryKey: ['node-allocations', nodeId],
    queryFn: () => api.getNodeAllocations(nodeId!),
    enabled: !!nodeId,
    refetchInterval: 5000,
  })

  const { data: allocation, isLoading, error: allocError } = useQuery({
    queryKey: ['allocation', allocId],
    queryFn: async () => {
      try {
        return await api.getAllocation(allocId!)
      } catch (error) {
        // Fallback to node allocations list
        const allocFromList = allocsData?.allocations.find(a => a.id === allocId)
        if (allocFromList) return allocFromList
        throw error
      }
    },
    enabled: !!allocId,
    refetchInterval: 5000,
    retry: false,
  })

  const { data: logs } = useQuery({
    queryKey: ['allocation-logs', allocId],
    queryFn: () => api.getAllocationLogs(allocId!),
    enabled: !!allocId,
  })

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

  if (isLoading) {
    return (
      <div className="container mx-auto p-6 space-y-6">
        <Skeleton className="h-12 w-64" />
        <Skeleton className="h-96" />
      </div>
    )
  }

  if (allocError) {
    return (
      <div className="container mx-auto p-6 space-y-6">
        <div className="flex items-center gap-4">
          <Button variant="ghost" size="icon" onClick={() => navigate(`/nodes/${nodeId}`)}>
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <h1 className="text-3xl font-bold">Service Not Available</h1>
        </div>
        <Alert variant="destructive">
          <AlertDescription>
            Unable to load service details. API endpoint may not be implemented yet.
            <br />
            <span className="text-xs mt-2 block">Error: {String(allocError)}</span>
          </AlertDescription>
        </Alert>
        <Alert>
          <AlertDescription>
            <strong>Required API endpoint:</strong> GET /api/v1/allocations/{allocId}
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  if (!allocation) {
    return (
      <div className="container mx-auto p-6">
        <Alert variant="destructive">
          <AlertDescription>Service allocation not found</AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <div className="container mx-auto p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button variant="ghost" size="icon" onClick={() => navigate(`/nodes/${nodeId}`)}>
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <div>
            <h1 className="text-3xl font-bold">{allocation.service_name}</h1>
            <p className="text-muted-foreground font-mono text-sm">
              {allocation.id} on{' '}
              <button
                className="underline hover:text-foreground"
                onClick={() => navigate(`/nodes/${nodeId}`)}
              >
                {allocation.node_id}
              </button>
            </p>
          </div>
          <Badge
            variant={
              allocation.status === 'running'
                ? 'default'
                : allocation.status === 'failed'
                ? 'destructive'
                : 'secondary'
            }
          >
            {allocation.status}
          </Badge>
        </div>
        <ThemeToggle />
      </div>

      <Tabs defaultValue="overview" className="space-y-4">
        <TabsList className="grid w-full grid-cols-4">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="health">Health</TabsTrigger>
          <TabsTrigger value="logs">Logs</TabsTrigger>
          <TabsTrigger value="actions">Actions</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-4">
          <div className="grid gap-4 md:grid-cols-4">
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">CPU Usage</CardTitle>
                <Cpu className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{allocation.cpu_usage}%</div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Memory Usage</CardTitle>
                <MemoryStick className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{allocation.memory_usage} MB</div>
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
                <div className="text-sm font-bold">
                  {formatDistanceToNow(new Date(allocation.started_at), { addSuffix: true })}
                </div>
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
                <span className="text-muted-foreground">Started</span>
                <span>{new Date(allocation.started_at).toLocaleString()}</span>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="health" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Health Status</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-lg font-medium">Current Status</span>
                <Badge
                  variant={
                    allocation.health_status === 'healthy'
                      ? 'default'
                      : allocation.health_status === 'unhealthy'
                      ? 'destructive'
                      : 'secondary'
                  }
                  className="text-lg px-4 py-1"
                >
                  {allocation.health_status}
                </Badge>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="logs" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Service Logs</CardTitle>
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
