import { useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Rocket } from 'lucide-react'
import { toast } from 'sonner'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { ResourceTabs } from '@/components/resource-tabs'
import { api, API_PREFIX } from '@/api/client'
import type { DeploymentRecord, DeploymentsResponse } from '@/types'

const statusVariant = (s: string) =>
  s === 'running' ? 'default'
    : s === 'completed' ? 'default'
    : s === 'failed' || s === 'rollback_failed' ? 'destructive'
    : s === 'reverted' ? 'secondary'
    : 'outline'

// Deploy tab — version input + live progress + history. The progress
// stream is event-driven: subscribes to GET /services/{name}/deploy
// with Accept: text/event-stream and merges incoming DeploymentRecord
// updates into local state. History falls back to a single REST fetch
// on mount + when the live stream flips to a terminal state, so we
// don't need a polling loop.
export default function ServiceDeploy() {
  const { name } = useParams<{ name: string }>()
  const [version, setVersion] = useState('')
  const [deploying, setDeploying] = useState(false)
  const [history, setHistory] = useState<DeploymentRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [live, setLive] = useState<DeploymentRecord | null>(null)
  const liveRef = useRef<DeploymentRecord | null>(null)

  useEffect(() => {
    if (!name) return
    let cancelled = false
    const loadHistory = async () => {
      try {
        const res = await api.getServiceDeployments(name) as DeploymentsResponse
        if (!cancelled) setHistory(res.deployments || [])
      } catch { /* keep current */ } finally {
        if (!cancelled) setLoading(false)
      }
    }
    loadHistory()

    const url = `${API_PREFIX}/services/${name}/deploy`
    const es = new EventSource(url)
    es.addEventListener('progress', (event) => {
      try {
        const rec = JSON.parse((event as MessageEvent).data) as DeploymentRecord
        liveRef.current = rec
        setLive(rec)
        // Terminal state — refresh history so the new record is in the table.
        if (rec.status !== 'running') {
          loadHistory()
        }
      } catch { /* ignore malformed */ }
    })
    return () => {
      cancelled = true
      es.close()
    }
  }, [name])

  const handleDeploy = async () => {
    if (!name || !version) return
    setDeploying(true)
    try {
      await api.deploy(name, version)
      toast.success(`Deploying ${name}@${version}`)
      setVersion('')
    } catch (err) {
      toast.error(`Deploy failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setDeploying(false)
    }
  }

  if (!name) return null
  const latest = live ?? history[0]
  const liveActive = live?.status === 'running'

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <Breadcrumbs items={[
        { label: 'Cluster', to: '/' },
        { label: 'Services', to: '/services' },
        { label: name, to: `/services/${name}` },
        { label: 'Deploy' },
      ]} />
      <ResourceTabs items={[
        { to: `/services/${name}`, label: 'Overview' },
        { to: `/services/${name}/allocations`, label: 'Allocations' },
        { to: `/services/${name}/autoscaler`, label: 'Autoscaler' },
        { to: `/services/${name}/deploy`, label: 'Deploy' },
      ]} />

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
            <Rocket className="h-4 w-4" /> Deploy a new version
          </CardTitle>
        </CardHeader>
        <CardContent className="flex items-center gap-2">
          <Input className="w-64" value={version} onChange={(e) => setVersion(e.target.value)}
            placeholder={latest?.version ? `e.g. ${latest.version}` : 'version tag'} />
          <Button onClick={handleDeploy} disabled={deploying || !version || liveActive}>
            {liveActive ? 'In progress…' : deploying ? 'Deploying…' : 'Deploy'}
          </Button>
        </CardContent>
      </Card>

      {live && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-muted-foreground">Live deploy</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="flex items-center gap-2 text-sm">
              <span className="font-mono">{live.version}</span>
              <Badge variant={statusVariant(live.status)}>{live.status}</Badge>
              <span className="text-muted-foreground">{live.strategy}</span>
            </div>
            <Progress value={live.progress} />
            {live.rollback_steps && live.rollback_steps.length > 0 && (
              <div className="text-xs text-muted-foreground">
                Rollback steps: {live.rollback_steps.length}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium text-muted-foreground">History</CardTitle>
        </CardHeader>
        <CardContent>
          {loading && history.length === 0 ? (
            <Skeleton className="h-32 w-full" />
          ) : history.length === 0 ? (
            <div className="text-center text-muted-foreground py-8">No deployments recorded.</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Started</TableHead>
                  <TableHead>Version</TableHead>
                  <TableHead>Strategy</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Progress</TableHead>
                  <TableHead>Completed</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {history.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell className="text-sm">{new Date(r.started_at).toLocaleString()}</TableCell>
                    <TableCell className="font-mono text-xs">{r.version}</TableCell>
                    <TableCell className="text-xs">{r.strategy}</TableCell>
                    <TableCell><Badge variant={statusVariant(r.status)}>{r.status}</Badge></TableCell>
                    <TableCell>{r.progress}%</TableCell>
                    <TableCell className="text-sm">
                      {r.completed_at ? new Date(r.completed_at).toLocaleString() : '—'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
