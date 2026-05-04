import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Server, CheckCircle, Package, Cpu, MemoryStick } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { ThemeToggle } from '@/components/theme-toggle'

export default function Dashboard() {
  const navigate = useNavigate()

  const { data: statusData, isLoading: statusLoading } = useQuery({
    queryKey: ['status'],
    queryFn: api.getStatus,
    refetchInterval: 5000,
  })

  const { data: nodesData, isLoading: nodesLoading } = useQuery({
    queryKey: ['nodes'],
    queryFn: api.getNodes,
    refetchInterval: 5000,
  })

  const isLoading = statusLoading || nodesLoading
  const nodes = nodesData?.nodes || []

  if (isLoading) {
    return (
      <div className="container mx-auto p-6 space-y-6">
        <Skeleton className="h-9 w-48" />
        <div className="grid gap-4 md:grid-cols-3">
          <Skeleton className="h-32" />
          <Skeleton className="h-32" />
          <Skeleton className="h-32" />
        </div>
        <Skeleton className="h-96" />
      </div>
    )
  }

  const stats = [
    {
      name: 'Total Nodes',
      value: statusData?.cluster.nodes_total || 0,
      icon: Server,
      color: 'text-blue-500',
    },
    {
      name: 'Healthy Nodes',
      value: statusData?.cluster.nodes_healthy || 0,
      icon: CheckCircle,
      color: 'text-green-500',
    },
    {
      name: 'Services',
      value: statusData?.services.loaded || 0,
      icon: Package,
      color: 'text-purple-500',
    },
  ]

  return (
    <div className="container mx-auto p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Cluster Dashboard</h1>
        <ThemeToggle />
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        {stats.map((stat) => (
          <Card key={stat.name}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{stat.name}</CardTitle>
              <stat.icon className={`h-4 w-4 ${stat.color}`} />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stat.value}</div>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Nodes</CardTitle>
        </CardHeader>
        <CardContent>
          {nodes.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <Server className="h-12 w-12 mb-4" />
              <p>No nodes found</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Node ID</TableHead>
                  <TableHead>Datacenter</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>CPU</TableHead>
                  <TableHead>Memory</TableHead>
                  <TableHead>Services</TableHead>
                  <TableHead>Last Seen</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {nodes.map((node) => {
                  const cpuUsed = node.cpu_total - node.cpu_available
                  const cpuPercent =
                    node.cpu_total > 0 ? Math.round((cpuUsed / node.cpu_total) * 100) : 0
                  const memUsed = node.memory_total - node.memory_available
                  const memPercent =
                    node.memory_total > 0 ? Math.round((memUsed / node.memory_total) * 100) : 0

                  return (
                    <TableRow
                      key={node.id}
                      className="cursor-pointer hover:bg-muted/50"
                      onClick={() => navigate(`/nodes/${node.id}`)}
                    >
                      <TableCell className="font-mono font-medium">{node.id}</TableCell>
                      <TableCell>{node.datacenter}</TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            node.status === 'ready'
                              ? 'default'
                              : node.status === 'down'
                              ? 'destructive'
                              : 'secondary'
                          }
                        >
                          {node.status}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Cpu className="h-4 w-4 text-muted-foreground" />
                          <div className="space-y-1">
                            <div className="text-sm font-medium">{cpuPercent}%</div>
                            <div className="text-xs text-muted-foreground">
                              {cpuUsed} / {node.cpu_total} MHz
                            </div>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <MemoryStick className="h-4 w-4 text-muted-foreground" />
                          <div className="space-y-1">
                            <div className="text-sm font-medium">{memPercent}%</div>
                            <div className="text-xs text-muted-foreground">
                              {memUsed} / {node.memory_total} MB
                            </div>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>{node.processes?.length || 0}</TableCell>
                      <TableCell className="text-muted-foreground">
                        {formatDistanceToNow(new Date(node.last_seen), { addSuffix: true })}
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
