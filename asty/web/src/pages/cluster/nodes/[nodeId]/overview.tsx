import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { LoadingBlock } from '@/components/loading-block'
import {
  Activity,
  Clock,
  Cpu,
  HardDrive,
  HardDriveDownload,
  Layers,
  MemoryStick,
  Signal,
  Skull,
  Wrench,
} from 'lucide-react'
import { toast } from 'sonner'
import { formatMB, formatMHz } from '@/lib/format'
import { nodeStatusSwitchClass } from '@/lib/node-status'
import { MetricsChart } from '@/components/metrics-chart'
import { NodeDrainDialog } from '@/components/node-drain-dialog'
import { NodeHeader } from '@/components/node-header'
import { NodeKillDialog } from '@/components/node-kill-dialog'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { ResourcesBlock } from '@/components/resources-block'
import { Tile } from '@/components/tile'
import { NatsTiles } from '@/features/cluster/nats-tiles'
import { api } from '@/api/client'
import { routes } from '@/lib/routes'
import { useT, nodeStatusKey } from '@/lib/i18n'
import { toastError } from '@/lib/toast'
import { useSubscribe } from '@/lib/use-subscribe'
import { useNodeTabs } from '@/pages/cluster/nodes/[nodeId]/tabs'
import { useClusterStore } from '@/store/cluster'

// confirmNodeGone probes the node a handful of times after a kill whose
// HTTP response failed. Returns true once the node 404s (gone =
// success), false if it's still present after the budget. Errors
// (unreachable) are retried, not treated as gone — we only declare
// success on a definitive absence. ~3s total covers KV-delete
// propagation plus DNS failover onto a surviving node.
async function confirmNodeGone(id: string): Promise<boolean> {
  for (let attempt = 0; attempt < 5; attempt++) {
    try {
      if (!(await api.nodeExists(id))) return true // 404 → gone → success
      // Still present: don't conclude failure yet — a successful kill's
      // KV delete may still be replicating to the node we asked. Keep
      // checking until the budget runs out.
    } catch {
      // couldn't reach a node to ask; wait and retry
    }
    await new Promise((resolve) => setTimeout(resolve, 600))
  }
  return false // still present after ~3s → genuine failure
}

