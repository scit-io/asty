// All formatters round their input first so float-point artefacts
// (e.g. 0.6 + 0.7 → 1.3000000000000003 from JSON encoding) never
// leak to the UI. Callers don't need to pre-round.
//
// Zero collapses to a bare "0" without a unit — saying "0 Gb" implies
// precision the system doesn't have, and a single "0" reads cleaner
// in dense table cells. Percent is the exception (0% means something
// specific) and keeps the unit.

// formatCount renders monotonic counters (NATS in_msgs, out_msgs, etc.)
// compactly so a tile doesn't have to grow to fit ten-digit lifetime
// totals. Uses standard K/M/B suffixes, one decimal place above 1000.
export function formatCount(n: number): string {
  const v = Math.round(n)
  if (v < 1000) return v.toString()
  if (v < 1_000_000) return `${(v / 1000).toFixed(1)}K`
  if (v < 1_000_000_000) return `${(v / 1_000_000).toFixed(1)}M`
  return `${(v / 1_000_000_000).toFixed(1)}B`
}

// formatMB renders Mb sizes with Gb suffix past the 1024 cutoff.
// Used by MetricTile callers showing memory/disk totals.
export function formatMB(mb: number): string {
  const v = Math.round(mb)
  if (v === 0) return '0'
  return v >= 1024 ? `${(v / 1024).toFixed(1)} Gb` : `${v} Mb`
}

// formatMHz renders CPU frequency with a unit-aware threshold:
// values past 10 000 MHz read more naturally in GHz than as
// "17.6k MHz". Returns the unit baked in, so callers should leave
// MetricTile's `unit` prop empty when using this formatter.
export function formatMHz(mhz: number): string {
  const v = Math.round(mhz)
  if (v === 0) return '0'
  return v >= 10000 ? `${(v / 1000).toFixed(1)} GHz` : `${v} MHz`
}

// formatPercent rounds float percentages from the backend (cpu_usage,
// memory_usage_pct, etc.) to a whole number so table cells don't
// suddenly read "0.6000000000000001%". Keeps the unit at zero —
// "0%" is meaningful in a way "0 Mb" isn't.
export function formatPercent(n: number): string {
  return `${Math.round(n)}%`
}
