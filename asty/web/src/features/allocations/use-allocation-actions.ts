import { useCallback, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { api } from '@/api/client'
import { routes } from '@/lib/routes'
import type { Allocation } from '@/types'

// AllocLite — the minimal subset of an Allocation the hook needs.
// Pages that have the full object pass it directly; pages that only
// know the URL params can construct one manually (the alloc-detail
// page passes the cached snapshot).
type AllocLite = Pick<Allocation, 'id' | 'node_id' | 'service_name'>

// useAllocationActions wraps the Restart/Stop flow shared by three
// pages: per-node alloc list, per-service alloc list, alloc detail.
// Returns:
//   - act(kind, alloc): dispatches the call, shows the toast, and
//     after Stop navigates to /nodes (the reconciler may have
//     backfilled the slot on a different node — the operator needs
//     the cluster-wide view to find it).
//   - pending: { [allocId]: boolean }. Pages with multiple rows index
//     by allocation id; single-alloc callers read pending[a.id].
export function useAllocationActions() {
  const navigate = useNavigate()
  const [pending, setPending] = useState<Record<string, boolean>>({})

  const act = useCallback(async (kind: 'restart' | 'stop', a: AllocLite) => {
    setPending((p) => ({ ...p, [a.id]: true }))
    try {
      await (kind === 'restart'
        ? api.restartAllocation(a.node_id, a.id)
        : api.stopAllocation(a.node_id, a.id))
      toast.success(`${kind === 'restart' ? 'Restarted' : 'Stopped'} ${a.service_name}`)
      if (kind === 'stop') navigate(routes.nodes)
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setPending((p) => ({ ...p, [a.id]: false }))
    }
  }, [navigate])

  return { act, pending }
}
