import { useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Layers, Heart, Cpu, MemoryStick, Rocket } from 'lucide-react'
import { toast } from 'sonner'
import { MetricsChart } from '@/components/metrics-chart'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { ServiceHeader } from '@/components/service-header'
import { Tile } from '@/components/tile'
import { ServiceConfigCard } from '@/features/services/service-config-card'
import { ServiceMinCopiesCard } from '@/features/services/service-min-copies-card'
import { api } from '@/api/client'
import { formatMB, formatMHz } from '@/lib/format'
import { serviceTabs } from '@/pages/services/[name]/tabs'
import { useClusterStore } from '@/store/cluster'

// Service Overview (/services/:name). All per-service data —
// snapshot, autoscaler info, scaling events, live deploy — flows
// through one subscribeService(name) that the cluster store opens; this
// page just reads from serviceCache[name]. The sibling tabs (Scaling
// events / Deploy history) read from the same cache so nothing is
// polled twice.
export default function ServiceOverview() {
  const { name } = useParams<{ name: string }>()
  const { serviceCache, subscribeService, refreshService, services: allServices } = useClusterStore()
  const cached = name ? serviceCache[name] : undefined
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
  const [version, setVersion] = useState('latest')
  const [deploying, setDeploying] = useState(false)

  useEffect(() => {
    if (!name) return
    return subscribeService(name)
  }, [name, subscribeService])

  const runtime = useMemo(() => allServices.find((s) => s.Name === name) || null, [name, allServices])
  const running = allocations.filter((a) => a.status === 'running').length
  const healthy = allocations.filter((a) => a.status === 'running' && a.health_status === 'healthy').length
  const healthPct = allocations.length > 0 ? Math.round((healthy / allocations.length) * 100) : 0
  const liveActive = live?.status === 'running'

  // Available versions for the Deploy select. Order:
  //   1. "latest" — always first, GitHub Release alias + sensible
  //      default that operators reach for most often.
  //   2. GitHub Releases tags (cached server-side, falls back to
  //      empty list in dev or when A_GITHUB_REPO is unset).
  //   3. Versions seen in this service's deploy history.
  //   4. Versions currently running on any allocation.
  // Deduplicated while preserving the first appearance.
  const availableVersions = useMemo(() => {
    const seen = new Set<string>()
    const out: string[] = []
    const add = (v: string) => {
      if (!v || seen.has(v)) return
      seen.add(v)
      out.push(v)
    }
    add('latest')
    githubVersions.forEach(add)
    deployHistory.forEach((r) => add(r.version))
    allocations.forEach((a) => add(a.version))
    return out
  }, [githubVersions, deployHistory, allocations])

  const handleDeploy = async () => {
    if (!name || !version) return
    setDeploying(true)
    try {
      await api.deploy(name, version)
      toast.success(`Deploying ${name}@${version}`)
      setVersion('latest')
      await refreshService(name)
    } catch (err) {
      toast.error(`Deploy failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setDeploying(false)
    }
  }

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

          <Card className={service.Type === 'service' ? 'col-span-12 lg:col-span-4' : 'col-span-12 lg:col-span-4 lg:row-span-2'}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Deploy</CardTitle>
              <Rocket className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent className="space-y-2">
              <div className="flex items-center gap-2 mt-2 mb-4">
                <Select value={version} onValueChange={setVersion}>
                  <SelectTrigger className="flex-1">
                    <SelectValue placeholder="version tag" />
                  </SelectTrigger>
                  <SelectContent>
                    {availableVersions.map((v) => (
                      <SelectItem key={v} value={v}>{v}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button onClick={handleDeploy} disabled={deploying || !version || liveActive}>
                  {liveActive ? 'In progress…' : deploying ? 'Deploying…' : 'Deploy'}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                Rolling update per <code className="font-mono">update</code> policy
                (optional canary → batches of <code className="font-mono">max_parallel</code>).
                Autoscaler paused for the rollout; auto-reverts on failure if
                <code className="font-mono"> auto_revert</code> is enabled.
              </p>
            </CardContent>
          </Card>
        </div>
      )}
    </PageShell>
  )
}
