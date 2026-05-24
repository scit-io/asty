import type { Node, Allocation } from '@/types'

// Maps status / health enums to the Badge variant they should render
// as. Sits next to lib/node-status.ts (which carries the matching
// Switch-track palette) so the dashboard's colour vocabulary lives in
// one place: ready/healthy/running → green, transition states →
// yellow, terminal failures → red.

export const nodeStatusVariant = (s?: Node['status']): 'success' | 'warning' | 'destructive' | 'secondary' => {
  switch (s) {
    case 'ready':    return 'success'
    case 'down':     return 'destructive'
    case 'draining':
    case 'drained': return 'warning'
    default:         return 'secondary'
  }
}

export const allocStatusVariant = (s?: Allocation['status']): 'success' | 'destructive' | 'secondary' =>
  s === 'running' ? 'success' : s === 'failed' ? 'destructive' : 'secondary'

export const allocHealthVariant = (h?: Allocation['health_status']): 'success' | 'destructive' | 'secondary' =>
  h === 'healthy' ? 'success' : h === 'unhealthy' ? 'destructive' : 'secondary'

// Deploy uses the green `default` variant for running/completed (so
// they read as a positive state, matching the `default` colour of
// progress and primary buttons), `secondary` for the operator-driven
// `reverted` outcome, `destructive` for failures, `outline` as a
// neutral fallback.
export const deployStatusVariant = (s?: string): 'default' | 'destructive' | 'secondary' | 'outline' => {
  switch (s) {
    case 'running':
    case 'completed':       return 'default'
    case 'failed':
    case 'rollback_failed': return 'destructive'
    case 'reverted':        return 'secondary'
    default:                return 'outline'
  }
}
