// uptimeLabel returns a compact "Xd Xh Xm Xs" string for the time
// elapsed since startedAt. Returns '—' when:
//   - the timestamp is missing,
//   - it's Go's zero-time ("0001-01-01T00:00:00Z"), which would
//     otherwise render as "over 2024 years",
//   - or the allocation isn't in the running state (uptime only makes
//     sense for live processes).
//
// Unit suffixes (s/m/h/d) localise via a module-level table that
// LocaleProvider keeps in sync via `setUptimeUnits`. Same shape as
// the format.ts unit table — the function is called from inside a
// DataTable cell renderer, which can't read React context.

type UptimeUnits = { s: string; m: string; h: string; d: string }

const UPTIME_BY_LOCALE: Record<string, UptimeUnits> = {
  en: { s: 's', m: 'm', h: 'h', d: 'd' },
  ru: { s: 'с', m: 'м', h: 'ч', d: 'д' },
}

let UPTIME: UptimeUnits = UPTIME_BY_LOCALE.en

export function setUptimeUnits(locale: string): void {
  UPTIME = UPTIME_BY_LOCALE[locale] ?? UPTIME_BY_LOCALE.en
}

export function uptimeLabel(startedAt: string | undefined, status: string): string {
  if (!startedAt || status !== 'running') return '—'
  const ts = Date.parse(startedAt)
  if (Number.isNaN(ts) || ts < Date.UTC(2000, 0, 1)) return '—'

  const secs = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (secs < 60) return `${secs}${UPTIME.s}`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}${UPTIME.m}`
  const hours = Math.floor(mins / 60)
  if (hours < 24) {
    const m = mins % 60
    return m === 0 ? `${hours}${UPTIME.h}` : `${hours}${UPTIME.h} ${m}${UPTIME.m}`
  }
  const days = Math.floor(hours / 24)
  const h = hours % 24
  return h === 0 ? `${days}${UPTIME.d}` : `${days}${UPTIME.d} ${h}${UPTIME.h}`
}
