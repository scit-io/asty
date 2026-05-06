import { useNavigate } from 'react-router-dom'
import { useState, useEffect, useRef } from 'react'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { MetricsChart } from '@/components/metrics-chart'
import { Server, Cpu, MemoryStick, FileText, Activity, Shield, RefreshCw, Heart } from 'lucide-react'
import type { MetricPoint, ClusterStatus } from '@/types'

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
}

export default function Cluster() {
  const navigate = useNavigate()
  const [nodes, setNodes] = useState<Node[]>([])
  const [clusterStatus, setClusterStatus] = useState<ClusterStatus | null>(null)
  const [cpuMetrics, setCpuMetrics] = useState<MetricPoint[]>([])
  const [memoryMetrics, setMemoryMetrics] = useState<MetricPoint[]>([])
  const [rpsMetrics, setRpsMetrics] = useState<MetricPoint[]>([])
  const [clusterLogs, setClusterLogs] = useState<string[]>([])
  const isStreamingRef = useRef(false)
  const eventSourceRef = useRef<EventSource | null>(null)
  const logsEndRef = useRef<HTMLDivElement>(null)
  const statusStreamRef = useRef<EventSource | null>(null)

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false

    const fetchData = async () => {
      try {
        const [nodesRes, statusRes, metricsRes] = await Promise.all([
          api.getNodes(),
          api.getStatus(),
          api.getClusterMetrics('1h'),
        ])
        if (cancelled) return
        setNodes(nodesRes.nodes || [])
        setClusterStatus(statusRes)
        setCpuMetrics(metricsRes.cpu || [])
        setMemoryMetrics(metricsRes.memory || [])
        setRpsMetrics(metricsRes.rps || [])
      } catch {
        // keep current state
      }
      if (!cancelled) timer = setTimeout(fetchData, 5000)
    }

    fetchData()
    return () => { cancelled = true; if (timer) clearTimeout(timer) }
  }, [])

  // SSE stream for real-time cluster status updates
  useEffect(() => {
    const eventSource = new EventSource('/api/v1/stream')

    eventSource.addEventListener('status', (event) => {
      try {
        const data = JSON.parse(event.data)
        if (data.cluster) {
          setClusterStatus((prev) => prev ? { ...prev, cluster: data.cluster } : prev)
        }
      } catch {
        // ignore parse errors
      }
    })

    eventSource.onerror = () => {
      eventSource.close()
    }

    statusStreamRef.current = eventSource
    return () => {
      eventSource.close()
      statusStreamRef.current = null
    }
  }, [])

  useEffect(() => {
    let retryCount = 0
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false

    const startStreaming = () => {
      if (cancelled) return
      const eventSource = new EventSource('/api/v1/logs/cluster?follow=true&lines=100')

      eventSource.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)
          setClusterLogs((prev) => [...prev, data.line])
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
  }, [])

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
      {clusterStatus && (
        <div className="grid gap-4 grid-cols-2 sm:grid-cols-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Nodes</CardTitle>
              <Server className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{clusterStatus.cluster.nodes_healthy}</div>
              <p className="text-xs text-muted-foreground">
                of {clusterStatus.cluster.nodes_total} total
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Services</CardTitle>
              <Activity className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{clusterStatus.services.loaded}</div>
              <p className="text-xs text-muted-foreground">loaded</p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Leader</CardTitle>
              <Shield className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-sm font-bold font-mono mt-1 mb-2">
                {clusterStatus.cluster.leader || 'none'}
              </div>
              <p className="text-xs text-muted-foreground font-mono">
                {clusterStatus.cluster.leader_ip || '-'}
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Cluster Health</CardTitle>
              <Heart className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                {clusterStatus.cluster.nodes_total > 0
                  ? Math.round((clusterStatus.cluster.nodes_healthy / clusterStatus.cluster.nodes_total) * 100)
                  : 0}%
              </div>
              <p className="text-xs text-muted-foreground">nodes healthy</p>
            </CardContent>
          </Card>
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-3">
        <MetricsChart title="Cluster CPU" data={cpuMetrics} color="hsl(var(--chart-1))" />
        <MetricsChart title="Cluster Memory" data={memoryMetrics} color="hsl(var(--chart-2))" />
        <MetricsChart title="Gateway RPS" data={rpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />
      </div>

      <Tabs defaultValue="nodes" className="space-y-4">
        <TabsList className="grid w-full grid-cols-2">
          <TabsTrigger value="nodes">
            <Server className="h-4 w-4 mr-2" />
            Cluster Nodes
          </TabsTrigger>
          <TabsTrigger value="logs">
            <FileText className="h-4 w-4 mr-2" />
            Cluster Logs
          </TabsTrigger>
        </TabsList>

        <TabsContent value="nodes" className="space-y-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle>Cluster Nodes</CardTitle>
              <Button
                variant="outline"
                size="sm"
                onClick={() => window.location.reload()}
              >
                <RefreshCw className="h-4 w-4 mr-2" />
                Refresh
              </Button>
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
                              : node.status === 'draining' || node.status === 'drained'
                              ? 'warning'
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
                        <span className="font-bold">{node.allocations_running || 0}</span> / {node.allocations_planned || 0}
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
              <CardTitle>Cluster Logs</CardTitle>
              <div className="flex items-center gap-2">
                {isStreamingRef.current && (
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
                    {isStreamingRef.current ? 'Waiting for cluster logs...' : 'Connecting to cluster log stream...'}
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
