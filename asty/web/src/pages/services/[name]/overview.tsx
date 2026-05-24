import { useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Layers, Heart, Cpu, MemoryStick, Rocket, Settings2, Scaling } from 'lucide-react'
import {
  Table,
  TableBody,
  TableCell,
  TableRow,
} from '@/components/ui/table'
import { toast } from 'sonner'
import { MetricsChart } from '@/components/metrics-chart'
import { ResourceTabs } from '@/components/resource-tabs'
import { ServiceHeader } from '@/components/service-header'
import { Tile } from '@/components/tile'
import { api } from '@/api/client'
import { formatMB, formatMHz } from '@/lib/format'
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
  // Pick the more recent of "latest scaling event" vs "latest deploy
  // record" so Last action reflects whichever happened most recently —
  // operator-driven scale, autoscaler decision, or a deploy roll. The
  // same comparison is mirrored server-side in snapshot.go for the
  // /services list view; here we redo it because Overview already has
  // both ring + history loaded into the per-service cache.
  const latestEventTs = latestEvent ? latestEvent.timestamp * 1000 : 0
  const latestDeploy = deployHistory[0] ?? null
  const latestDeployTs = latestDeploy ? new Date(latestDeploy.started_at).getTime() : 0
  const deployIsLatest = latestDeploy !== null && latestDeployTs >= latestEventTs
  const githubVersions = cached?.availableVersions ?? []
  const [scaleTo, setScaleTo] = useState('')
  const [scaling, setScaling] = useState(false)
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

  // Default the Min copies input to the live current_copies once the
  // runtime first arrives; after that the field is owned by the
  // operator so the autoscaler bumping current_copies up/down doesn't
  // clobber whatever they're typing.
  const minCopiesInitialized = useRef(false)
  useEffect(() => {
    if (!minCopiesInitialized.current && runtime?.current_copies !== undefined) {
      setScaleTo(String(runtime.current_copies))
      minCopiesInitialized.current = true
    }
  }, [runtime?.current_copies])

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
      await refreshService(name)
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setScaling(false)
    }
  }

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
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <ServiceHeader name={name} service={service} />

      <ResourceTabs items={[
        { to: `/services/${name}`, label: 'Overview' },
        { to: `/services/${name}/allocations`, label: 'Allocations' },
        { to: `/services/${name}/autoscaler`, label: 'Scaling events' },
        { to: `/services/${name}/deploy`, label: 'Deploy history' },
      ]} />

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

          <Card className="col-span-12 lg:col-span-8 lg:row-span-2">
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Configuration</CardTitle>
                <Settings2 className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                {!runtime ? (
                  <Skeleton className="h-32 w-full" />
                ) : (
                  <Table>
                    <TableBody>
                      <TableRow>
                        <TableCell className="text-muted-foreground px-0 py-2">Current copies</TableCell>
                        <TableCell className="font-mono text-right px-0 py-2">{runtime.current_copies ?? 0}</TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell className="text-muted-foreground px-0 py-2">Min copies (floor)</TableCell>
                        <TableCell className="font-mono text-right px-0 py-2">
                          <span className="inline-flex items-center gap-2 justify-end">
                            {autoscaler?.min_copies ?? runtime.min_copies ?? 0}
                            {autoscaler?.min_copies_override && (
                              <Badge variant="secondary" className="font-sans text-[10px]">
                                overridden (default {autoscaler.min_copies_default})
                              </Badge>
                            )}
                          </span>
                        </TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell className="text-muted-foreground px-0 py-2">Max copies (ceiling)</TableCell>
                        <TableCell className="font-mono text-right px-0 py-2">
                          {autoscaler?.max_copies && autoscaler.max_copies > 0 ? autoscaler.max_copies : 'unlimited'}
                        </TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell className="text-muted-foreground px-0 py-2">Target CPU</TableCell>
                        <TableCell className="font-mono text-right px-0 py-2">{runtime.target_cpu ?? 0}%</TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell className="text-muted-foreground px-0 py-2">Target Memory</TableCell>
                        <TableCell className="font-mono text-right px-0 py-2">{runtime.target_memory ?? 0}%</TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell className="text-muted-foreground px-0 py-2">Traffic threshold</TableCell>
                        <TableCell className="font-mono text-right px-0 py-2">{runtime.traffic_threshold ?? 0} RPS</TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell className="text-muted-foreground px-0 py-2">Cooldown</TableCell>
                        <TableCell className="text-right px-0 py-2">
                          <span className="inline-flex gap-1 justify-end">
                            {runtime.cooldown_up_active && <Badge variant="secondary">up</Badge>}
                            {runtime.cooldown_down_active && <Badge variant="secondary">down</Badge>}
                            {autoscaler?.deploy_in_progress && <Badge variant="secondary">deploy</Badge>}
                            {!runtime.cooldown_up_active && !runtime.cooldown_down_active && !autoscaler?.deploy_in_progress &&
                              <span className="text-muted-foreground">inactive</span>}
                          </span>
                        </TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell className="text-muted-foreground px-0 py-2">Last action</TableCell>
                        <TableCell className="text-right text-sm px-0 py-2">
                          {deployIsLatest && latestDeploy ? (() => {
                            const d = new Date(latestDeploy.started_at)
                            return (
                              <span className="inline-flex items-center gap-2 justify-end">
                                <span>deploy <span className="font-mono">{latestDeploy.version}</span></span>
                                <Badge variant={latestDeploy.status === 'failed' || latestDeploy.status === 'rollback_failed' ? 'destructive' : latestDeploy.status === 'reverted' ? 'secondary' : 'default'} className="text-[10px]">
                                  {latestDeploy.status}
                                </Badge>
                                <span className="text-muted-foreground">·</span>
                                <span>{d.toLocaleTimeString()} · <span className="text-muted-foreground">{d.toLocaleDateString()}</span></span>
                              </span>
                            )
                          })() : latestEvent ? (() => {
                            const d = new Date(latestEvent.timestamp * 1000)
                            return (
                              <span className="inline-flex items-center gap-2 justify-end">
                                <span>{latestEvent.action === 'scale_up' ? 'scale up' : 'scale down'}</span>
                                {latestEvent.reason?.startsWith('manual:') && (
                                  <Badge variant="outline" className="text-[10px]">manual</Badge>
                                )}
                                <span className="text-muted-foreground">·</span>
                                <span>{d.toLocaleTimeString()} · <span className="text-muted-foreground">{d.toLocaleDateString()}</span></span>
                              </span>
                            )
                          })() : runtime.last_action ? (() => {
                            const d = runtime.last_action_at ? new Date(runtime.last_action_at * 1000) : null
                            return (
                              <span>
                                {runtime.last_action}
                                {d && <> · {d.toLocaleTimeString()} · <span className="text-muted-foreground">{d.toLocaleDateString()}</span></>}
                              </span>
                            )
                          })() : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </TableCell>
                      </TableRow>
                    </TableBody>
                  </Table>
                )}
              </CardContent>
            </Card>

          {service.Type === 'service' && (
            <Card className="col-span-12 lg:col-span-4">
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Min copies</CardTitle>
                <Scaling className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent className="space-y-2">
                <div className="flex items-center gap-2 mt-2 mb-4">
                  <Input className="flex-1" type="number" min={0}
                    placeholder="copies" value={scaleTo}
                    onChange={(e) => setScaleTo(e.target.value)} />
                  <Button onClick={handleScale} disabled={scaling || !scaleTo}>
                    {scaling ? 'Saving…' : 'Set floor'}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">
                  Per-service floor. Autoscaler can grow above; lowering stops
                  excess copies immediately.
                </p>
              </CardContent>
            </Card>
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
    </div>
  )
}
