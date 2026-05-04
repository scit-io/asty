import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { useState, useEffect, useRef } from 'react'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Server, Cpu, MemoryStick, FileText } from 'lucide-react'

export default function Dashboard() {
  const navigate = useNavigate()
  const [clusterLogs, setClusterLogs] = useState<string[]>([])
  const [isStreaming, setIsStreaming] = useState(false)
  const eventSourceRef = useRef<EventSource | null>(null)
  const logsEndRef = useRef<HTMLDivElement>(null)

  const { isLoading: statusLoading } = useQuery({
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

  // Start SSE cluster log streaming
  useEffect(() => {
    const startStreaming = () => {
      const eventSource = new EventSource('/api/v1/logs/cluster?follow=true&lines=100')

      eventSource.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)
          setClusterLogs((prev) => [...prev, data.line])

          setTimeout(() => {
            logsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
          }, 100)
        } catch (err) {
          console.error('Failed to parse cluster log event:', err)
        }
      }

      eventSource.onerror = (error) => {
        console.error('Cluster SSE error:', error)
        eventSource.close()
        setIsStreaming(false)

        setTimeout(() => {
          if (eventSourceRef.current === eventSource) {
            startStreaming()
          }
        }, 5000)
      }

      eventSource.onopen = () => {
        setIsStreaming(true)
      }

      eventSourceRef.current = eventSource
    }

    startStreaming()

    return () => {
      eventSourceRef.current?.close()
      eventSourceRef.current = null
      setIsStreaming(false)
    }
  }, [])

  if (isLoading) {
    return (
      <div className="container mx-auto p-6 space-y-6">
        <Skeleton className="h-9 w-48" />
        <Skeleton className="h-96" />
      </div>
    )
  }

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
      <Tabs defaultValue="nodes" className="space-y-4">
        <TabsList className="grid w-full grid-cols-2">
          <TabsTrigger value="nodes">
            <Server className="h-4 w-4 mr-2" />
            Nodes
          </TabsTrigger>
          <TabsTrigger value="logs">
            <FileText className="h-4 w-4 mr-2" />
            Cluster Logs
          </TabsTrigger>
        </TabsList>

        <TabsContent value="nodes" className="space-y-4">
          <Card>
            <CardHeader className="text-right">
              <CardTitle>Cluster Nodes</CardTitle>
            </CardHeader>
            <CardContent className="overflow-x-auto">
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
                  <TableHead>IP</TableHead>
                  <TableHead>DC</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>CPU</TableHead>
                  <TableHead>Memory</TableHead>
                  <TableHead className="text-right">Services</TableHead>
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
                      <TableCell className="font-mono">{node.ip || '-'}</TableCell>
                      <TableCell>{node.datacenter}</TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            node.status === 'ready'
                              ? 'success'
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
                      <TableCell className="text-right">
                        {node.allocations_running || 0} / {node.allocations_planned || 0}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="logs" className="space-y-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle>Asty Cluster Logs</CardTitle>
              <div className="flex items-center gap-2">
                {isStreaming && (
                  <Badge variant="default" className="animate-pulse">
                    Live
                  </Badge>
                )}
                <Button variant="outline" size="sm" onClick={() => setClusterLogs([])}>
                  Clear
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              <div className="bg-muted rounded-md p-4 h-[600px] overflow-auto font-mono text-sm">
                {clusterLogs.length > 0 ? (
                  <>
                    {clusterLogs.map((line, idx) => (
                      <div key={idx} className="text-foreground whitespace-pre-wrap break-all">
                        {line}
                      </div>
                    ))}
                    <div ref={logsEndRef} />
                  </>
                ) : (
                  <div className="text-muted-foreground">
                    {isStreaming ? 'Waiting for cluster logs...' : 'Connecting to cluster log stream...'}
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
