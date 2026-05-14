import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
import { Clock, Tag, Hash, Heart, HardDrive, RefreshCw, RotateCw, StopCircle, Wrench } from 'lucide-react'
import { formatMB } from '@/lib/format'
import { uptimeLabel } from '@/lib/uptime'
import { toast } from 'sonner'
import { AllocationHeader } from '@/components/allocation-header'
import { ResourceTabs } from '@/components/resource-tabs'
import { ResourcesBlock } from '@/components/resources-block'
import { StatusTile } from '@/components/status-tile'
import { api } from '@/api/client'
import { useClusterStore } from '@/store/cluster'
import type { Allocation } from '@/types'

const healthVariant = (h?: Allocation['health_status']) =>
  h === 'healthy' ? 'success' : h === 'unhealthy' ? 'destructive' : 'secondary'

// Allocation Overview (/nodes/:id/allocations/:allocId) — first tab
// of the allocation section. RPS tile is absent: the backend has no
// per-allocation RPS counter; if/when Phase E adds the
// infrastructure, ResourcesBlock will surface it automatically.
export default function AllocationDetail() {
  const { nodeId, allocId } = useParams<{ nodeId: string; allocId: string }>()
  const { allocationCache, subscribeAllocation } = useClusterStore()
  const cached = allocId ? allocationCache[allocId] : undefined
  const allocation = cached?.allocation || null
  const svcDef = cached?.service || null
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!nodeId || !allocId) return
    return subscribeAllocation(nodeId, allocId)
  }, [nodeId, allocId, subscribeAllocation])

  const cpuTotal = svcDef?.Resources.CPU ?? 100
  const memTotal = svcDef?.Resources.Memory ?? 0

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

      <ResourcesBlock
        title="Allocation"
        data={{
          cpuUsage: allocation.cpu_usage,
          cpuTotal: cpuTotal,
          memoryUsage: allocation.memory_usage,
          memoryTotal: memTotal,
        }}
      />

      <div className="grid gap-3 grid-cols-2 lg:grid-cols-6">
        <StatusTile title="Disk" icon={<HardDrive className="h-4 w-4" />}
          value={formatMB(allocation.disk_usage)} hint="on-disk under work_dir" />
        <StatusTile title="Version" icon={<Tag className="h-4 w-4" />}
          value={<span className="text-base font-mono">{allocation.version || '—'}</span>} />
        <StatusTile title="PID" icon={<Hash className="h-4 w-4" />}
          value={<span className="text-base font-mono">{allocation.pid || '—'}</span>} />
        <StatusTile title="Health" icon={<Heart className="h-4 w-4" />}
          value={<Badge variant={healthVariant(allocation.health_status)}>{allocation.health_status || 'unknown'}</Badge>} />
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Uptime</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-sm font-bold mt-1 mb-2">
              {uptimeLabel(allocation.started_at, allocation.status)}
            </div>
            {allocation.started_at && !allocation.started_at.startsWith('0001-') && (
              <p className="text-xs text-muted-foreground">
                {new Date(allocation.started_at).toLocaleString()}
              </p>
            )}
          </CardContent>
        </Card>
        <StatusTile title="Restarts" icon={<RefreshCw className="h-4 w-4" />}
          value={allocation.restarts}
          hint={`${allocation.consecutive_failures} consecutive failures`} />
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
            <Wrench className="h-4 w-4" /> Maintenance
          </CardTitle>
        </CardHeader>
        <CardContent className="flex gap-2">
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button variant="outline" disabled={busy}>
                <RotateCw className="h-4 w-4 mr-2" /> Restart
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
                <StopCircle className="h-4 w-4 mr-2" /> Stop
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
        </CardContent>
      </Card>
    </div>
  )
}
