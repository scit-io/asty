import { useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Layers, Activity, Cpu, MemoryStick } from 'lucide-react'
import { toast } from 'sonner'
import { MetricsChart } from '@/components/metrics-chart'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { ResourceTabs } from '@/components/resource-tabs'
import { Tile } from '@/components/tile'
import { api } from '@/api/client'
import { formatMB, formatMHz } from '@/lib/format'
import { useClusterStore } from '@/store/cluster'

// Service Overview (/services/:name) — the Overview tab of the
// service section. Resource budget tiles + scaling status + per-
// service charts. Allocations/Autoscaler/Deploy are siblings now;
// the embedded Tabs from the old page have been pulled apart.
export default function ServiceOverview() {
  const { name } = useParams<{ name: string }>()
  const { serviceCache, subscribeService, services: allServices } = useClusterStore()
  const cached = name ? serviceCache[name] : undefined
  const service = cached?.service || null
  const allocations = cached?.allocations || []
  const cpuMetrics = cached?.cpuMetrics || []
  const memoryMetrics = cached?.memoryMetrics || []
  const allocCountMetrics = cached?.allocCountMetrics || []
  const [scaleTo, setScaleTo] = useState('')
  const [scaling, setScaling] = useState(false)

  useEffect(() => {
    if (!name) return
    return subscribeService(name)
  }, [name, subscribeService])

  const runtime = useMemo(() => allServices.find((s) => s.Name === name) || null, [name, allServices])
  const running = allocations.filter((a) => a.status === 'running').length

  const handleScale = async () => {
    if (!name || !scaleTo) return
    const n = parseInt(scaleTo, 10)
    if (Number.isNaN(n) || n < 0) {
      toast.error('Enter a non-negative integer')
      return
    }
    setScaling(true)
    try {
      await api.scaleService(name, n)
      toast.success(`Set ${name} floor to ${n}`)
      setScaleTo('')
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setScaling(false)
    }
  }

  if (!name) return null

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <Breadcrumbs items={[
          { label: 'Cluster', to: '/' },
          { label: 'Services', to: '/services' },
          { label: name },
        ]} />
        <div className="flex items-center gap-3">
          <h1 className="text-2xl sm:text-3xl font-bold">{name}</h1>
          {service && <Badge variant={service.Type === 'system' ? 'secondary' : 'default'}>{service.Type}</Badge>}
        </div>
      </div>

      <ResourceTabs items={[
        { to: `/services/${name}`, label: 'Overview' },
        { to: `/services/${name}/allocations`, label: 'Allocations' },
        { to: `/services/${name}/autoscaler`, label: 'Autoscaler' },
        { to: `/services/${name}/deploy`, label: 'Deploy' },
      ]} />

      {!service ? (
        <Skeleton className="h-32 w-full" />
      ) : (
        <>
          <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
            <Tile variant="stat" title="Copies" icon={<Layers className="h-4 w-4" />}
              value={`${running} / ${allocations.length}`}
              hint={service.Type === 'service' && runtime?.min_copies !== undefined ? `min ${runtime.min_copies}` : 'running / total'} />
            <Tile variant="stat" title="CPU budget" icon={<Cpu className="h-4 w-4" />}
              value={formatMHz(service.Resources.CPU)} hint="per allocation" />
            <Tile variant="stat" title="Memory budget" icon={<MemoryStick className="h-4 w-4" />}
              value={formatMB(service.Resources.Memory)} hint="per allocation" />
            <Tile variant="stat" title="Health check" icon={<Activity className="h-4 w-4" />}
              value={service.Health.Type || 'none'}
              hint={service.Health.Path || ''} />
          </div>

          <div className="grid gap-3 md:grid-cols-3">
            <MetricsChart title="Avg CPU%" data={cpuMetrics} color="hsl(var(--chart-1))" />
            <MetricsChart title="Avg Memory" data={memoryMetrics} color="hsl(var(--chart-2))" unit=" Mb" />
            <MetricsChart title="Running allocations" data={allocCountMetrics} color="hsl(var(--chart-3))" unit="" />
          </div>

          {service.Type === 'service' && (
            <Card>
              <CardHeader>
                <CardTitle className="text-sm font-medium text-muted-foreground">Min copies (floor)</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                <div className="flex items-center gap-2">
                  <Input className="w-32" type="number" min={0}
                    placeholder="copies" value={scaleTo}
                    onChange={(e) => setScaleTo(e.target.value)} />
                  <Button onClick={handleScale} disabled={scaling || !scaleTo}>
                    {scaling ? 'Saving…' : 'Set floor'}
                  </Button>
                  <span className="text-xs text-muted-foreground">
                    Current: {runtime?.current_copies ?? 0} · floor: {runtime?.min_copies ?? 0}
                  </span>
                </div>
                <p className="text-xs text-muted-foreground">
                  Sets the per-service minimum copy count. The autoscaler still grows
                  above this in response to traffic or resource pressure; lowering the
                  floor below the current copy count stops the excess copies immediately.
                </p>
              </CardContent>
            </Card>
          )}
        </>
      )}
    </div>
  )
}
