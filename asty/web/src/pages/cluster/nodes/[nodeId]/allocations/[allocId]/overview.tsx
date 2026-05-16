import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Activity, Clock, Cpu, HardDrive, MemoryStick, RefreshCw, RotateCw, StopCircle, Tag, Wrench } from 'lucide-react'
import { formatMB, formatMHz } from '@/lib/format'
import { toast } from 'sonner'
import { AllocationHeader } from '@/components/allocation-header'
import { ResourceTabs } from '@/components/resource-tabs'
import { Tile } from '@/components/tile'
import { api } from '@/api/client'
import { useClusterStore } from '@/store/cluster'

// Allocation Overview (/nodes/:id/allocations/:allocId) — first tab
// of the allocation section. RPS tile is a placeholder: the backend
// has no per-allocation RPS counter yet.
export default function AllocationDetail() {
  const { nodeId, allocId } = useParams<{ nodeId: string; allocId: string }>()
  const { allocationCache, nodeCache, subscribeAllocation } = useClusterStore()
  const cached = allocId ? allocationCache[allocId] : undefined
  const allocation = cached?.allocation || null
  const svcDef = cached?.service || null
  const node = nodeId ? nodeCache[nodeId]?.node : undefined
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!nodeId || !allocId) return
    return subscribeAllocation(nodeId, allocId)
  }, [nodeId, allocId, subscribeAllocation])

  const cpuTotal = svcDef?.Resources.CPU ?? 100
  const memTotal = svcDef?.Resources.Memory ?? 0
  const diskTotal = node?.disk_total ?? 0
  const uptimeStartedAt = allocation?.status === 'running' ? allocation.started_at : ''
  const diskUnit = node?.disk_type === 'ssd' || node?.disk_type === 'hdd' ? node.disk_type.toUpperCase() : ''

  const act = async (kind: 'restart' | 'stop') => {
    if (!nodeId || !allocId) return
    setBusy(true)
    try {
      await (kind === 'restart' ? api.restartAllocation(nodeId, allocId) : api.stopAllocation(nodeId, allocId))
      toast.success(`${kind === 'restart' ? 'Restarted' : 'Stopped'} allocation`)
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setBusy(false)
    }
  }

  if (!allocation) {
    return (
      <div className="container mx-auto p-4 sm:p-6">
        <Skeleton className="h-8 w-64 mb-4" />
        <Skeleton className="h-32 w-full" />
      </div>
    )
  }

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
      <AllocationHeader allocation={allocation} />

      <ResourceTabs items={[
        { to: `/nodes/${nodeId}/allocations/${allocId}`, label: 'Overview' },
        { to: `/nodes/${nodeId}/allocations/${allocId}/logs`, label: 'Logs' },
      ]} />

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Allocation</h2>

        <div className="grid grid-cols-1 sm:grid-cols-4 gap-3">
          <Tile variant="metric" title="CPU" icon={<Cpu className="h-4 w-4" />}
            usage={allocation.cpu_usage} total={cpuTotal} format={formatMHz} />
          <Tile variant="metric" title="Memory" icon={<MemoryStick className="h-4 w-4" />}
            usage={allocation.memory_usage} total={memTotal} format={formatMB} />
          <Tile variant="metric" title="Disk" icon={<HardDrive className="h-4 w-4" />}
            usage={allocation.disk_usage} total={diskTotal} unit={diskUnit} format={formatMB} />
          <Tile variant="stat" bar title="RPS" icon={<Activity className="h-4 w-4" />}
            value={0} hint="Requests per second" />
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          <Tile variant="stat" size="sm" title="Version" icon={<Tag className="h-4 w-4" />}
            value={allocation.version || '—'}
            hint={`PID: ${allocation.pid || '—'}`} />

          <Tile variant="timestamp" title="Uptime" icon={<Clock className="h-4 w-4" />}
            timestamp={uptimeStartedAt} />

          <Tile variant="stat" title="Restarts" icon={<RefreshCw className="h-4 w-4" />}
            value={allocation.restarts}
            hint={`${allocation.consecutive_failures} consecutive failures`} />

          <Tile variant="actions" title="Maintenance" icon={<Wrench className="h-4 w-4" />}
            actions={
              <div className="flex justify-between w-full">
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button variant="outline" disabled={busy}>
                      <RotateCw className="h-4 w-4" /> Restart
                    </Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>Restart {allocation.service_name}?</AlertDialogTitle>
                      <AlertDialogDescription>The agent will SIGTERM the process and start it again with the same version.</AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>Cancel</AlertDialogCancel>
                      <AlertDialogAction onClick={() => act('restart')}>Restart</AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button variant="destructive" disabled={busy}>
                      <StopCircle className="h-4 w-4" /> Stop
                    </Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>Stop {allocation.service_name}?</AlertDialogTitle>
                      <AlertDialogDescription>The allocation will be terminated and will not auto-restart.</AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>Cancel</AlertDialogCancel>
                      <AlertDialogAction onClick={() => act('stop')}>Stop</AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            } />
        </div>
      </section>
    </div>
  )
}
