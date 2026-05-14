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
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Clock, HelpCircle, Layers, Wrench } from 'lucide-react'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatDistanceToNow } from 'date-fns'
import { toast } from 'sonner'
import { MetricsChart } from '@/components/metrics-chart'
import { NodeHeader } from '@/components/node-header'
import { ResourceTabs } from '@/components/resource-tabs'
import { ResourcesBlock } from '@/components/resources-block'
import { StatusTile } from '@/components/status-tile'
import { api } from '@/api/client'
import { useClusterStore } from '@/store/cluster'

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
      <NodeHeader node={node} />

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
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Started At</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-sm font-bold mt-1 mb-2">
              {node.created_at ? formatDistanceToNow(new Date(node.created_at), { addSuffix: true }) : '—'}
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
            <CardTitle className="text-sm font-medium">Maintenance</CardTitle>
            <Wrench className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2 mt-1 mb-2">
              <div className="text-sm font-bold">Drain</div>
              <Switch
                checked={draining}
                onCheckedChange={(checked) => checked ? setShowDrainDialog(true) : handleDrain(false)}
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
              {node.status === 'ready' ? 'Normal'
                : node.status === 'draining' ? 'Migrating…'
                : node.status === 'drained' ? 'Drained'
                : node.status}
            </p>
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
