// uptimeLabel returns a compact "Xd Xh Xm Xs" string for the time
// elapsed since startedAt. Returns '—' when:
//   - the timestamp is missing,
//   - it's Go's zero-time ("0001-01-01T00:00:00Z"), which would
//     otherwise render as "over 2024 years",
//   - or the allocation isn't in the running state (uptime only makes
//     sense for live processes).
export function uptimeLabel(startedAt: string | undefined, status: string): string {
  if (!startedAt || status !== 'running') return '—'
  const ts = Date.parse(startedAt)
  if (Number.isNaN(ts) || ts < Date.UTC(2000, 0, 1)) return '—'

  const secs = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (secs < 60) return `${secs}s`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m`
  const hours = Math.floor(mins / 60)
  if (hours < 24) {
    const m = mins % 60
    return m === 0 ? `${hours}h` : `${hours}h ${m}m`
  }
  const days = Math.floor(hours / 24)
  const h = hours % 24
  return h === 0 ? `${days}d` : `${days}d ${h}h`
}
