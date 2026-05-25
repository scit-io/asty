import type { Allocation, Node } from '@/types'
import type { MessageKey } from './dictionaries'

// Enum→key maps for backend-emitted status strings. The dictionaries
// also carry transitional states the wire types don't currently
// surface (joining/stale/restarting/…); the switch handles them as
// `string` so a future backend bump that ships those values lands
// cleanly without flashing the raw wire string.

export function nodeStatusKey(status: Node['status'] | string | undefined): MessageKey {
  switch (status) {
    case 'joining': return 'node.status.joining'
    case 'ready':   return 'node.status.ready'
    case 'stale':   return 'node.status.stale'
    case 'draining': return 'node.status.draining'
    case 'drained': return 'node.status.drained'
    case 'paused':  return 'node.status.paused'
    case 'down':    return 'node.status.down'
    case 'deleted': return 'node.status.deleted'
    default:        return 'node.status.ready'
  }
}

export function allocStatusKey(status: Allocation['status'] | string | undefined): MessageKey {
  switch (status) {
    case 'pending':    return 'alloc.status.pending'
    case 'starting':   return 'alloc.status.starting'
    case 'running':    return 'alloc.status.running'
    case 'restarting': return 'alloc.status.restarting'
    case 'stopping':   return 'alloc.status.stopping'
    case 'stopped':    return 'alloc.status.stopped'
    case 'failed':     return 'alloc.status.failed'
    case 'deleted':    return 'alloc.status.deleted'
    default:           return 'alloc.status.pending'
  }
}

export function allocHealthKey(h: Allocation['health_status'] | undefined): MessageKey {
  switch (h) {
    case 'healthy':   return 'alloc.health.healthy'
    case 'unhealthy': return 'alloc.health.unhealthy'
    default:          return 'alloc.health.unknown'
  }
}

export function serviceTypeKey(type: string | undefined): MessageKey {
  return type === 'system' ? 'service.type.system' : 'service.type.service'
}

// Deploy status comes off the wire as one of a handful of FSM tokens.
// Unknown values (a future state we haven't typed yet) flow through
// as 'pending' so the badge still renders something sensible instead
// of vanishing.
export function deployStatusKey(status: string | undefined): MessageKey {
  switch (status) {
    case 'pending':         return 'deploy.status.pending'
    case 'running':         return 'deploy.status.running'
    case 'completed':       return 'deploy.status.completed'
    case 'failed':          return 'deploy.status.failed'
    case 'rolling_back':    return 'deploy.status.rolling_back'
    case 'reverted':        return 'deploy.status.reverted'
    case 'rollback_failed': return 'deploy.status.rollback_failed'
    default:                return 'deploy.status.pending'
  }
}
