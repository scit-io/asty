import { useState, useEffect } from 'react'
import { api } from '@/api/client'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Rocket, Package } from 'lucide-react'
import { useClusterStore } from '@/store/cluster'
import type { DeploymentRecord } from '@/types'

export default function Deploy() {
  const services = useClusterStore((s) => s.services)
  const [deployments, setDeployments] = useState<DeploymentRecord[]>([])
  const [selectedService, setSelectedService] = useState('')
  const [version, setVersion] = useState('')
  const [deploying, setDeploying] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null)

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false

    const fetchData = async () => {
      try {
        const depRes = await api.getDeployments().catch(() => ({ deployments: [], count: 0 }))
        if (cancelled) return
        setDeployments(depRes.deployments || [])
      } catch { /* keep current */ }
      if (!cancelled) timer = setTimeout(fetchData, 10000)
    }

    fetchData()
    return () => { cancelled = true; if (timer) clearTimeout(timer) }
  }, [])

  const handleDeploy = async () => {
    if (!selectedService || !version) return
    setDeploying(true)
    setResult(null)

    try {
      await api.deploy(selectedService, version)
      setResult({ ok: true, message: `Deployment of ${selectedService}@${version} initiated` })
      setVersion('')
    } catch (err) {
      setResult({ ok: false, message: `Failed: ${err instanceof Error ? err.message : 'unknown error'}` })
    } finally {
      setDeploying(false)
    }
  }

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Rocket className="h-5 w-5" />
            Deploy Service
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="space-y-2">
              <label className="text-sm font-medium">Service</label>
              <select
                value={selectedService}
                onChange={(e) => setSelectedService(e.target.value)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              >
                <option value="">Select service...</option>
                {services.map((svc) => (
                  <option key={svc.Name} value={svc.Name}>
                    {svc.Name} ({svc.Type})
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">Version</label>
              <input
                type="text"
                value={version}
                onChange={(e) => setVersion(e.target.value)}
                placeholder="e.g. 1.2.3"
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              />
            </div>

            <div className="flex items-end">
              <Button
                onClick={handleDeploy}
                disabled={!selectedService || !version || deploying}
                className="w-full"
              >
                {deploying ? 'Deploying...' : 'Deploy'}
              </Button>
            </div>
          </div>

          {result && (
            <Alert variant={result.ok ? 'default' : 'destructive'}>
              <AlertDescription>{result.message}</AlertDescription>
            </Alert>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Package className="h-5 w-5" />
            Deployment History
          </CardTitle>
        </CardHeader>
        <CardContent className="overflow-x-auto">
          {deployments.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <Package className="h-12 w-12 mb-4" />
              <p>No deployments yet</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Service</TableHead>
                  <TableHead>Version</TableHead>
                  <TableHead>Strategy</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Progress</TableHead>
                  <TableHead>Started</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {deployments.map((dep) => (
                  <TableRow key={dep.id}>
                    <TableCell className="font-medium">{dep.service}</TableCell>
                    <TableCell className="font-mono">{dep.version}</TableCell>
                    <TableCell>{dep.strategy}</TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          dep.status === 'completed' ? 'default'
                            : dep.status === 'failed' ? 'destructive'
                            : dep.status === 'running' ? 'secondary'
                            : 'outline'
                        }
                      >
                        {dep.status}
                      </Badge>
                    </TableCell>
                    <TableCell>{dep.progress}%</TableCell>
                    <TableCell className="text-sm">
                      {new Date(dep.started_at).toLocaleString()}
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
