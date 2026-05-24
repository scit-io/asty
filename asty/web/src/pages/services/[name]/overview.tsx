import { useMemo } from 'react'
import { useParams } from 'react-router-dom'
import { Skeleton } from '@/components/ui/skeleton'
import { Layers, Heart, Cpu, MemoryStick } from 'lucide-react'
import { MetricsChart } from '@/components/metrics-chart'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { ServiceHeader } from '@/components/service-header'
import { Tile } from '@/components/tile'
import { ServiceConfigCard } from '@/features/services/service-config-card'
import { ServiceDeployCard } from '@/features/services/service-deploy-card'
import { ServiceMinCopiesCard } from '@/features/services/service-min-copies-card'
import { formatMB, formatMHz } from '@/lib/format'
import { useSubscribe } from '@/lib/use-subscribe'
import { serviceTabs } from '@/pages/services/[name]/tabs'
import { useClusterStore } from '@/store/cluster'

// Service Overview (/services/:name). All per-service data —
// snapshot, autoscaler info, scaling events, live deploy — flows
// through one subscribeService(name) that the cluster store opens; this
// page just reads from serviceCache[name]. The sibling tabs (Scaling
// events / Deploy history) read from the same cache so nothing is
// polled twice. Page-level logic is layout + the four cards
// (Configuration / Min copies / Deploy) — each card owns its own
// form state and handlers.
export default function ServiceOverview() {
  const { name } = useParams<{ name: string }>()
  const subscribeService = useClusterStore((s) => s.subscribeService)
  const refreshService = useClusterStore((s) => s.refreshService)
  const allServices = useClusterStore((s) => s.services)
  const cached = useClusterStore((s) => name ? s.serviceCache[name] : undefined)
  const service = cached?.service || null
  const allocations = cached?.allocations || []
  const cpuMetrics = cached?.cpuMetrics || []
  const memoryMetrics = cached?.memoryMetrics || []
  const allocCountMetrics = cached?.allocCountMetrics || []
  const autoscaler = cached?.autoscaler || null
  const latestEvent = cached?.scalingEvents?.[0] ?? null
  const live = cached?.liveDeploy ?? null
  const deployHistory = cached?.deployHistory ?? []
  const latestDeploy = deployHistory[0] ?? null
  const githubVersions = cached?.availableVersions ?? []

  useSubscribe(subscribeService, name)

  const runtime = useMemo(() => allServices.find((s) => s.Name === name) || null, [name, allServices])
  const running = allocations.filter((a) => a.status === 'running').length
  const healthy = allocations.filter((a) => a.status === 'running' && a.health_status === 'healthy').length
  const healthPct = allocations.length > 0 ? Math.round((healthy / allocations.length) * 100) : 0
  const liveActive = live?.status === 'running'

  if (!name) return null

  return (
    <PageShell>
      <ServiceHeader name={name} service={service} />
      <ResourceTabs items={serviceTabs(name)} />

      {!service ? (
        <Skeleton className="h-32 w-full" />
      ) : (
        <div className="grid grid-cols-12 gap-3">
          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title="Copies" icon={<Layers className="h-4 w-4" />}
            value={`${running} / ${allocations.length}`}
            hint={service.Type === 'service' && runtime?.min_copies !== undefined ? `min ${runtime.min_copies}` : 'running / total'} />
          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title="CPU budget" icon={<Cpu className="h-4 w-4" />}
            value={formatMHz(service.Resources.CPU)} hint="per allocation" />
          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title="Memory budget" icon={<MemoryStick className="h-4 w-4" />}
            value={formatMB(service.Resources.Memory)} hint="per allocation" />
          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title="Health" icon={<Heart className="h-4 w-4" />}
            value={`${healthPct}%`}
            hint={`${healthy} of ${allocations.length} healthy`} />

          <MetricsChart className="col-span-12 md:col-span-4"
            title="CPU% per copy" data={cpuMetrics} color="hsl(var(--chart-1))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title="Memory per copy" data={memoryMetrics} color="hsl(var(--chart-2))" unit=" Mb" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title="Running allocations" data={allocCountMetrics} color="hsl(var(--chart-3))" unit="" />

          <ServiceConfigCard
            className="col-span-12 lg:col-span-8 lg:row-span-2"
            runtime={runtime}
            autoscaler={autoscaler}
            latestDeploy={latestDeploy}
            latestEvent={latestEvent}
          />

          {service.Type === 'service' && (
            <ServiceMinCopiesCard
              className="col-span-12 lg:col-span-4"
              name={name}
              currentCopies={runtime?.current_copies}
              onChanged={() => refreshService(name)}
            />
          )}

          <ServiceDeployCard
            className={service.Type === 'service' ? 'col-span-12 lg:col-span-4' : 'col-span-12 lg:col-span-4 lg:row-span-2'}
            name={name}
            githubVersions={githubVersions}
            deployHistory={deployHistory}
            allocations={allocations}
            liveActive={liveActive}
            onChanged={() => refreshService(name)}
          />
        </div>
      )}
    </PageShell>
  )
}
