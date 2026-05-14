import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
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
import { api } from '@/api/client'
import type { DeploymentRecord, DeploymentsResponse } from '@/types'

const statusVariant = (s: string) =>
  s === 'running' ? 'default' : s === 'completed' ? 'default' :
  s === 'failed' ? 'destructive' : s === 'reverted' ? 'secondary' : 'outline'

// Deploy tab — version input + history. The placeholder text in the
// version input comes from the latest record so the operator sees
// what's running before typing the next tag (model: "подставляем
// версию для деплоя из доступных").
export default function ServiceDeploy() {
  const { name } = useParams<{ name: string }>()
  const [version, setVersion] = useState('')
  const [deploying, setDeploying] = useState(false)
  const [history, setHistory] = useState<DeploymentRecord[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!name) return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null
    const poll = async () => {
      try {
        const res = await api.getServiceDeployments(name) as DeploymentsResponse
        if (!cancelled) setHistory(res.deployments || [])
      } catch { /* keep current */ }
      if (!cancelled) {
        setLoading(false)
        timer = setTimeout(poll, 10000)
      }
    }
    poll()
    return () => { cancelled = true; if (timer) clearTimeout(timer) }
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
  const latest = history[0]
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
          <Button onClick={handleDeploy} disabled={deploying || !version}>
            {deploying ? 'Deploying…' : 'Deploy'}
          </Button>
        </CardContent>
      </Card>

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