// Node Overview (/nodes/:id) — first tab of the node section.
// Maintenance section (drain switch) + Asty/NATS sub-blocks land
// here; allocations and logs moved to their own routes.
export default function NodeDetail() {
  const t = useT()
  const { nodeId } = useParams<{ nodeId: string }>()
  const navigate = useNavigate()
  const tabs = useNodeTabs(nodeId ?? '')
  const subscribeNode = useClusterStore((s) => s.subscribeNode)
  const updateNodeStatus = useClusterStore((s) => s.updateNodeStatus)
  const cached = useClusterStore((s) => nodeId ? s.nodeCache[nodeId] : undefined)
  const isLastNode = useClusterStore((s) => s.nodes.length <= 1)
  // Kill is refused by the API until the cluster has fully healed from
  // the previous membership change; disable the button to match (the
  // backend 409 stays the authoritative gate).
  const clusterStabilized = useClusterStore((s) => s.clusterStatus?.cluster.stabilized ?? false)
  const node = cached?.node || null
  const cpuMetrics = cached?.cpuMetrics || []
  const memoryMetrics = cached?.memoryMetrics || []
  const rpsMetrics = cached?.rpsMetrics || []
  const [showDrainDialog, setShowDrainDialog] = useState(false)
  const [showKillDialog, setShowKillDialog] = useState(false)

  useSubscribe(subscribeNode, nodeId)

  const handleDrain = async (enable: boolean) => {
    if (!nodeId) return
    try {
      await api.drainNode(nodeId, enable)
      updateNodeStatus(nodeId, enable ? 'draining' : 'ready')
      toast.success(t(enable ? 'toast.drain_started' : 'toast.drain_resumed'))
    } catch (err) {
      toastError(err, t)
    }
  }

  const handleKill = async () => {
    if (!nodeId) return
    try {
      await api.killNode(nodeId, nodeId)
    } catch (err) {
      // Killing the leader removes the very node serving this request
      // and opens a brief leaderless window, so a 5xx/transport error
      // does not mean the kill failed. Judge by state instead: if the
      // node is gone, it worked. Bounded one-shot confirmation (not a
      // steady poll) — a few retries cover KV-delete propagation and
      // the browser landing on a live node via DNS failover.
      if (!(await confirmNodeGone(nodeId))) {
        toastError(err, t, 'toast.kill_failed')
        throw err
      }
    }
    toast.success(t('toast.kill_success', { id: nodeId }))
    navigate(routes.nodes)
  }

  if (!node) {
    return (
      <PageShell bare>
        <Skeleton className="h-8 w-64 mb-4" />
        <LoadingBlock />
      </PageShell>
    )
  }

  const rps = rpsMetrics.length ? rpsMetrics[rpsMetrics.length - 1].value : 0
  const draining = node.status === 'draining' || node.status === 'drained'
  const diskUnit = node.disk_type === 'ssd' || node.disk_type === 'hdd' ? node.disk_type.toUpperCase() : ''
  const drainHint = node.status === 'ready' ? t('drain.normal')
    : node.status === 'draining' ? t('drain.migrating')
    : node.status === 'drained' ? t('drain.drained')
    : t(nodeStatusKey(node.status))

  // hasAsty gates only the agent sub-block — Asty is always present on
  // a running agent, but we still hold rendering until at least one
  // non-zero sample so a fresh node doesn't paint an all-zero block
  // before the first heartbeat. NATS metrics on the other hand always
  // render: an all-zeros reading just means the section is briefly
  // empty (no data yet, or stream just reconnecting), and the zeros
  // themselves are honest "we measured zero" values.
  const hasAsty = node.self_cpu_percent > 0 || node.self_memory_mb > 0 || node.self_disk_mb > 0

  return (
    <PageShell>
      <NodeHeader node={node} />

      <ResourceTabs items={tabs} />

      <section className="space-y-3">
        <div className="grid grid-cols-12 gap-3">
          {/* Resource tiles — five cards in their own subgrid; 12-col
              can't divide by 5 cleanly, so the row owns its own
              grid-cols-5 at lg. */}
          <div className="col-span-12 grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
            <Tile variant="metric"
              title={t('tile.cpu')} icon={<Cpu className="h-4 w-4" />}
              usage={node.cpu_total - node.cpu_available} total={node.cpu_total} format={formatMHz} />
            <Tile variant="metric"
              title={t('tile.ram')} icon={<MemoryStick className="h-4 w-4" />}
              usage={node.memory_total - node.memory_available} total={node.memory_total} format={formatMB} />
            <Tile variant="metric"
              title={t('tile.swap')} icon={<HardDriveDownload className="h-4 w-4" />}
              usage={node.swap_total - node.swap_available} total={node.swap_total} format={formatMB} />
            <Tile variant="metric"
              title={t('tile.disk')} icon={<HardDrive className="h-4 w-4" />}
              usage={node.disk_total - node.disk_available} total={node.disk_total}
              unit={diskUnit} format={formatMB} />
            <Tile variant="stat" bar
              title={t('tile.rps')} icon={<Activity className="h-4 w-4" />}
              value={Math.round(rps)} hint={t('common.requests_per_second')} />
          </div>

          <MetricsChart className="col-span-12 md:col-span-4"
            title={t('chart.node_cpu')} data={cpuMetrics} color="hsl(var(--chart-1))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title={t('chart.node_ram')} data={memoryMetrics} color="hsl(var(--chart-2))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title={t('chart.node_rps')} data={rpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />

          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title={t('tile.allocations')} icon={<Layers className="h-4 w-4" />}
            value={`${node.allocations_running} / ${node.allocations_planned}`} hint={t('tile.hint.running_planned')} />
          <Tile className="col-span-6 lg:col-span-3" variant="timestamp"
            title={t('tile.started_at')} icon={<Clock className="h-4 w-4" />}
            timestamp={node.created_at} />
          <Tile className="col-span-6 lg:col-span-3" variant="timestamp"
            title={t('tile.last_heartbeat')} icon={<Signal className="h-4 w-4" />}
            timestamp={node.last_seen} />
          <Tile className="col-span-6 lg:col-span-3" variant="actions"
            title={t('tile.maintenance')} icon={<Wrench className="h-4 w-4" />}
            actions={
              <div className="flex items-center justify-between w-full">
                <div className="flex flex-col gap-1">
                  <div className="flex items-center gap-2 mb-1">
                    <label htmlFor="drain-switch" className="text-sm font-bold cursor-pointer">
                      {t('drain.dialog.confirm')}
                    </label>
                    <Switch
                      id="drain-switch"
                      size="sm"
                      checked={draining}
                      className={nodeStatusSwitchClass(node.status)}
                      onCheckedChange={(checked) => checked ? setShowDrainDialog(true) : handleDrain(false)}
                      disabled={node.status === 'draining'}
                    />
                  </div>
                  <span className="text-xs text-muted-foreground leading-none">{drainHint}</span>
                </div>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => setShowKillDialog(true)}
                  disabled={!clusterStabilized}
                  title={clusterStabilized ? t('kill.tooltip') : t('kill.unstable')}
                >
                  <Skull />
                  {t('kill.button')}
                </Button>
              </div>
            } />
        </div>
      </section>

      {hasAsty && (
        <ResourcesBlock title="Asty"
          data={{
            cpuUsage: node.self_cpu_percent, cpuTotal: 100,
            memoryUsage: node.self_memory_mb, memoryTotal: node.memory_total,
            diskUsage: node.self_disk_mb, diskTotal: node.disk_total,
          }} />
      )}

      <NatsTiles data={{
        cpuUsage: node.nats_cpu_percent, cpuTotal: 100,
        memoryUsage: node.nats_memory_mb, memoryTotal: node.memory_total,
        diskUsage: node.nats_disk_mb, diskTotal: node.disk_total,
        connections: node.nats_connections,
        subscriptions: node.nats_subscriptions,
        slow: node.nats_slow_consumers,
        inMsgs: node.nats_in_msgs,
        outMsgs: node.nats_out_msgs,
        jsMessages: node.nats_jetstream_messages,
      }} />

      <NodeDrainDialog
        open={showDrainDialog}
        nodeId={node.id}
        onOpenChange={setShowDrainDialog}
        onConfirm={() => { setShowDrainDialog(false); handleDrain(true) }}
      />
      <NodeKillDialog
        open={showKillDialog}
        nodeId={node.id}
        isLastNode={isLastNode}
        onOpenChange={setShowKillDialog}
        onConfirm={handleKill}
      />
    </PageShell>
  )
}
