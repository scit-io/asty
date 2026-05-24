import { useMemo, useState } from 'react'
import { Rocket } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { api } from '@/api/client'
import type { Allocation, DeploymentRecord } from '@/types'

interface ServiceDeployCardProps {
  name: string
  // GitHub Releases tags (cached server-side, empty in dev when
  // A_GITHUB_REPO is unset). Source 1 of the version dropdown after
  // the static "latest" alias.
  githubVersions: string[]
  // Deploy history for this service — the version field across
  // records becomes source 2 of the dropdown.
  deployHistory: DeploymentRecord[]
  // Live allocations — versions currently running become source 3.
  allocations: Allocation[]
  // liveActive disables the Deploy button while a rollout is in
  // flight (deploy SSE pushed status === 'running').
  liveActive: boolean
  // onChanged fires after the dispatch so the page can refetch the
  // /autoscaler payload + deploy history without waiting for the
  // SSE/poll tick. Usually `() => refreshService(name)`.
  onChanged: () => Promise<void>
  className?: string
}

// ServiceDeployCard renders the per-service "Deploy a new version"
// card. Owns the version dropdown's composition (latest + GitHub +
// history + running) so the page hands raw sources, not a finished
// list.
export function ServiceDeployCard({
  name, githubVersions, deployHistory, allocations, liveActive, onChanged, className,
}: ServiceDeployCardProps) {
  const [version, setVersion] = useState('latest')
  const [deploying, setDeploying] = useState(false)

  // Available versions for the Deploy select. Order:
  //   1. "latest" — always first, GitHub Release alias + sensible
  //      default that operators reach for most often.
  //   2. GitHub Releases tags.
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
    if (!version) return
    setDeploying(true)
    try {
      await api.deploy(name, version)
      toast.success(`Deploying ${name}@${version}`)
      setVersion('latest')
      await onChanged()
    } catch (err) {
      toast.error(`Deploy failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setDeploying(false)
    }
  }

  return (
    <Card className={className}>
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
  )
}
