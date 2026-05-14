import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Switch } from '@/components/ui/switch'
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
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Clock, Layers, Wrench } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { toast } from 'sonner'
import { MetricsChart } from '@/components/metrics-chart'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { ResourceTabs } from '@/components/resource-tabs'
import { ResourcesBlock } from '@/components/resources-block'
import { StatusTile } from '@/components/status-tile'
import { api } from '@/api/client'
import { useClusterStore } from '@/store/cluster'

const statusVariant = (s?: string): 'default' | 'secondary' | 'destructive' | 'outline' => {
  if (s === 'ready') return 'default'
  if (s === 'down') return 'destructive'
  if (s === 'draining' || s === 'drained' || s === 'paused') return 'secondary'
  return 'outline'
}

// Node Overview (/nodes/:id) — first tab of the node section.
// Maintenance section (drain switch) + Asty/NATS sub-blocks land
// here; allocations and logs moved to their own routes.
export default function NodeDetail() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const { nodeCache, subscribeNode, updateNodeStatus } = useClusterStore()
  const cached = nodeId ? nodeCache[nodeId] : undefined
  const node = cached?.node || null
  const cpuMetrics = cached?.cpuMetrics || []
  const memoryMetrics = cached?.memoryMetrics || []
  const rpsMetrics = cached?.rpsMetrics || []
  const [showDrainDialog, setShowDrainDialog] = useState(false)

  useEffect(() => {
    if (!nodeId) return
    return subscribeNode(nodeId)
  }, [nodeId, subscribeNode])

  const handleDrain = async (enable: boolean) => {
    if (!nodeId) return
    try {
      await api.drainNode(nodeId, enable)
      updateNodeStatus(nodeId, enable ? 'draining' : 'ready')
      toast.success(enable ? 'Draining node' : 'Node resumed')
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    }
  }

  if (!node) {
    return (
      <div className="container mx-auto p-4 sm:p-6">
        <Skeleton className="h-8 w-64 mb-4" />
        <Skeleton className="h-32 w-full" />
      </div>
    )
  }

  const rps = rpsMetrics.length ? rpsMetrics[rpsMetrics.length - 1].value : 0
  const draining = node.status === 'draining' || node.status === 'drained'

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <Breadcrumbs items={[
            { label: 'Cluster', to: '/' },
            { label: 'Nodes', to: '/nodes' },
            { label: node.id },
          ]} />
          <h1 className="text-2xl font-bold mt-1">{node.id} <Badge variant={statusVariant(node.status)}>{node.status}</Badge></h1>
        </div>
      </div>

      <ResourceTabs items={[
        { to: `/nodes/${node.id}`, label: 'Overview' },
        { to: `/nodes/${node.id}/allocations`, label: 'Allocations' },
        { to: `/nodes/${node.id}/logs`, label: 'Logs' },
      ]} />

      <ResourcesBlock
        title="Node"
        data={{
          cpuUsage: node.cpu_total - node.cpu_available,
          cpuTotal: node.cpu_total,
          memoryUsage: node.memory_total - node.memory_available,
          memoryTotal: node.memory_total,
          diskUsage: node.disk_total - node.disk_available,
          diskTotal: node.disk_total,
          rps,
        }}
      />

      <div className="grid gap-3 md:grid-cols-3">
        <MetricsChart title="Node CPU" data={cpuMetrics} color="hsl(var(--chart-1))" />
        <MetricsChart title="Node Memory" data={memoryMetrics} color="hsl(var(--chart-2))" />
        <MetricsChart title="Node RPS" data={rpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />
      </div>

      <div className="grid gap-3 grid-cols-2 lg:grid-cols-3">
        <StatusTile title="Allocations" icon={<Layers className="h-4 w-4" />}
          value={`${node.allocations_running} / ${node.allocations_planned}`} hint="running / planned" />
        <StatusTile title="Started At" icon={<Clock className="h-4 w-4" />}
          value={node.created_at ? formatDistanceToNow(new Date(node.created_at), { addSuffix: true }) : '—'}
          hint={node.created_at || ''} />
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
              <Wrench className="h-4 w-4" /> Maintenance
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-sm">Drain</span>
              <Switch checked={draining}
                onCheckedChange={(checked) => checked ? setShowDrainDialog(true) : handleDrain(false)} />
            </div>
            <div className="text-xs text-muted-foreground">
              {draining ? 'Allocations migrating to peers' : 'Accepting new allocations'}
            </div>
          </CardContent>
        </Card>
      </div>

      <ResourcesBlock title="Asty (agent on this node)"
        data={{
          cpuUsage: node.self_cpu_percent, cpuTotal: 100,
          memoryUsage: node.self_memory_mb, memoryTotal: node.memory_total,
          diskUsage: node.self_disk_mb, diskTotal: node.disk_total,
        }} />

      <ResourcesBlock title="NATS (local server)"
        data={{
          cpuUsage: node.nats_cpu_percent, cpuTotal: 100,
          memoryUsage: node.nats_memory_mb, memoryTotal: node.memory_total,
          diskUsage: Math.round(node.nats_jetstream_bytes / (1024 * 1024)), diskTotal: node.disk_total,
        }} />
      <p className="text-xs text-muted-foreground">
        NATS connections: {node.nats_connections} · subs: {node.nats_subscriptions} · slow consumers: {node.nats_slow_consumers}
      </p>

      <AlertDialog open={showDrainDialog} onOpenChange={setShowDrainDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Drain node {node.id}?</AlertDialogTitle>
            <AlertDialogDescription>
              All running allocations on this node will be migrated to peers and the node will stop accepting new placements.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => { setShowDrainDialog(false); handleDrain(true) }}>
              Drain
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
